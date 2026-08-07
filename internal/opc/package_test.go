package opc_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
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

// TestOpenRejectsUnreproducibleInternalAttrs 는 중앙 디렉토리의 internal file
// attributes(오프셋 +36)가 0 이 아닌 zip 을 거절하는지 본다.
//
// zip.FileHeader 에는 이 필드가 없어서 writer 가 언제나 0 을 쓴다. Info-ZIP 은
// 텍스트 엔트리에 bit 0 을 세우므로 실제로 만나는 값이다. 통과시키면 raw 통과가
// "바이트 동일"을 낸다는 주장이 헤더 축에서 조용히 깨진다.
func TestOpenRejectsUnreproducibleInternalAttrs(t *testing.T) {
	src := buildZip(t, [][2]string{{"word/document.xml", "<w:document/>"}}, nil)

	// 중앙 디렉토리 레코드를 찾아 internal file attributes 에 bit 0 을 세운다.
	i := bytes.Index(src, []byte("PK\x01\x02"))
	if i < 0 {
		t.Fatal("중앙 디렉토리 레코드를 못 찾았다")
	}
	mutated := append([]byte(nil), src...)
	mutated[i+36] = 0x01

	_, err := opc.OpenBytes(mutated)
	var ue *opc.UnsupportedError
	if !errors.As(err, &ue) {
		t.Fatalf("재현 불가 컨테이너가 거절되지 않았다: err=%v", err)
	}
	if ue.Detail == "" {
		t.Fatal("무엇을 재현 못 했는지 detail 이 비어있다")
	}
}

// TestOpenRejectsLocalExtraMismatch 는 로컬 헤더의 extra field 가 중앙 레코드의
// 것과 다른 zip 을 거절하는지 본다.
//
// zip.Reader 는 FileHeader.Extra 를 **중앙** 레코드에서만 채우고, writer 는 그
// 사본을 양쪽 헤더에 찍는다. 그래서 로컬 쪽 내용이 소실된다. 여기서는 길이가 같은
// 내용으로 바꿔 오프셋 이동 없이 그 소실만 드러낸다 (Info-ZIP 의 UT 타임스탬프는
// 로컬 쪽이 더 길어서 파일 길이까지 달라진다).
func TestOpenRejectsLocalExtraMismatch(t *testing.T) {
	// 태그 0x9999, 길이 4, 페이로드 "AAAA" — zip.Reader 가 해석하지 않고 건너뛴다.
	extra := []byte{0x99, 0x99, 0x04, 0x00, 'A', 'A', 'A', 'A'}
	src := buildZip(t, [][2]string{{"word/document.xml", "<w:document/>"}}, extra)

	if !bytes.HasPrefix(src, []byte("PK\x03\x04")) {
		t.Fatal("로컬 헤더로 시작하지 않는다")
	}
	nameLen := int(src[26]) | int(src[27])<<8
	extraLen := int(src[28]) | int(src[29])<<8
	if extraLen != len(extra) {
		t.Fatalf("로컬 extra 길이 %d, 기대 %d", extraLen, len(extra))
	}
	// 로컬 헤더의 extra 페이로드만 "BBBB" 로 바꾼다. 중앙 레코드는 "AAAA" 그대로다.
	mutated := append([]byte(nil), src...)
	at := 30 + nameLen + 4
	copy(mutated[at:at+4], []byte("BBBB"))

	_, err := opc.OpenBytes(mutated)
	var ue *opc.UnsupportedError
	if !errors.As(err, &ue) {
		t.Fatalf("로컬 extra 소실이 거절되지 않았다: err=%v", err)
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
