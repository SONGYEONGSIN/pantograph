package opc_test

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

// assertRoundTrip 은 OpenBytes → Bytes() 왕복이 원본과 바이트 단위로 같은지 검증한다.
// TestIdentityGenerated 와 TestIdentityReal 이 공유한다.
func assertRoundTrip(t *testing.T, src []byte) {
	t.Helper()
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(src, got) {
		t.Fatalf("바이트 불일치: 원본 %d바이트, 재작성 %d바이트", len(src), len(got))
	}
}

// I1 항등 — 생성 docx
func TestIdentityGenerated(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문 한 줄"})
	assertRoundTrip(t, src)
}

// I1 항등 — 실제 Word docx.
// 픽스처가 없으면 FAIL 이다. skip 으로 바꾸지 말 것 (spec §10).
func TestIdentityReal(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "real", "*.docx"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("testdata/real/*.docx 없음 — I1 은 실제 Word 문서로만 의미가 있다 (spec §10)")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			assertRoundTrip(t, src)
		})
	}
}

// buildZip 은 주어진 엔트리로 zip 을 만든다. 이름이 겹쳐도 그대로 쓴다.
func buildZip(t *testing.T, entries [][2]string, extra []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		fh := &zip.FileHeader{Name: e[0], Method: zip.Store, Extra: extra}
		w, err := zw.CreateHeader(fh)
		if err != nil {
			t.Fatalf("CreateHeader %s: %v", e[0], err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatalf("Write %s: %v", e[0], err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

// TestReproducesNonZeroInternalAttrs 는 중앙 디렉토리의 internal file
// attributes(레코드 +36)가 0 이 아닌 zip 을 그대로 재현하는지 본다.
//
// 이 필드는 archive/zip 시절 거절 사유였다 — zip.FileHeader 에 필드가 없어
// writer 가 언제나 0 을 찍었고, Info-ZIP 은 텍스트 엔트리에 bit 0 을 세운다.
// 이제 중앙 레코드를 원본 바이트로 통과시키므로 재현된다. 게이트의 계약은
// "재현 못 하는 것을 거절한다" 이지 "이 필드를 거절한다" 가 아니므로, 재현이
// 옳은 결과다. 파트를 고쳐 재조립한 뒤에도 살아있어야 한다.
func TestReproducesNonZeroInternalAttrs(t *testing.T) {
	src := buildZip(t, [][2]string{
		{"[Content_Types].xml", "<Types/>"},
		{"word/document.xml", "<w:document/>"},
	}, nil)

	// 첫 중앙 디렉토리 레코드([Content_Types].xml)의 internal file attributes 에 bit 0 을 세운다.
	i := bytes.Index(src, []byte("PK\x01\x02"))
	if i < 0 {
		t.Fatal("중앙 디렉토리 레코드를 못 찾았다")
	}
	mutated := append([]byte(nil), src...)
	mutated[i+36] = 0x01

	assertRoundTrip(t, mutated)

	got := dirtyWrite(t, mutated, []byte(`<w:document><w:body/></w:document>`))
	rec := entryByName(t, walkZip(t, got), "[Content_Types].xml").central
	if attrs := binary.LittleEndian.Uint16(rec[36:]); attrs != 0x01 {
		t.Fatalf("재조립 후 internal file attributes 가 0x%04x — 0x0001 이어야 한다", attrs)
	}
}

// TestReproducesLocalExtraMismatch 는 로컬 헤더의 extra field 가 중앙 레코드의
// 것과 다른 zip 을 그대로 재현하는지 본다.
//
// zip.Reader 는 FileHeader.Extra 를 **중앙** 레코드에서만 채우고 writer 는 그
// 사본을 양쪽 헤더에 찍는다 — 그래서 archive/zip 은 이 컨테이너를 표현할 수
// 없었다. Word 의 0xa220 growth hint 가 바로 이 모양이다(로컬에만 존재).
// 여기서는 길이가 같은 내용으로 바꿔 오프셋 이동 없이 그 축만 시험한다.
func TestReproducesLocalExtraMismatch(t *testing.T) {
	// 태그 0x9999, 길이 4, 페이로드 "AAAA" — 어떤 리더도 해석하지 않고 건너뛴다.
	extra := []byte{0x99, 0x99, 0x04, 0x00, 'A', 'A', 'A', 'A'}
	src := buildZip(t, [][2]string{
		{"[Content_Types].xml", "<Types/>"},
		{"word/document.xml", "<w:document/>"},
	}, extra)

	if !bytes.HasPrefix(src, []byte("PK\x03\x04")) {
		t.Fatal("로컬 헤더로 시작하지 않는다")
	}
	nameLen := int(src[26]) | int(src[27])<<8
	extraLen := int(src[28]) | int(src[29])<<8
	if extraLen != len(extra) {
		t.Fatalf("로컬 extra 길이 %d, 기대 %d", extraLen, len(extra))
	}
	// 첫 엔트리의 **로컬** extra 페이로드만 "BBBB" 로 바꾼다. 중앙 레코드는 "AAAA" 그대로다.
	mutated := append([]byte(nil), src...)
	at := 30 + nameLen + 4
	copy(mutated[at:at+4], []byte("BBBB"))

	assertRoundTrip(t, mutated)

	got := dirtyWrite(t, mutated, []byte(`<w:document><w:body/></w:document>`))
	e := entryByName(t, walkZip(t, got), "[Content_Types].xml")
	if !bytes.Equal(e.localExtra, []byte{0x99, 0x99, 0x04, 0x00, 'B', 'B', 'B', 'B'}) {
		t.Fatalf("재조립 후 로컬 extra 가 % x — BBBB 여야 한다", e.localExtra)
	}
	cExtraLen := int(binary.LittleEndian.Uint16(e.central[30:]))
	cExtra := e.central[46+int(binary.LittleEndian.Uint16(e.central[28:])):][:cExtraLen]
	if !bytes.Equal(cExtra, extra) {
		t.Fatalf("재조립 후 중앙 extra 가 % x — AAAA 여야 한다", cExtra)
	}
}

// TestOpenRejectsDuplicateEntryNames 는 같은 이름의 엔트리가 둘 이상인 zip 을
// 거절하는지 본다.
//
// parts 맵은 뒤엣것이 앞엣것을 덮어쓰지만 order 에는 둘 다 남아, Write 가 뒤엣것을
// 두 번 내보낸다. archive/zip 은 중복 이름을 군말 없이 받으므로 여기서 막지 않으면
// 조작·손상된 docx 가 "ok": true 와 함께 입력과 다른 파일로 나간다.
func TestOpenRejectsDuplicateEntryNames(t *testing.T) {
	src := buildZip(t, [][2]string{
		{"[Content_Types].xml", "<Types/>"},
		{"word/document.xml", "<w:document>첫째</w:document>"},
		{"word/document.xml", "<w:document>둘째</w:document>"},
	}, nil)

	_, err := opc.OpenBytes(src)
	if err == nil {
		t.Fatal("이름이 중복된 엔트리가 거절되지 않았다")
	}
	if !strings.Contains(err.Error(), "word/document.xml") {
		t.Fatalf("에러가 중복된 이름을 지목하지 않는다: %v", err)
	}
}

func TestPartDecompresses(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목"})
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("<w:body>")) {
		t.Fatalf("document.xml 에 <w:body> 없음: %s", content)
	}
}

func TestNamesPreservesZipOrder(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목"})
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	want := []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"}
	got := p.Names()
	if len(got) != len(want) {
		t.Fatalf("엔트리 수 %d, 기대 %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("엔트리 %d: %q, 기대 %q", i, got[i], want[i])
		}
	}
}

// TestReplaceRoundTrip 은 Replace() 로 갈아끼운 파트가 재작성 결과에 그대로
// 반영되고, 건드리지 않은 엔트리는 raw 압축 바이트가 그대로인지 검증한다.
func TestReplaceRoundTrip(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}

	newContent := []byte(`<w:document><w:body><w:p/></w:body></w:document>`)
	if err := p.Replace("word/document.xml", newContent); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	p2, err := opc.OpenBytes(got)
	if err != nil {
		t.Fatalf("재작성 결과 OpenBytes: %v", err)
	}
	content, err := p2.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Equal(content, newContent) {
		t.Fatalf("교체된 내용 불일치: got %q, want %q", content, newContent)
	}

	// 건드리지 않은 엔트리는 CreateRaw 통과 경로를 타므로 raw 압축 바이트가 그대로여야 한다.
	wantRaw := rawBytes(t, src, "[Content_Types].xml")
	gotRaw := rawBytes(t, got, "[Content_Types].xml")
	if !bytes.Equal(wantRaw, gotRaw) {
		t.Fatalf("미수정 엔트리 raw 바이트 변경됨: 원본 %d바이트, 재작성 %d바이트", len(wantRaw), len(gotRaw))
	}
}

// TestReplaceNilCacheBug 는 F1 회귀 테스트다.
// Part() 의 캐시 판정이 content != nil 에 의존하면, Replace(name, nil) 로
// 유효한 nil 을 넣은 뒤 Part() 를 호출했을 때 "아직 안 풀었다"로 오판해
// 원본을 다시 압축 해제하고 그 결과로 content 필드를 덮어써버린다.
// dirty 는 true 로 남아있으므로 Write() 는 의도한 빈 내용 대신 원본을 재압축해 내보낸다.
func TestReplaceNilCacheBug(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목"})
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if err := p.Replace("word/document.xml", nil); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	// Replace 직후 현재 상태를 조회하는 것은 자연스러운 사용 흐름이다 —
	// 바로 이 호출이 캐시를 오염시키는지가 F1 의 핵심이다.
	if _, err := p.Part("word/document.xml"); err != nil {
		t.Fatalf("Part: %v", err)
	}

	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	p2, err := opc.OpenBytes(got)
	if err != nil {
		t.Fatalf("재작성 결과 OpenBytes: %v", err)
	}
	content, err := p2.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if len(content) != 0 {
		t.Fatalf("Replace(nil) 이 무시됨 — 원본 내용이 재작성 결과에 남음: %d바이트: %s", len(content), content)
	}
}

// TestGrowthHintPreservedOnDirtyWrite 는 Word 가 **로컬 헤더에만** 쓰는 0xa220
// extra field(Open Packaging Growth Hint — 파트 확장용 예약 패딩)가 파트를 하나
// 고쳐 재조립한 뒤에도 살아있는지 본다.
//
// 이것이 archive/zip 을 버린 이유 자체다. zip.Reader 는 Extra 를 **중앙**
// 레코드에서만 채우고 zip.Writer 는 그 사본을 양쪽에 찍으므로 "로컬에만 있고
// 중앙엔 없는 extra" 는 표현 자체가 불가능했고, 파일당 1,832바이트가 사라졌다.
// 파일 전체 비교로도 잡히지만 무엇이 왜 보존돼야 하는지는 여기서 직접 못박는다.
func TestGrowthHintPreservedOnDirtyWrite(t *testing.T) {
	src := readFixture(t, "form-a.docx")

	var hinted []string
	for _, e := range walkZip(t, src) {
		if e.name == "word/document.xml" {
			continue // 이건 고칠 엔트리다 — 미수정 엔트리만 본다
		}
		if hasExtraID(e.localExtra, 0xa220) {
			hinted = append(hinted, e.name)
		}
		if l := len(e.centralExtra(t)); l != 0 {
			t.Fatalf("%s: 중앙 extra 가 %d바이트 — 픽스처가 '로컬에만 있는 extra' 를 시험하지 못한다", e.name, l)
		}
	}
	if len(hinted) == 0 {
		t.Fatal("픽스처에 0xa220 growth hint 를 가진 미수정 엔트리가 없다 — 테스트가 무의미하다")
	}

	got := dirtyWrite(t, src, patchedDocument(t, src))
	before, after := walkZip(t, src), walkZip(t, got)
	for _, name := range hinted {
		b, a := entryByName(t, before, name), entryByName(t, after, name)
		if !bytes.Equal(b.localExtra, a.localExtra) {
			t.Fatalf("%s: growth hint 가 바뀌었다 (원본 %d바이트, 재조립 %d바이트)",
				name, len(b.localExtra), len(a.localExtra))
		}
		if !hasExtraID(a.localExtra, 0xa220) {
			t.Fatalf("%s: 재조립 후 0xa220 extra 가 사라졌다", name)
		}
		if !bytes.Equal(b.local, a.local) {
			t.Fatalf("%s: 미수정 엔트리의 로컬 블록이 달라졌다 (원본 %d바이트, 재조립 %d바이트)",
				name, len(b.local), len(a.local))
		}
	}
}

// TestOffsetsPatchedOnDirtyWrite 는 수정된 파트의 크기가 달라져 뒤쪽 엔트리가
// 전부 밀렸을 때, 중앙 디렉토리의 로컬 헤더 오프셋이 다시 계산됐는지 본다.
//
// 오프셋을 틀리게 고쳐도 앞에서부터 훑는 리더는 파일을 열어버린다. 그래서
// "열리더라" 로는 부족하고, 중앙 디렉토리가 가리키는 자리에 실제로 그 엔트리의
// 로컬 헤더가 있는지를 엔트리 단위로 확인한다 (walkZip 이 서명까지 대조한다).
func TestOffsetsPatchedOnDirtyWrite(t *testing.T) {
	src := readFixture(t, "form-a.docx")
	got := dirtyWrite(t, src, patchedDocument(t, src))
	if len(got) == len(src) {
		t.Fatal("길이가 그대로다 — 오프셋이 밀리지 않아 이 테스트가 아무것도 안 본다")
	}

	// 게이트를 다시 통과해야 한다 — 재조립 결과가 자기정합이라는 뜻이다.
	if _, err := opc.OpenBytes(got); err != nil {
		t.Fatalf("재조립 결과를 다시 열 수 없다: %v", err)
	}

	before, after := walkZip(t, src), walkZip(t, got)
	if len(before) != len(after) {
		t.Fatalf("엔트리 수 %d → %d", len(before), len(after))
	}
	for i := range before {
		b, a := before[i], after[i]
		if b.name != a.name {
			t.Fatalf("엔트리 %d 이름 %q → %q", i, b.name, a.name)
		}
		if b.name == "word/document.xml" {
			if bytes.Equal(b.payload, a.payload) {
				t.Fatal("수정한 파트인데 압축 데이터가 그대로다")
			}
			continue
		}
		if !bytes.Equal(b.local, a.local) {
			t.Fatalf("%s: 미수정 엔트리의 로컬 블록이 달라졌다", b.name)
		}
	}

	// 독립 리더로도 전 엔트리가 읽혀야 한다.
	zr, err := zip.NewReader(bytes.NewReader(got), int64(len(got)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	if len(zr.File) != len(before) {
		t.Fatalf("archive/zip 이 본 엔트리 수 %d, 기대 %d", len(zr.File), len(before))
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("%s: Open: %v", f.Name, err)
		}
		if _, err := io.ReadAll(rc); err != nil {
			t.Fatalf("%s: ReadAll: %v", f.Name, err)
		}
		rc.Close()
	}
}

// TestModifiedEntryKeepsDataDescriptor 는 data descriptor(범용 플래그 bit 3)를
// 가진 엔트리를 고쳐 쓸 때 descriptor 를 새 값으로 다시 쓰는지 본다.
//
// archive/zip 의 Writer 는 CreateHeader 로 만든 **모든** 엔트리에 bit 3 을
// 세운다. 즉 이 프로젝트의 생성 픽스처는 전 엔트리가 descriptor 를 갖는다 —
// descriptor 엔트리의 수정을 거절하면 테스트 전체가 무너진다. 그래서 거절이
// 아니라 재작성으로 다룬다.
func TestModifiedEntryKeepsDataDescriptor(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	for _, e := range walkZip(t, src) {
		if e.flags&0x08 == 0 {
			t.Fatalf("%s: 생성 픽스처에 data descriptor 가 없다 — 이 테스트의 전제가 깨졌다", e.name)
		}
	}

	newContent := []byte(`<w:document><w:body><w:p/></w:body></w:document>`)
	got := dirtyWrite(t, src, newContent)

	e := entryByName(t, walkZip(t, got), "word/document.xml")
	if e.desc == nil {
		t.Fatal("수정한 엔트리에서 data descriptor 가 사라졌다")
	}
	at := 0
	if binary.LittleEndian.Uint32(e.desc) == 0x08074b50 {
		at = 4
	}
	if crc := binary.LittleEndian.Uint32(e.desc[at:]); crc != crc32.ChecksumIEEE(newContent) {
		t.Fatalf("descriptor 의 crc32 가 0x%08x — 새 내용의 crc 여야 한다", crc)
	}
	if n := binary.LittleEndian.Uint32(e.desc[at+4:]); int(n) != len(e.payload) {
		t.Fatalf("descriptor 의 압축 크기가 %d — 실제 페이로드는 %d바이트", n, len(e.payload))
	}
	if n := binary.LittleEndian.Uint32(e.desc[at+8:]); int(n) != len(newContent) {
		t.Fatalf("descriptor 의 원본 크기가 %d — 새 내용은 %d바이트", n, len(newContent))
	}

	// APPNOTE 4.4.4 — descriptor 를 쓰는 엔트리는 로컬 헤더의 crc·크기가 0 이다.
	// 여기를 안 보면 양쪽에 실제 값을 찍는 퇴행이 그대로 통과한다.
	for _, f := range []struct {
		name string
		off  int
	}{{"crc32", 14}, {"압축 크기", 18}, {"원본 크기", 22}} {
		if v := binary.LittleEndian.Uint32(e.local[f.off:]); v != 0 {
			t.Fatalf("descriptor 엔트리인데 로컬 헤더의 %s 가 0x%08x — 0 이어야 한다", f.name, v)
		}
	}
}

// TestTwoDirtyEntriesInOneWrite 는 한 번의 재조립에서 파트를 **둘** 고쳤을 때도
// 오프셋·EOCD·descriptor 가 맞는지 본다.
//
// 오프셋을 "밀린 만큼 더하기"로 계산하면 dirty 가 하나일 때만 맞는다.
// assemble 은 매 엔트리마다 offsets[i] = len(out) 로 다시 잡으므로 개수와 무관하게
// 맞아야 한다 — 그 성질을 여기서 고정한다.
func TestTwoDirtyEntriesInOneWrite(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	want := map[string][]byte{
		"[Content_Types].xml": []byte(`<Types xmlns="urn:x"/>`),
		"word/document.xml":   []byte(`<w:document><w:body><w:p/></w:body></w:document>`),
	}
	for name, content := range want {
		if err := p.Replace(name, content); err != nil {
			t.Fatalf("Replace %s: %v", name, err)
		}
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	// 게이트를 다시 통과해야 한다 — 오프셋·EOCD 가 자기정합이라는 뜻이다.
	p2, err := opc.OpenBytes(got)
	if err != nil {
		t.Fatalf("두 파트를 고친 결과를 다시 열 수 없다: %v", err)
	}
	for name, content := range want {
		c, err := p2.Part(name)
		if err != nil {
			t.Fatalf("Part %s: %v", name, err)
		}
		if !bytes.Equal(c, content) {
			t.Fatalf("%s: 내용 불일치 — got %q, want %q", name, c, content)
		}
	}

	// 안 건드린 나머지는 로컬 블록이 바이트 동일해야 한다.
	before, after := walkZip(t, src), walkZip(t, got)
	if !bytes.Equal(entryByName(t, before, "_rels/.rels").local, entryByName(t, after, "_rels/.rels").local) {
		t.Fatal("_rels/.rels: 미수정 엔트리의 로컬 블록이 달라졌다")
	}
	// 고친 두 엔트리 모두 descriptor 가 남아있어야 한다 (원본이 갖고 있었으므로).
	for name := range want {
		if entryByName(t, after, name).desc == nil {
			t.Fatalf("%s: 재조립 후 data descriptor 가 사라졌다", name)
		}
	}
}

// TestDecompressStopsAtDeclaredSize 는 압축 해제가 헤더가 선언한 크기 이상으로
// 읽지 않는지 본다.
//
// 스트림과 crc 를 모두 쥔 쪽이 헤더보다 훨씬 크게 부푸는 데이터를 넣으면
// (압축 폭탄) 상한 없는 io.ReadAll 은 메모리가 닿는 데까지 읽는다. 길이 검사는
// **다 읽은 뒤에야** 도는 사후 검사라 할당을 막지 못한다.
func TestDecompressStopsAtDeclaredSize(t *testing.T) {
	const (
		bomb     = 32 << 20 // 실제로 부푸는 크기
		declared = 16       // 헤더가 주장하는 크기
	)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "word/document.xml", Method: zip.Deflate})
	if err != nil {
		t.Fatalf("CreateHeader: %v", err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("A"), bomb)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	src := buf.Bytes()

	// 중앙 레코드의 원본 크기만 낮춘다 — 로컬 블록은 손대지 않으므로 컨테이너는
	// 바이트 그대로 재현되고 게이트를 통과한다.
	i := bytes.Index(src, []byte("PK\x01\x02"))
	if i < 0 {
		t.Fatal("중앙 디렉토리 레코드를 못 찾았다")
	}
	binary.LittleEndian.PutUint32(src[i+24:], declared)

	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, perr := p.Part("word/document.xml")
	runtime.ReadMemStats(&after)

	if perr == nil {
		t.Fatal("선언 크기와 다른데 압축 해제가 성공했다")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 4<<20 {
		t.Fatalf("%d바이트를 선언한 엔트리를 푸는 데 %d바이트를 할당했다 — 압축 폭탄에 상한이 없다",
			declared, grew)
	}
}

// TestOpenRejectsZip64Locator 는 EOCD 앞에 EOCD64 로케이터가 있는 아카이브를
// 거절하는지 본다. ZIP64 는 32비트 필드를 sentinel 로 덮고 별도 레코드로 옮기므로,
// 32비트만 고치는 이 writer 로는 재조립할 수 없다 — 근사하지 않고 거절한다.
func TestOpenRejectsZip64Locator(t *testing.T) {
	src := buildZip(t, [][2]string{{"word/document.xml", "<w:document/>"}}, nil)
	i := bytes.LastIndex(src, []byte("PK\x05\x06"))
	if i < 0 {
		t.Fatal("EOCD 를 못 찾았다")
	}
	// 중앙 디렉토리와 EOCD 사이에 20바이트 로케이터를 끼운다 — 중앙 디렉토리
	// 오프셋은 그대로라 EOCD 를 고칠 필요가 없다.
	loc := make([]byte, 20)
	copy(loc, []byte("PK\x06\x07"))
	mutated := bytes.Join([][]byte{src[:i], loc, src[i:]}, nil)

	assertUnsupported(t, mutated)
}

// TestOpenRejectsZip64Sentinel 은 중앙 레코드의 32비트 크기 필드가 ZIP64
// sentinel(0xFFFFFFFF)인 아카이브를 거절하는지 본다. 로케이터가 없어도 sentinel
// 하나면 실제 값은 ZIP64 확장 필드에 있다.
func TestOpenRejectsZip64Sentinel(t *testing.T) {
	src := buildZip(t, [][2]string{{"word/document.xml", "<w:document/>"}}, nil)
	i := bytes.Index(src, []byte("PK\x01\x02"))
	if i < 0 {
		t.Fatal("중앙 디렉토리 레코드를 못 찾았다")
	}
	mutated := append([]byte(nil), src...)
	binary.LittleEndian.PutUint32(mutated[i+20:], 0xFFFFFFFF) // 압축 크기

	assertUnsupported(t, mutated)
}

// TestOpenRejectsPrependedStub 은 첫 로컬 헤더가 오프셋 0 이 아닌 아카이브를
// 거절하는지 본다 (SFX 스텁·선행 쓰레기). 앞에 붙은 바이트는 어느 엔트리에도
// 속하지 않아 재조립이 그것을 되살릴 방법이 없다.
//
// 오프셋을 전부 밀어 정합을 맞춘 뒤 거절되는지 본다 — "못 읽어서" 가 아니라
// "앞에 뭔가 붙어서" 거절하는지를 가리기 위해서다.
func TestOpenRejectsPrependedStub(t *testing.T) {
	src := buildZip(t, [][2]string{{"word/document.xml", "<w:document/>"}}, nil)
	const stub = 64
	mutated := append(bytes.Repeat([]byte{0xCC}, stub), src...)

	eocd := bytes.LastIndex(mutated, []byte("PK\x05\x06"))
	if eocd < 0 {
		t.Fatal("EOCD 를 못 찾았다")
	}
	cd := bytes.Index(mutated, []byte("PK\x01\x02"))
	if cd < 0 {
		t.Fatal("중앙 디렉토리 레코드를 못 찾았다")
	}
	binary.LittleEndian.PutUint32(mutated[eocd+16:],
		binary.LittleEndian.Uint32(mutated[eocd+16:])+stub) // 중앙 디렉토리 오프셋
	binary.LittleEndian.PutUint32(mutated[cd+42:],
		binary.LittleEndian.Uint32(mutated[cd+42:])+stub) // 로컬 헤더 오프셋

	assertUnsupported(t, mutated)
}

// TestOpenGateRejectsUnlistedAnomaly 는 명시적 거절 목록에 없는 이상 컨테이너를
// 열기 시점 자기검사가 잡아내는지 본다.
//
// 여기서는 엔트리 사이에 8바이트 패딩을 끼운다 — ZIP64 도, 멀티 디스크도, 선행
// 스텁도 아니라 어떤 규칙에도 안 걸리지만, 엔트리를 붙여 찍는 재조립이 그 8바이트를
// 되살릴 방법이 없다. 게이트가 없으면 조용히 뭉개져 나간다. 다음번 미지의 생성기가
// 스스로 정체를 드러내는 자리가 바로 여기다.
func TestOpenGateRejectsUnlistedAnomaly(t *testing.T) {
	src := buildZip(t, [][2]string{
		{"[Content_Types].xml", "<Types/>"},
		{"word/document.xml", "<w:document/>"},
	}, nil)

	const pad = 8
	second := walkZip(t, src)[1].localStart
	mutated := bytes.Join([][]byte{src[:second], bytes.Repeat([]byte{0}, pad), src[second:]}, nil)

	// 밀린 오프셋을 전부 맞춰준다 — "못 읽어서" 가 아니라 "재현이 안 돼서"
	// 거절하는지를 가리기 위해서다.
	eocd := bytes.LastIndex(mutated, []byte("PK\x05\x06"))
	if eocd < 0 {
		t.Fatal("EOCD 를 못 찾았다")
	}
	off := int(binary.LittleEndian.Uint32(mutated[eocd+16:])) + pad
	binary.LittleEndian.PutUint32(mutated[eocd+16:], uint32(off))
	for range 2 {
		if lho := binary.LittleEndian.Uint32(mutated[off+42:]); lho >= uint32(second) {
			binary.LittleEndian.PutUint32(mutated[off+42:], lho+pad)
		}
		off += 46 + int(binary.LittleEndian.Uint16(mutated[off+28:])) +
			int(binary.LittleEndian.Uint16(mutated[off+30:])) +
			int(binary.LittleEndian.Uint16(mutated[off+32:]))
	}

	_, err := opc.OpenBytes(mutated)
	var ue *opc.UnsupportedError
	if !errors.As(err, &ue) {
		t.Fatalf("엔트리 사이 패딩이 거절되지 않았다: err=%v", err)
	}
	// 목록에 있는 규칙이 아니라 자기검사가 잡았음을 확인한다.
	if !strings.Contains(ue.Detail, "처음 갈린다") {
		t.Fatalf("자기검사가 아니라 다른 규칙이 잡았다: %s", ue.Detail)
	}
}

// assertUnsupported 는 컨테이너가 UnsupportedError 로 거절되고 무엇이 문제인지
// detail 에 적히는지 본다.
func assertUnsupported(t *testing.T, src []byte) {
	t.Helper()
	_, err := opc.OpenBytes(src)
	var ue *opc.UnsupportedError
	if !errors.As(err, &ue) {
		t.Fatalf("거절되지 않았다: err=%v", err)
	}
	if ue.Detail == "" {
		t.Fatal("무엇이 지원되지 않는지 detail 이 비어있다")
	}
}

// readFixture 는 testdata/real 의 실제 Word 픽스처를 읽는다.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "real", name))
	if err != nil {
		t.Fatalf("픽스처 %s: %v — 실제 Word 문서 없이는 이 테스트가 의미 없다 (spec §10)", name, err)
	}
	return b
}

// patchedDocument 는 픽스처의 word/document.xml 에 빈 문단을 하나 끼운 내용을
// 돌려준다. 크기가 달라져야 뒤쪽 엔트리 오프셋이 밀린다.
func patchedDocument(t *testing.T, src []byte) []byte {
	t.Helper()
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	doc, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	got := bytes.Replace(doc, []byte("<w:body>"), []byte("<w:body><w:p/>"), 1)
	if bytes.Equal(got, doc) {
		t.Fatal("픽스처의 document.xml 에 <w:body> 가 없다")
	}
	return got
}

// dirtyWrite 는 word/document.xml 을 갈아끼운 뒤 재조립 결과를 돌려준다.
func dirtyWrite(t *testing.T, src, content []byte) []byte {
	t.Helper()
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if err := p.Replace("word/document.xml", content); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return got
}

// zipEntry 는 테스트가 독립적으로 파싱한 zip 엔트리 하나다.
type zipEntry struct {
	name       string
	flags      uint16
	localStart int    // 로컬 헤더의 파일 내 오프셋
	local      []byte // 로컬 헤더 + 이름 + extra + 압축 페이로드
	localExtra []byte
	payload    []byte
	desc       []byte // data descriptor. 없으면 nil
	central    []byte // 중앙 디렉토리 레코드 전체
}

// centralExtra 는 중앙 레코드의 extra field 를 잘라낸다.
func (e zipEntry) centralExtra(t *testing.T) []byte {
	t.Helper()
	nameLen := int(binary.LittleEndian.Uint16(e.central[28:]))
	extraLen := int(binary.LittleEndian.Uint16(e.central[30:]))
	return e.central[46+nameLen:][:extraLen]
}

// walkZip 은 구현과 무관하게 중앙 디렉토리를 직접 걸어 엔트리를 뽑는다.
// 구현이 쓰는 파서를 재사용하면 같은 오해를 공유해 테스트가 무의미해진다.
func walkZip(t *testing.T, b []byte) []zipEntry {
	t.Helper()
	eocd := bytes.LastIndex(b, []byte("PK\x05\x06"))
	if eocd < 0 {
		t.Fatal("EOCD 없음")
	}
	n := int(binary.LittleEndian.Uint16(b[eocd+10:]))
	off := int(binary.LittleEndian.Uint32(b[eocd+16:]))
	out := make([]zipEntry, 0, n)
	for i := range n {
		if binary.LittleEndian.Uint32(b[off:]) != 0x02014b50 {
			t.Fatalf("엔트리 %d: 오프셋 %d 에 중앙 레코드 서명이 없다", i, off)
		}
		flags := binary.LittleEndian.Uint16(b[off+8:])
		csize := int(binary.LittleEndian.Uint32(b[off+20:]))
		nameLen := int(binary.LittleEndian.Uint16(b[off+28:]))
		extraLen := int(binary.LittleEndian.Uint16(b[off+30:]))
		cmtLen := int(binary.LittleEndian.Uint16(b[off+32:]))
		name := string(b[off+46 : off+46+nameLen])
		ls := int(binary.LittleEndian.Uint32(b[off+42:]))
		if binary.LittleEndian.Uint32(b[ls:]) != 0x04034b50 {
			t.Fatalf("%s: 중앙 디렉토리가 가리키는 오프셋 %d 에 로컬 헤더 서명이 없다", name, ls)
		}
		hdrLen := 30 + int(binary.LittleEndian.Uint16(b[ls+26:])) + int(binary.LittleEndian.Uint16(b[ls+28:]))
		end := ls + hdrLen + csize
		e := zipEntry{
			name:       name,
			flags:      flags,
			localStart: ls,
			local:      b[ls:end],
			localExtra: b[ls+30+int(binary.LittleEndian.Uint16(b[ls+26:])) : ls+hdrLen],
			payload:    b[ls+hdrLen : end],
			central:    b[off : off+46+nameLen+extraLen+cmtLen],
		}
		if flags&0x08 != 0 {
			descLen := 12
			if binary.LittleEndian.Uint32(b[end:]) == 0x08074b50 {
				descLen = 16
			}
			e.desc = b[end : end+descLen]
		}
		out = append(out, e)
		off += 46 + nameLen + extraLen + cmtLen
	}
	return out
}

// entryByName 은 walkZip 결과에서 이름으로 하나를 고른다.
func entryByName(t *testing.T, entries []zipEntry, name string) zipEntry {
	t.Helper()
	for _, e := range entries {
		if e.name == name {
			return e
		}
	}
	t.Fatalf("엔트리 없음: %s", name)
	return zipEntry{}
}

// hasExtraID 는 extra field 목록에 주어진 헤더 ID 가 있는지 본다.
func hasExtraID(extra []byte, id uint16) bool {
	for p := 0; p+4 <= len(extra); {
		if binary.LittleEndian.Uint16(extra[p:]) == id {
			return true
		}
		p += 4 + int(binary.LittleEndian.Uint16(extra[p+2:]))
	}
	return false
}

// rawBytes 는 zip 데이터에서 이름이 name 인 엔트리의 압축된 원본 바이트를 돌려준다.
func rawBytes(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.OpenRaw()
		if err != nil {
			t.Fatalf("OpenRaw: %v", err)
		}
		raw, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		return raw
	}
	t.Fatalf("엔트리 없음: %s", name)
	return nil
}
