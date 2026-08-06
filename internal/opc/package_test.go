package opc_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

// I1 항등 — 생성 docx
func TestIdentityGenerated(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문 한 줄"})

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
