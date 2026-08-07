// Package opc 는 docx(OPC 컨테이너)를 zip 엔트리 단위로 다룬다.
//
// 핵심 계약: 수정되지 않은 엔트리는 압축을 풀지도, 다시 압축하지도 않는다.
// zip.File.OpenRaw 로 읽은 압축 데이터를 zip.Writer.CreateRaw 로 그대로 흘려보낸다.
// 그래서 "안 건드린 파트는 바이트 동일"이 증명이 아니라 구조로 보장된다.
package opc

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

type part struct {
	file    *zip.File
	header  zip.FileHeader
	raw     []byte // 압축된 원본 바이트
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
	order []string // zip 원본 엔트리 순서
	parts map[string]*part
}

// UnsupportedError 는 zip 컨테이너를 바이트 동일하게 재조립할 수 없을 때다.
//
// raw 통과는 엔트리의 **내용**을 보존하지만 **헤더**는 보존하지 못한다.
// Write 는 zip.FileHeader 로부터 로컬·중앙 헤더를 다시 찍어내는데,
// archive/zip 이 zip 헤더의 모든 것을 표현하지는 못하기 때문이다 (확인된 두 가지):
//
//   - 중앙 디렉토리의 internal file attributes — zip.FileHeader 에 필드가 없어
//     writer 가 항상 0 을 쓴다. Info-ZIP 은 텍스트 엔트리에 bit 0 을 세운다
//   - 로컬 헤더의 extra field — zip.Reader 는 **중앙** 레코드의 것만 채우고
//     writer 는 그 사본을 양쪽 헤더에 찍는다. Info-ZIP 의 UT 타임스탬프 extra 는
//     로컬 쪽이 더 길어서 파일 길이 자체가 달라진다
//
// 이런 파일은 폴백으로 근사하지 않고 거절한다 — 재현할 수 없는 것을
// 재현했다고 말하지 않는다.
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
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("zip 열기 실패: %w", err)
	}
	sum := sha256.Sum256(b)
	p := &Package{
		Hash:  "sha256:" + hex.EncodeToString(sum[:]),
		src:   b,
		parts: make(map[string]*part, len(zr.File)),
	}
	for _, f := range zr.File {
		// 이름이 겹치면 parts 맵에서 뒤엣것이 앞엣것을 덮어쓴다. order 에는 둘 다
		// 남으므로 Write 가 같은 엔트리를 두 번 내보내 원본과 다른 파일이 된다.
		// archive/zip 은 중복 이름을 그냥 받아주므로 여기서 막는다.
		if _, dup := p.parts[f.Name]; dup {
			return nil, fmt.Errorf("zip 엔트리 이름 중복: %s", f.Name)
		}
		rc, err := f.OpenRaw()
		if err != nil {
			return nil, fmt.Errorf("%s: raw 열기 실패: %w", f.Name, err)
		}
		raw, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("%s: raw 읽기 실패: %w", f.Name, err)
		}
		p.order = append(p.order, f.Name)
		p.parts[f.Name] = &part{file: f, header: f.FileHeader, raw: raw}
	}

	// 자기검사 — 지금 읽은 것을 그대로 되쓰면 원본이 나오는가.
	//
	// I1(항등)은 빈 패치 경로에서 src 를 그대로 흘려보내 구조로 보장되지만,
	// 파트를 하나라도 고치면 Write 는 컨테이너를 재조립한다. 그때 헤더가
	// 재현되지 않는 파일이면 "안 건드린 엔트리는 바이트 동일"(I2)이 조용히 깨진다.
	// 열 때 한 번 검사해서, 통과한 파일은 재조립해도 안전함이 보장되게 한다.
	var check bytes.Buffer
	check.Grow(len(b))
	if err := p.writeReassembled(&check); err != nil {
		return nil, fmt.Errorf("컨테이너 자기검사 실패: %w", err)
	}
	if got := check.Bytes(); !bytes.Equal(b, got) {
		return nil, &UnsupportedError{Detail: reproDetail(b, got)}
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
	rc, err := pt.file.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: 열기 실패: %w", name, err)
	}
	defer rc.Close()
	c, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("%s: 읽기 실패: %w", name, err)
	}
	pt.content = c
	pt.loaded = true
	return c, nil
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
// 능력에 의존시키지 않고 구조로 못박는다. 재조립 경로는 zip.FileHeader 가
// 표현하지 못하는 헤더 정보를 잃을 수 있다 (UnsupportedError 참조).
func (p *Package) Write(w io.Writer) error {
	if !p.dirty() {
		_, err := w.Write(p.src)
		return err
	}
	return p.writeReassembled(w)
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

// writeReassembled 는 엔트리를 하나씩 다시 써서 컨테이너를 조립한다.
// dirty 가 아닌 엔트리는 압축 데이터를 그대로 통과시킨다.
func (p *Package) writeReassembled(w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, name := range p.order {
		pt := p.parts[name]
		fh := pt.header // 값 복사 — 원본 헤더를 건드리지 않는다

		if !pt.dirty {
			dst, err := zw.CreateRaw(&fh)
			if err != nil {
				return fmt.Errorf("%s: CreateRaw 실패: %w", name, err)
			}
			if _, err := dst.Write(pt.raw); err != nil {
				return fmt.Errorf("%s: raw 쓰기 실패: %w", name, err)
			}
			continue
		}

		// 재압축 대상이므로 원본의 CRC·크기를 지운다. Writer 가 다시 계산한다.
		fh.CRC32 = 0
		fh.CompressedSize, fh.CompressedSize64 = 0, 0
		fh.UncompressedSize, fh.UncompressedSize64 = 0, 0
		dst, err := zw.CreateHeader(&fh)
		if err != nil {
			return fmt.Errorf("%s: CreateHeader 실패: %w", name, err)
		}
		if _, err := dst.Write(pt.content); err != nil {
			return fmt.Errorf("%s: 쓰기 실패: %w", name, err)
		}
	}
	return zw.Close()
}

// Bytes 는 Write 결과를 메모리로 돌려준다.
func (p *Package) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
