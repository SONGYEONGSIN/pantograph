package dump_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

func docOf(t *testing.T, src []byte) *parts.Document {
	t.Helper()
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	return d
}

func realDoc(t *testing.T, name string) *parts.Document {
	t.Helper()
	p, err := opc.Open(filepath.Join("..", "..", "testdata", "real", name))
	if err != nil {
		t.Fatalf("Open %s: %v", name, err)
	}
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	return d
}

// I3 결정성
func TestDumpIsDeterministic(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문", "꼬리말"})
	run := func() []byte {
		d, err := dump.Build(docOf(t, src), nil)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		b, err := dump.Marshal(d)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		return b
	}
	for i := 0; i < 20; i++ {
		if a, b := run(), run(); !bytes.Equal(a, b) {
			t.Fatalf("반복 %d 에서 덤프가 달라졌다", i)
		}
	}
}

func TestDumpDocxShape(t *testing.T) {
	d, err := dump.Build(docOf(t, testutil.MinimalDocx([]string{"제목"})), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if d.Doc.Format != "docx" {
		t.Fatalf("Format = %q", d.Doc.Format)
	}
	if len(d.Doc.Parts) != 3 {
		t.Fatalf("Parts %d개, want 3", len(d.Doc.Parts))
	}
	if len(d.Doc.Scanned) != 1 || d.Doc.Scanned[0] != "word/document.xml" {
		t.Fatalf("Scanned = %v", d.Doc.Scanned)
	}
	if len(d.ScannedParts) != 1 {
		t.Fatalf("ScannedParts %d개, want 1", len(d.ScannedParts))
	}
	sp := d.ScannedParts[0]
	if sp.Ref != "docx/document" || sp.Root != "document" {
		t.Fatalf("%+v", sp)
	}
	if len(sp.Nodes) == 0 {
		t.Fatal("노드가 비었다")
	}
	if sp.Nodes[0].Path != "document" {
		t.Fatalf("첫 노드 Path = %q, want document", sp.Nodes[0].Path)
	}
}

func TestDumpPptxHasAllSlides(t *testing.T) {
	d, err := dump.Build(realDoc(t, "deck-a.pptx"), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if d.Doc.Format != "pptx" {
		t.Fatalf("Format = %q", d.Doc.Format)
	}
	if len(d.ScannedParts) != 3 {
		t.Fatalf("슬라이드 %d개, want 3", len(d.ScannedParts))
	}
	for i, want := range []string{"pptx/slide[1]", "pptx/slide[2]", "pptx/slide[3]"} {
		if d.ScannedParts[i].Ref != want {
			t.Errorf("[%d].Ref = %q, want %q", i, d.ScannedParts[i].Ref, want)
		}
	}
}

func TestDumpSelectorNarrows(t *testing.T) {
	d, err := dump.Build(realDoc(t, "deck-a.pptx"), []string{"pptx/slide[2]"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(d.ScannedParts) != 1 || d.ScannedParts[0].Ref != "pptx/slide[2]" {
		t.Fatalf("선택자가 안 먹었다: %+v", d.Doc.Scanned)
	}
	// doc.parts 는 컨테이너 전체 그대로다 — 선택자가 좁히는 건 스캔 대상뿐
	if len(d.Doc.Parts) < 10 {
		t.Fatalf("doc.parts 가 선택자에 같이 좁혀졌다: %d개", len(d.Doc.Parts))
	}
}

// 노드 JSON 에 part 가 실리면 안 된다 — 묶음 머리에 이미 있다
func TestNodeJSONOmitsPart(t *testing.T) {
	d, err := dump.Build(docOf(t, testutil.MinimalDocx([]string{"제목"})), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	b, err := json.Marshal(d.ScannedParts[0].Nodes[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if bytes.Contains(b, []byte(`"part"`)) {
		t.Fatalf("노드 JSON 에 part 가 실렸다: %s", b)
	}
	// Go 쪽에는 채워져 있어야 한다
	if d.ScannedParts[0].Nodes[0].Part != "word/document.xml" {
		t.Fatalf("Node.Part = %q", d.ScannedParts[0].Nodes[0].Part)
	}
}
