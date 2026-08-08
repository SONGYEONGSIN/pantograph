package parts_test

import (
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

func openReal(t *testing.T, name string) *opc.Package {
	t.Helper()
	p, err := opc.Open(filepath.Join("..", "..", "testdata", "real", name))
	if err != nil {
		t.Fatalf("Open %s: %v", name, err)
	}
	return p
}

func TestPlanDocx(t *testing.T) {
	p, err := opc.OpenBytes(testutil.MinimalDocx([]string{"제목"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	format, ps, err := parts.Plan(p)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if format != "docx" {
		t.Fatalf("format = %q, want docx", format)
	}
	if len(ps) != 1 {
		t.Fatalf("본문 파트 %d개, want 1: %+v", len(ps), ps)
	}
	if ps[0].Name != "word/document.xml" || ps[0].Ref != "docx/document" || ps[0].Root != "document" {
		t.Fatalf("%+v", ps[0])
	}
}

// pptx 는 실제 PowerPoint 산출물로만 의미가 있다.
func TestPlanOrdersSlidesByPresentation(t *testing.T) {
	p := openReal(t, "deck-a.pptx")
	format, ps, err := parts.Plan(p)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if format != "pptx" {
		t.Fatalf("format = %q, want pptx", format)
	}
	if len(ps) != 3 {
		t.Fatalf("슬라이드 %d개, want 3: %+v", len(ps), ps)
	}
	for i, want := range []string{"pptx/slide[1]", "pptx/slide[2]", "pptx/slide[3]"} {
		if ps[i].Ref != want {
			t.Errorf("ps[%d].Ref = %q, want %q", i, ps[i].Ref, want)
		}
		if ps[i].Root != "sld" {
			t.Errorf("ps[%d].Root = %q, want sld", i, ps[i].Root)
		}
	}

	// 순서는 sldIdLst 가 정한다. 파일명 정렬과 우연히 같을 수는 있어도
	// 그것에 기대지 않는다 — 여기서는 세 파트가 모두 슬라이드인지만 본다.
	for i, pt := range ps {
		if got := pt.Name; len(got) < 16 || got[:16] != "ppt/slides/slide" {
			t.Errorf("ps[%d].Name = %q — 슬라이드 파트가 아니다", i, got)
		}
	}
}

// deck-a.pptx 는 파일명과 sldIdLst 순서가 우연히 같아, 파일명으로 정렬하는
// 구현도 TestPlanOrdersSlidesByPresentation 을 통과시켜 버린다. 이 테스트는
// 둘을 일부러 어긋나게 만든 합성 컨테이너로 그 구멍을 막는다 — sldIdLst 가
// slide3 → slide1 → slide2 순서를 가리키게 하고, 실제로 그 순서로 나오는지 본다.
func TestPlanUsesSldIdOrderNotFileOrder(t *testing.T) {
	const slideCT = `application/vnd.openxmlformats-officedocument.presentationml.slide+xml`
	src := testutil.ZipOf(map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Override PartName="/ppt/slides/slide1.xml" ContentType="` + slideCT + `"/>` +
			`<Override PartName="/ppt/slides/slide2.xml" ContentType="` + slideCT + `"/>` +
			`<Override PartName="/ppt/slides/slide3.xml" ContentType="` + slideCT + `"/>` +
			`</Types>`,
		"ppt/presentation.xml": `<?xml version="1.0"?>` +
			`<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<p:sldIdLst>` +
			`<p:sldId id="1" r:id="rId1"/>` +
			`<p:sldId id="2" r:id="rId2"/>` +
			`<p:sldId id="3" r:id="rId3"/>` +
			`</p:sldIdLst></p:presentation>`,
		// rId1→slide3, rId2→slide1, rId3→slide2 — sldIdLst 순서(rId1,rId2,rId3)를
		// 따라가면 slide3, slide1, slide2 가 나온다. 파일명 오름차순과는 다르다.
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide3.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>` +
			`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/>` +
			`</Relationships>`,
		"ppt/slides/slide1.xml": `<p:sld/>`,
		"ppt/slides/slide2.xml": `<p:sld/>`,
		"ppt/slides/slide3.xml": `<p:sld/>`,
	})

	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	format, ps, err := parts.Plan(p)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if format != "pptx" {
		t.Fatalf("format = %q, want pptx", format)
	}

	wantNames := []string{"ppt/slides/slide3.xml", "ppt/slides/slide1.xml", "ppt/slides/slide2.xml"}
	wantRefs := []string{"pptx/slide[1]", "pptx/slide[2]", "pptx/slide[3]"}
	if len(ps) != len(wantNames) {
		t.Fatalf("슬라이드 %d개, want %d: %+v", len(ps), len(wantNames), ps)
	}
	for i := range ps {
		if ps[i].Name != wantNames[i] {
			t.Errorf("ps[%d].Name = %q, want %q — sldIdLst 순서가 아니라 파일명 순서로 나왔다", i, ps[i].Name, wantNames[i])
		}
		if ps[i].Ref != wantRefs[i] {
			t.Errorf("ps[%d].Ref = %q, want %q", i, ps[i].Ref, wantRefs[i])
		}
		if ps[i].Root != "sld" {
			t.Errorf("ps[%d].Root = %q, want sld", i, ps[i].Root)
		}
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	p := openReal(t, "deck-a.pptx")
	_, a, err := parts.Plan(p)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for i := 0; i < 10; i++ {
		_, b, err := parts.Plan(p)
		if err != nil {
			t.Fatalf("Plan: %v", err)
		}
		if len(a) != len(b) {
			t.Fatalf("길이가 달라졌다: %d vs %d", len(a), len(b))
		}
		for j := range a {
			if a[j] != b[j] {
				t.Fatalf("반복 %d, 파트 %d 이 달라졌다: %+v vs %+v", i, j, a[j], b[j])
			}
		}
	}
}

func TestPlanRejectsUnknownFormat(t *testing.T) {
	// 본문 파트가 없는 최소 OPC 컨테이너
	src := testutil.ZipOf(map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="text/xml"/></Types>`,
		"junk.xml":            `<a/>`,
	})
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if _, _, err := parts.Plan(p); err == nil {
		t.Fatal("알려진 본문 파트가 없는데 에러가 없다")
	}
}
