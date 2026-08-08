package parts_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"path/filepath"
	"strings"
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

// slideCT 는 슬라이드 파트의 ContentType 이다.
const slideCT = `application/vnd.openxmlformats-officedocument.presentationml.slide+xml`

// pptxOf 는 sldIdLst·rels·엔트리를 직접 지정해 최소 pptx 컨테이너를 만든다.
// Plan 의 거절 경로 시험용이다.
func pptxOf(overrides, sldIds, rels string, entries map[string]string) []byte {
	all := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			overrides + `</Types>`,
		"ppt/presentation.xml": `<?xml version="1.0"?>` +
			`<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<p:sldIdLst>` + sldIds + `</p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			rels + `</Relationships>`,
	}
	for n, c := range entries {
		all[n] = c
	}
	return testutil.ZipOf(all)
}

// TestPlanRejectsDuplicateSlideTarget 는 같은 슬라이드를 두 번 가리키는
// sldIdLst 를 거절하는지 본다.
//
// Plan 은 공격자가 고른 XML 을 무한한 작업량으로 바꿀 수 있는 유일한 지점이다.
// sldId 마다 계획 항목을 하나씩 붙이면서 이름 중복을 안 봤기 때문에, 같은 rId
// 를 N 번 쓰거나 두 rId 가 같은 Target 을 가리키면 계획이 N 배로 부푼다 —
// Document.Open 의 맵은 중복을 접지만 d.plan 은 N 개를 그대로 들고 있고,
// Select(nil) 이 N 개를 돌려주고, dump.Build 가 **같은 노드 슬라이스**를 N 번
// 담아 Marshal 이 N 번 직렬화한다. 2KB 짜리 presentation.xml 이 수백 MB 의
// stdout 이 된다. 거절이므로 폴백 금지 규칙과도 맞는다.
func TestPlanRejectsDuplicateSlideTarget(t *testing.T) {
	src := pptxOf(
		`<Override PartName="/ppt/slides/slide1.xml" ContentType="`+slideCT+`"/>`,
		`<p:sldId id="1" r:id="rId1"/><p:sldId id="2" r:id="rId2"/>`,
		// 서로 다른 rId 두 개가 같은 파트를 가리킨다
		`<Relationship Id="rId1" Target="slides/slide1.xml"/>`+
			`<Relationship Id="rId2" Target="slides/slide1.xml"/>`,
		map[string]string{"ppt/slides/slide1.xml": `<p:sld/>`})

	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	_, ps, err := parts.Plan(p)
	if err == nil {
		t.Fatalf("같은 슬라이드를 두 번 가리키는 sldIdLst 가 거절되지 않았다 — 계획이 %d개다: %+v", len(ps), ps)
	}
	if !errors.Is(err, parts.ErrUnsupportedFormat) {
		t.Errorf("거절이 unsupported_format 부류가 아니다: %v", err)
	}
}

// TestPlanRejectsSlideMissingFromContainer 는 컨테이너에 없는 파트를 가리키는
// sldId 를 거절하는지 본다.
//
// [Content_Types].xml 도 공격자가 고른 입력이므로 "슬라이드 ContentType 이다"는
// 그 파트가 실제로 있다는 근거가 못 된다. 없는 파트를 계획에 넣으면 첫
// doc.Tree 에서야 실패하는데, 그때는 이미 N 항목 계획이 만들어진 뒤다.
func TestPlanRejectsSlideMissingFromContainer(t *testing.T) {
	src := pptxOf(
		// slide1 은 컨테이너에 있고(hasSlide 성립), slide9 는 선언만 있다
		`<Override PartName="/ppt/slides/slide1.xml" ContentType="`+slideCT+`"/>`+
			`<Override PartName="/ppt/slides/slide9.xml" ContentType="`+slideCT+`"/>`,
		`<p:sldId id="1" r:id="rId1"/>`,
		`<Relationship Id="rId1" Target="slides/slide9.xml"/>`,
		map[string]string{"ppt/slides/slide1.xml": `<p:sld/>`})

	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	_, ps, err := parts.Plan(p)
	if err == nil {
		t.Fatalf("컨테이너에 없는 파트가 계획에 들어갔다: %+v", ps)
	}
	if !errors.Is(err, parts.ErrUnsupportedFormat) {
		t.Errorf("거절이 unsupported_format 부류가 아니다: %v", err)
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

// corruptPayload 는 zip 안 name 엔트리의 압축 데이터 마지막 바이트를 뒤집어
// **컨테이너에 있지만 읽을 수 없는 파트**를 만든다.
//
// 중앙 디렉토리에서 크기와 로컬 헤더 위치를 읽는다 — archive/zip 은 deflate
// 엔트리의 로컬 헤더에 크기를 0 으로 적고 데이터 디스크립터로 뒤에 붙이므로,
// 로컬 헤더만 봐서는 페이로드 끝을 알 수 없다.
func corruptPayload(t *testing.T, z []byte, name string) []byte {
	t.Helper()
	out := append([]byte(nil), z...)
	for i := 0; i+46 <= len(out); i++ {
		if !bytes.Equal(out[i:i+4], []byte("PK\x01\x02")) {
			continue
		}
		nlen := int(binary.LittleEndian.Uint16(out[i+28:]))
		if i+46+nlen > len(out) || string(out[i+46:i+46+nlen]) != name {
			continue
		}
		csize := int(binary.LittleEndian.Uint32(out[i+20:]))
		lho := int(binary.LittleEndian.Uint32(out[i+42:]))
		data := lho + 30 + int(binary.LittleEndian.Uint16(out[lho+26:])) +
			int(binary.LittleEndian.Uint16(out[lho+28:]))
		if csize == 0 || data+csize > len(out) {
			t.Fatalf("%s 의 페이로드 범위가 이상하다: data=%d csize=%d", name, data, csize)
		}
		out[data+csize-1] ^= 0xFF
		return out
	}
	t.Fatalf("중앙 디렉토리에서 %s 를 못 찾았다", name)
	return nil
}

// TestPlanDoesNotCallCorruptPartMissing 은 있지만 읽히지 않는 파트를
// "없음" 이라고 부르지 않는지 본다.
//
// 거절 자체는 맞다 — 손상된 입력이지 도구의 고장이 아니므로 unsupported_format
// (코드 1) 이 맞는 부류다. 틀린 것은 **사유**다: presentation.xml 이 멀쩡히
// 자리에 있는데 "없음" 이라고 하면, 그 말을 믿고 파트를 추가하려는 에이전트는
// 고칠 수 없는 것을 고치려 든다. 원인은 opc 가 이미 정확히 말하므로
// (파트 없음 / crc 불일치) 여기서 원인을 단정하지 않는다.
func TestPlanDoesNotCallCorruptPartMissing(t *testing.T) {
	src := corruptPayload(t, pptxOf(
		`<Override PartName="/ppt/slides/slide1.xml" ContentType="`+slideCT+`"/>`,
		`<p:sldId id="1" r:id="rId1"/>`,
		`<Relationship Id="rId1" Target="slides/slide1.xml"/>`,
		map[string]string{"ppt/slides/slide1.xml": `<p:sld/>`}), "ppt/presentation.xml")

	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	_, ps, err := parts.Plan(p)
	if err == nil {
		t.Fatalf("읽히지 않는 presentation.xml 로 계획이 세워졌다: %+v", ps)
	}
	if !errors.Is(err, parts.ErrUnsupportedFormat) {
		t.Fatalf("거절이 unsupported_format 부류가 아니다: %v", err)
	}
	// 이 컨테이너에는 presentation.xml 이 **있다** — 손상됐을 뿐이다.
	// 그러므로 "없음" 이라는 말은 어느 형태로든 참일 수 없다.
	if strings.Contains(err.Error(), "없음") {
		t.Fatalf("있지만 읽히지 않는 파트를 없다고 한다: %v", err)
	}
}
