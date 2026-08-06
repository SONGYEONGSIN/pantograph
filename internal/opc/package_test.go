package opc_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
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
