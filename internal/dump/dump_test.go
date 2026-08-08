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

// TestDumpOfRealFixturesIsDeterministic 은 I3 를 **실제 픽스처**로 검증한다.
//
// TestDumpIsDeterministic 은 합성 docx 만 돌고, TestPlanIsDeterministic 은 Plan
// 만 반복한다 — 둘 다 "실제 Word·PowerPoint 문서로 I3 를 검증했다"는 README 의
// 주장을 뒷받침하지 못했다. 결정성이 구조적으로 성립하더라도(전 필드가
// 슬라이스·문자열이고 맵 순회가 개입하지 않는다) 주장과 테스트가 어긋나면
// 그것이 이 프로젝트가 막으려는 실패 양식 그 자체다.
//
// Plan → Build → Marshal 전 경로를 매번 새 Document 로 두 번 돌려 바이트를 댄다 —
// 파트 순서(pptx 는 슬라이드 3장)와 노드 순서가 모두 여기 걸린다.
func TestDumpOfRealFixturesIsDeterministic(t *testing.T) {
	for _, name := range []string{"form-a.docx", "deck-a.pptx"} {
		t.Run(name, func(t *testing.T) {
			run := func() []byte {
				d, err := dump.Build(realDoc(t, name), nil)
				if err != nil {
					t.Fatalf("Build: %v", err)
				}
				b, err := dump.Marshal(d)
				if err != nil {
					t.Fatalf("Marshal: %v", err)
				}
				return b
			}
			a, b := run(), run()
			if !bytes.Equal(a, b) {
				t.Fatalf("I3 위반 — %s 를 두 번 덤프했는데 바이트가 다르다 (%d vs %d바이트)",
					name, len(a), len(b))
			}
			if len(a) == 0 {
				t.Fatal("덤프가 비었다 — 비교가 무의미하다")
			}
		})
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
