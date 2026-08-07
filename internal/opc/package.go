// Package opc 는 docx(OPC 컨테이너)를 zip 엔트리 단위로 다룬다.
//
// 핵심 계약: 수정되지 않은 엔트리는 압축을 풀지도, 다시 압축하지도 않는다.
// 원본 컨테이너에서 잘라낸 **로컬 헤더까지 포함한** 바이트 블록을 그대로
// 흘려보낸다. 그래서 "안 건드린 파트는 바이트 동일"이 증명이 아니라 구조로
// 보장된다.
//
// archive/zip 을 쓰지 않고 컨테이너를 직접 파싱하고 직접 찍는다. zip.Reader 는
// FileHeader.Extra 를 **중앙** 레코드에서만 채우고 zip.Writer 는 그 사본을
// 로컬·중앙 양쪽에 찍는다. Word 가 실제로 쓰는 "로컬에만 있고 중앙엔 없는
// extra"(0xa220 Open Packaging Growth Hint — 파트를 제자리에서 키우려고 잡아둔
// 예약 패딩, 엔트리당 264~520바이트)는 그 추상화로 표현할 수 없어 조용히
// 사라진다. 우리 쓰임의 버그가 아니라 추상화의 표현력 한계다.
package opc

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// zip 레코드 서명과 고정 길이. 오프셋은 APPNOTE 4.3 을 따른다.
const (
	sigLocal      = 0x04034b50
	sigCentral    = 0x02014b50
	sigEOCD       = 0x06054b50
	sigEOCD64Loc  = 0x07064b50
	sigDataDesc   = 0x08074b50
	localHdrLen   = 30
	centralHdrLen = 46
	eocdLen       = 22
)

const (
	flagDataDescriptor = 0x0008 // 범용 플래그 bit 3 — crc·크기가 페이로드 뒤에 온다
	methodStore        = 0
	methodDeflate      = 8

	// ZIP64 sentinel — 실제 값이 32/16비트에 안 담겨 별도 레코드로 옮겨졌다는 표시.
	sentinel32 = 0xFFFFFFFF
	sentinel16 = 0xFFFF

	// zip64 extra field 의 헤더 ID. sentinel 없이 이것만 붙는 생성기도 있다.
	extraIDZip64 = 0x0001

	// 재압축 레벨. 고정해야 같은 입력이 같은 바이트를 낸다 (I3).
	// archive/zip 의 기본값과 같은 5 를 쓴다 — 이 패키지가 archive/zip 으로
	// 만들던 출력과의 연속성을 지킨다.
	flateLevel = 5
)

type part struct {
	name string

	// 원본 컨테이너에서 잘라낸 바이트. 안 건드린 엔트리는 local 을 그대로 내보낸다.
	local   []byte // 로컬 헤더 + 이름 + extra + 압축 페이로드 (+ data descriptor)
	central []byte // 중앙 디렉토리 레코드 전체 (헤더 + 이름 + extra + 주석)

	// 중앙 레코드에서 읽은 값. data descriptor 를 쓰는 엔트리는 로컬 헤더 쪽이
	// 0 이므로 중앙 것만 권위가 있다.
	flags  uint16
	method uint16
	crc32  uint32
	csize  uint32
	usize  uint32

	hdrLen  int // local 안에서 압축 페이로드가 시작하는 위치 (30 + 이름 + extra)
	descLen int // data descriptor 길이 (0 | 12 | 16)

	content []byte // 압축 해제 캐시. content 자체는 nil 이 유효한 값일 수 있어 loaded 로 적재 여부를 따로 추적한다
	loaded  bool
	dirty   bool
}

// Package 는 열린 docx 하나를 나타낸다.
type Package struct {
	// Hash 는 입력 파일 전체 바이트의 sha256 이다 ("sha256:<hex>").
	// 패치의 낙관적 잠금이 이 값을 대조한다.
	Hash string

	src   []byte
	order []string // zip 중앙 디렉토리의 엔트리 순서
	parts map[string]*part
	eocd  []byte // EOCD 레코드 + 아카이브 주석
}

// UnsupportedError 는 zip 컨테이너를 바이트 동일하게 재조립할 수 없을 때다.
//
// 이 writer 는 안 건드린 엔트리를 원본 바이트 그대로 흘려보내고, 고친 엔트리의
// crc·크기와 뒤로 밀린 오프셋만 32비트 필드에 다시 찍는다. 그 전제가 깨지는
// 컨테이너는 근사하지 않고 거절한다:
//
//   - ZIP64 — 실제 크기·오프셋이 32비트 필드 밖에 있어 찍을 자리가 없다
//   - 멀티 디스크 — 파일 하나가 컨테이너 전체가 아니다
//   - 첫 로컬 헤더가 오프셋 0 이 아님 — 앞에 붙은 바이트(SFX 스텁 등)가
//     어느 엔트리에도 속하지 않아 재조립이 되살릴 방법이 없다
//   - store(0)·deflate(8) 아닌 압축 방식의 파트를 풀거나 다시 압축해야 할 때
//
// 여기에 더해 Open 이 열자마자 "읽은 것을 되쓰면 원본이 나오는가"를 검사한다.
// 위 목록에 없는 미지의 생성기는 이 게이트에서 스스로 정체를 드러낸다 —
// 조용히 뭉개지는 대신.
type UnsupportedError struct {
	Detail string
}

func (e *UnsupportedError) Error() string {
	return "이 zip 컨테이너는 바이트 동일하게 재현할 수 없다: " + e.Detail
}

func Open(path string) (*Package, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return OpenBytes(b)
}

func OpenBytes(b []byte) (*Package, error) {
	p, err := parse(b)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	p.Hash = "sha256:" + hex.EncodeToString(sum[:])

	// 자기검사 — 지금 읽은 것을 그대로 되쓰면 원본이 나오는가.
	//
	// I1(항등)은 빈 패치 경로에서 src 를 그대로 흘려보내 구조로 보장되지만,
	// 파트를 하나라도 고치면 Write 는 컨테이너를 재조립한다. 그때 헤더가
	// 재현되지 않는 파일이면 "안 건드린 엔트리는 바이트 동일"(I2)이 조용히 깨진다.
	// 열 때 한 번 검사해서, 통과한 파일은 재조립해도 안전함이 보장되게 한다.
	got, err := p.assemble()
	if err != nil {
		return nil, fmt.Errorf("컨테이너 자기검사 실패: %w", err)
	}
	if !bytes.Equal(b, got) {
		return nil, &UnsupportedError{Detail: reproDetail(b, got)}
	}
	return p, nil
}

func u16(b []byte, off int) uint16 { return binary.LittleEndian.Uint16(b[off:]) }
func u32(b []byte, off int) uint32 { return binary.LittleEndian.Uint32(b[off:]) }

func put32(b []byte, off int, v uint32) { binary.LittleEndian.PutUint32(b[off:], v) }

// findEOCD 는 EOCD 레코드의 오프셋을 찾는다.
//
// 서명만 보면 페이로드 안의 우연한 4바이트에 걸릴 수 있어, 주석 길이가 파일
// 끝까지의 거리와 정확히 맞는 자리만 인정한다. 주석은 최대 65535바이트다.
func findEOCD(b []byte) (int, error) {
	hi := len(b) - eocdLen
	if hi < 0 {
		return 0, fmt.Errorf("zip 이 아니다: %d바이트로는 EOCD 가 들어가지 않는다", len(b))
	}
	lo := max(hi-sentinel16, 0)
	for i := hi; i >= lo; i-- {
		if u32(b, i) == sigEOCD && int(u16(b, i+20)) == len(b)-i-eocdLen {
			return i, nil
		}
	}
	return 0, fmt.Errorf("zip 이 아니다: EOCD 레코드를 못 찾았다")
}

// hasZip64Extra 는 extra field 목록에 zip64 확장(0x0001)이 있는지 본다.
func hasZip64Extra(extra []byte) bool {
	for p := 0; p+4 <= len(extra); {
		if u16(extra, p) == extraIDZip64 {
			return true
		}
		p += 4 + int(u16(extra, p+2))
	}
	return false
}

// parse 는 컨테이너를 중앙 디렉토리 기준으로 걸어 엔트리를 잘라낸다.
//
// 엔트리 목록의 권위는 중앙 디렉토리다. 각 레코드가 적어둔 로컬 헤더 오프셋으로
// 로컬 블록을 찾아 길이를 잰다.
func parse(b []byte) (*Package, error) {
	eocdAt, err := findEOCD(b)
	if err != nil {
		return nil, err
	}

	// EOCD64 로케이터가 EOCD 바로 앞에 있으면 ZIP64 아카이브다.
	if eocdAt >= 20 && u32(b, eocdAt-20) == sigEOCD64Loc {
		return nil, &UnsupportedError{Detail: "ZIP64 아카이브다 (EOCD64 로케이터가 있다)"}
	}

	diskNo, cdDisk := u16(b, eocdAt+4), u16(b, eocdAt+6)
	thisEntries, total := u16(b, eocdAt+8), u16(b, eocdAt+10)
	cdOff := u32(b, eocdAt+16)

	if diskNo != 0 || cdDisk != 0 {
		return nil, &UnsupportedError{Detail: fmt.Sprintf(
			"멀티 디스크 아카이브다 (디스크 번호 %d, 중앙 디렉토리 디스크 %d)", diskNo, cdDisk)}
	}
	if thisEntries == sentinel16 || total == sentinel16 ||
		u32(b, eocdAt+12) == sentinel32 || cdOff == sentinel32 {
		return nil, &UnsupportedError{Detail: "EOCD 의 엔트리 수·크기·오프셋 중 ZIP64 sentinel 값이 있다"}
	}
	if thisEntries != total {
		return nil, &UnsupportedError{Detail: fmt.Sprintf(
			"이 디스크의 엔트리 %d개가 전체 %d개와 다르다 — 멀티 디스크 아카이브다", thisEntries, total)}
	}

	p := &Package{
		src:   b,
		parts: make(map[string]*part, total),
		eocd:  b[eocdAt:],
	}
	off := int(cdOff)
	minStart := len(b)
	for i := range int(total) {
		if off+centralHdrLen > len(b) || u32(b, off) != sigCentral {
			return nil, fmt.Errorf("중앙 디렉토리 레코드 %d 이 오프셋 %d 에 없다", i, off)
		}
		nameLen := int(u16(b, off+28))
		extraLen := int(u16(b, off+30))
		recLen := centralHdrLen + nameLen + extraLen + int(u16(b, off+32))
		if off+recLen > len(b) {
			return nil, fmt.Errorf("중앙 디렉토리 레코드 %d 이 파일 끝을 넘는다", i)
		}
		name := string(b[off+centralHdrLen : off+centralHdrLen+nameLen])

		if disk := u16(b, off+34); disk != 0 {
			return nil, &UnsupportedError{Detail: fmt.Sprintf("%s: 디스크 %d 에 있다 — 멀티 디스크 아카이브다", name, disk)}
		}
		crc, csize, usize := u32(b, off+16), u32(b, off+20), u32(b, off+24)
		lho := u32(b, off+42)
		if csize == sentinel32 || usize == sentinel32 || lho == sentinel32 ||
			hasZip64Extra(b[off+centralHdrLen+nameLen:][:extraLen]) {
			return nil, &UnsupportedError{Detail: fmt.Sprintf("%s: ZIP64 엔트리다 (크기·오프셋이 32비트 필드 밖에 있다)", name)}
		}

		// 이름이 겹치면 parts 맵에서 뒤엣것이 앞엣것을 덮어쓴다. order 에는 둘 다
		// 남으므로 Write 가 같은 엔트리를 두 번 내보내 원본과 다른 파일이 된다.
		if _, dup := p.parts[name]; dup {
			return nil, fmt.Errorf("zip 엔트리 이름 중복: %s", name)
		}

		ls := int(lho)
		if ls+localHdrLen > len(b) || u32(b, ls) != sigLocal {
			return nil, fmt.Errorf("%s: 로컬 헤더가 오프셋 %d 에 없다", name, ls)
		}
		hdrLen := localHdrLen + int(u16(b, ls+26)) + int(u16(b, ls+28))
		end := ls + hdrLen + int(csize)
		if end > len(b) {
			return nil, fmt.Errorf("%s: 압축 데이터가 파일 끝을 넘는다", name)
		}
		flags := u16(b, off+8)
		descLen := 0
		if flags&flagDataDescriptor != 0 {
			// descriptor 는 서명이 선택 사항이라 길이가 12 또는 16 이다.
			descLen = 12
			if end+4 <= len(b) && u32(b, end) == sigDataDesc {
				descLen = 16
			}
			if end+descLen > len(b) {
				return nil, fmt.Errorf("%s: data descriptor 가 파일 끝을 넘는다", name)
			}
		}

		minStart = min(minStart, ls)
		p.order = append(p.order, name)
		p.parts[name] = &part{
			name:    name,
			local:   b[ls : end+descLen],
			central: b[off : off+recLen],
			flags:   flags,
			method:  u16(b, off+10),
			crc32:   crc,
			csize:   csize,
			usize:   usize,
			hdrLen:  hdrLen,
			descLen: descLen,
		}
		off += recLen
	}

	// 앞에 붙은 바이트는 어느 엔트리에도 속하지 않아 재조립이 되살릴 수 없다.
	if total > 0 && minStart != 0 {
		return nil, &UnsupportedError{Detail: fmt.Sprintf(
			"첫 로컬 헤더가 오프셋 %d 에 있다 — 앞에 %d바이트가 붙어있다 (SFX 스텁 등)", minStart, minStart)}
	}
	return p, nil
}

// reproDetail 은 재조립 결과가 원본과 어디서 갈리는지 한 줄로 설명한다.
func reproDetail(src, got []byte) string {
	n := min(len(src), len(got))
	for i := range n {
		if src[i] != got[i] {
			return fmt.Sprintf("오프셋 %d 에서 처음 갈린다 (원본 0x%02x, 재조립 0x%02x; 길이 %d vs %d)",
				i, src[i], got[i], len(src), len(got))
		}
	}
	return fmt.Sprintf("앞 %d바이트는 같으나 길이가 다르다 (원본 %d, 재조립 %d)", n, len(src), len(got))
}

// Source 는 원본 파일 바이트를 돌려준다. I1 검증에 쓴다.
func (p *Package) Source() []byte { return p.src }

// Names 는 zip 원본 순서의 엔트리 이름을 돌려준다.
func (p *Package) Names() []string {
	out := make([]string, len(p.order))
	copy(out, p.order)
	return out
}

// Part 는 엔트리의 압축 해제 내용을 돌려준다. 결과는 캐시된다.
func (p *Package) Part(name string) ([]byte, error) {
	pt, ok := p.parts[name]
	if !ok {
		return nil, fmt.Errorf("파트 없음: %s", name)
	}
	if pt.loaded {
		return pt.content, nil
	}
	c, err := pt.decompress()
	if err != nil {
		return nil, err
	}
	pt.content = c
	pt.loaded = true
	return c, nil
}

// decompress 는 엔트리의 압축을 푼다.
//
// 길이와 crc32 를 헤더 값과 대조한다 — archive/zip 이 해주던 손상 검사를
// 직접 파싱으로 옮기면서 잃지 않기 위해서다.
func (pt *part) decompress() ([]byte, error) {
	raw := pt.local[pt.hdrLen : pt.hdrLen+int(pt.csize)]
	var content []byte
	switch pt.method {
	case methodStore:
		// raw 는 Package.src 를 가리키는 부분 슬라이스다. 호출자가 캐시를 고쳐
		// 원본 바이트를 오염시키지 않도록 복사한다.
		content = bytes.Clone(raw)
	case methodDeflate:
		fr := flate.NewReader(bytes.NewReader(raw))
		defer fr.Close()
		// 헤더가 선언한 크기 + 1 바이트에서 끊는다. 아래 길이 검사는 다 읽은
		// **뒤에야** 도는 사후 검사라, 상한이 없으면 스트림과 crc 를 모두 쥔 쪽이
		// 작은 크기를 선언하고 실제로는 메모리가 닿는 데까지 부풀릴 수 있다
		// (압축 폭탄). +1 은 "선언보다 크다"를 길이 검사가 보게 하려는 것이다.
		var err error
		if content, err = io.ReadAll(io.LimitReader(fr, int64(pt.usize)+1)); err != nil {
			return nil, fmt.Errorf("%s: 압축 해제 실패: %w", pt.name, err)
		}
	default:
		return nil, &UnsupportedError{Detail: fmt.Sprintf(
			"%s: 압축 방식 %d 은 다루지 않는다 (store 0 · deflate 8 만)", pt.name, pt.method)}
	}
	if uint32(len(content)) != pt.usize {
		return nil, fmt.Errorf("%s: 압축 해제 결과가 %d바이트인데 헤더는 %d바이트라고 한다", pt.name, len(content), pt.usize)
	}
	if sum := crc32.ChecksumIEEE(content); sum != pt.crc32 {
		return nil, fmt.Errorf("%s: crc32 가 0x%08x 인데 헤더는 0x%08x — 컨테이너가 손상됐다", pt.name, sum, pt.crc32)
	}
	return content, nil
}

// Replace 는 엔트리 내용을 갈아끼운다. 해당 엔트리만 dirty 가 된다.
func (p *Package) Replace(name string, content []byte) error {
	pt, ok := p.parts[name]
	if !ok {
		return fmt.Errorf("파트 없음: %s", name)
	}
	pt.content = content
	pt.loaded = true
	pt.dirty = true
	return nil
}

// Write 는 컨테이너를 내보낸다.
//
// 고친 파트가 하나도 없으면 원본 바이트를 그대로 쓴다 — I1(항등)을 헤더 재현
// 능력에 의존시키지 않고 구조로 못박는다.
func (p *Package) Write(w io.Writer) error {
	if !p.dirty() {
		_, err := w.Write(p.src)
		return err
	}
	out, err := p.assemble()
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

// dirty 는 고쳐진 파트가 하나라도 있는지 본다.
func (p *Package) dirty() bool {
	for _, pt := range p.parts {
		if pt.dirty {
			return true
		}
	}
	return false
}

// rewritten 은 재압축한 엔트리의 새 값이다.
type rewritten struct {
	payload []byte
	crc     uint32
	usize   uint32
}

// recompress 는 고친 내용을 원본과 같은 압축 방식으로 다시 압축한다.
func (pt *part) recompress() (*rewritten, error) {
	rw := &rewritten{crc: crc32.ChecksumIEEE(pt.content), usize: uint32(len(pt.content))}
	switch pt.method {
	case methodStore:
		rw.payload = pt.content
	case methodDeflate:
		var buf bytes.Buffer
		zw, err := flate.NewWriter(&buf, flateLevel)
		if err != nil {
			return nil, fmt.Errorf("%s: flate writer: %w", pt.name, err)
		}
		if _, err := zw.Write(pt.content); err != nil {
			return nil, fmt.Errorf("%s: 압축 실패: %w", pt.name, err)
		}
		if err := zw.Close(); err != nil {
			return nil, fmt.Errorf("%s: 압축 마무리 실패: %w", pt.name, err)
		}
		rw.payload = buf.Bytes()
	default:
		return nil, &UnsupportedError{Detail: fmt.Sprintf(
			"%s: 압축 방식 %d 은 다루지 않는다 (store 0 · deflate 8 만)", pt.name, pt.method)}
	}
	return rw, nil
}

// assemble 은 컨테이너를 바이트 단위로 다시 찍는다.
//
//   - 안 건드린 엔트리: 로컬 블록을 원본 바이트 그대로 (헤더의 extra 까지)
//   - 고친 엔트리: 원본 로컬 헤더를 복사해 crc·크기 세 값만 제자리에서 고치고
//     재압축한 페이로드를 붙인다. 이름·extra·플래그·압축 방식·시각은 그대로다 —
//     growth hint 는 우리가 다시 쓴 엔트리에서도 살아남는다
//   - 중앙 디렉토리: 원본 레코드를 복사해 고친 엔트리의 crc·크기와, 자리가 밀린
//     모든 엔트리의 로컬 헤더 오프셋을 고친다
//   - EOCD: 중앙 디렉토리 크기·오프셋만 고치고 아카이브 주석은 그대로
//
// Package 상태를 바꾸지 않는다 — 같은 Package 에 여러 번 불러도 같은 바이트가 나온다.
func (p *Package) assemble() ([]byte, error) {
	out := make([]byte, 0, len(p.src)+len(p.src)/8)
	offsets := make([]uint32, len(p.order))
	news := make([]*rewritten, len(p.order))

	for i, name := range p.order {
		pt := p.parts[name]
		offsets[i] = uint32(len(out))
		if !pt.dirty {
			out = append(out, pt.local...)
			continue
		}

		rw, err := pt.recompress()
		if err != nil {
			return nil, err
		}
		news[i] = rw

		hdr := bytes.Clone(pt.local[:pt.hdrLen])
		if pt.flags&flagDataDescriptor != 0 {
			// APPNOTE 4.4.4 — descriptor 를 쓰는 엔트리는 로컬 헤더의 세 값이 0 이고
			// 실제 값은 페이로드 뒤 descriptor 에 온다.
			put32(hdr, 14, 0)
			put32(hdr, 18, 0)
			put32(hdr, 22, 0)
		} else {
			put32(hdr, 14, rw.crc)
			put32(hdr, 18, uint32(len(rw.payload)))
			put32(hdr, 22, rw.usize)
		}
		out = append(out, hdr...)
		out = append(out, rw.payload...)

		if pt.descLen > 0 {
			desc := make([]byte, pt.descLen)
			at := 0
			if pt.descLen == 16 {
				put32(desc, 0, sigDataDesc)
				at = 4
			}
			put32(desc, at, rw.crc)
			put32(desc, at+4, uint32(len(rw.payload)))
			put32(desc, at+8, rw.usize)
			out = append(out, desc...)
		}
	}

	cdOff := len(out)
	for i, name := range p.order {
		pt := p.parts[name]
		rec := bytes.Clone(pt.central)
		if rw := news[i]; rw != nil {
			put32(rec, 16, rw.crc)
			put32(rec, 20, uint32(len(rw.payload)))
			put32(rec, 24, rw.usize)
		}
		put32(rec, 42, offsets[i])
		out = append(out, rec...)
	}

	// 32비트 필드에 안 담기는 크기는 ZIP64 가 필요하다 — 잘라 담지 않고 거절한다.
	if uint64(len(out)) > sentinel32 {
		return nil, &UnsupportedError{Detail: fmt.Sprintf(
			"재조립 결과가 %d바이트로 32비트 오프셋을 넘는다 — ZIP64 가 필요하다", len(out))}
	}

	eocd := bytes.Clone(p.eocd)
	put32(eocd, 12, uint32(len(out)-cdOff))
	put32(eocd, 16, uint32(cdOff))
	return append(out, eocd...), nil
}

// Bytes 는 Write 결과를 메모리로 돌려준다.
func (p *Package) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
