package diff_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/diff"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

func openDoc(t *testing.T, name string) *parts.Document {
	t.Helper()
	p, err := opc.Open(filepath.Join("..", "..", "testdata", "real", name))
	if err != nil {
		t.Fatalf("Open %s: %v", name, err)
	}
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open %s: %v", name, err)
	}
	return d
}

// TestD1SelfCompareIsEmpty 는 같은 파일을 두 번 주면 차이가 없어야 함을 본다.
//
// 해시가 같으면 순회 없이 빈 리포트를 낼 수 있지만 **그 지름길을 넣지 않는다** —
// 넣으면 이 테스트가 비교 코드를 한 줄도 통과하지 않아, 통과하는데 아무것도
// 검증하지 않는 상태가 된다 (설계 §5).
func TestD1SelfCompareIsEmpty(t *testing.T) {
	for _, name := range []string{"form-a.docx", "deck-a.pptx"} {
		t.Run(name, func(t *testing.T) {
			rep, err := diff.Compare(openDoc(t, name), openDoc(t, name), name, name, nil)
			if err != nil {
				t.Fatalf("Compare: %v", err)
			}
			if len(rep.Diffs) != 0 {
				t.Fatalf("자기 자신과 %d개 다르다: %+v", len(rep.Diffs), rep.Diffs)
			}
			if rep.Summary.Total != 0 {
				t.Fatalf("total=%d (기대 0)", rep.Summary.Total)
			}
		})
	}
}

// TestD2BodyTextMatchesTemplateKeys 는 diff 의 본문 text 항목이 tmpl.Extract 가
// 뽑은 가변 키와 정확히 일치하는지 본다.
//
// 독립적으로 짠 두 코드 경로가 같은 답을 내는지 보는 교차 검증이다. 값은
// 설계 §10 의 실측 기준표에서 왔다.
func TestD2BodyTextMatchesTemplateKeys(t *testing.T) {
	type want struct{ part, path, exp, act string }
	cases := []struct {
		a, b  string
		wants []want
	}{
		{"form-a.docx", "form-b.docx", []want{
			{"word/document.xml", "document/body[1]/p[2]/r[2]/t[1]", ": 홍길동", ": 김철수"},
			{"word/document.xml", "document/body[1]/p[3]/r[2]/t[1]", ": 1,200,000", ": 880,000"},
			{"word/document.xml", "document/body[1]/p[4]/r[2]/t[1]", ": A & B 프로젝트", ": C & D 프로젝트"},
		}},
		{"deck-a.pptx", "deck-b.pptx", []want{
			{"ppt/slides/slide1.xml", "sld/cSld[1]/spTree[1]/sp[1]/txBody[1]/p[1]/r[1]/t[1]", "표지", "겉표지"},
			{"ppt/slides/slide2.xml", "sld/cSld[1]/spTree[1]/sp[1]/txBody[1]/p[1]/r[1]/t[1]", "둘째 장", "두번째쪽"},
			{"ppt/slides/slide3.xml", "sld/cSld[1]/spTree[1]/sp[1]/txBody[1]/p[1]/r[1]/t[1]", "셋째 장", "마지막쪽"},
		}},
	}
	for _, c := range cases {
		t.Run(c.a, func(t *testing.T) {
			rep, err := diff.Compare(openDoc(t, c.a), openDoc(t, c.b), c.a, c.b, nil)
			if err != nil {
				t.Fatalf("Compare: %v", err)
			}
			var body []diff.Diff
			for _, d := range rep.Diffs {
				if d.Scope == "body" {
					body = append(body, d)
				}
			}
			if len(body) != len(c.wants) {
				t.Fatalf("본문 항목 %d개 (기대 %d개): %+v", len(body), len(c.wants), body)
			}
			for i, w := range c.wants {
				g := body[i]
				if g.Kind != "text" {
					t.Errorf("%d: kind=%q (기대 text)", i, g.Kind)
					continue
				}
				if g.Part != w.part || g.Path != w.path {
					t.Errorf("%d: %s %s (기대 %s %s)", i, g.Part, g.Path, w.part, w.path)
				}
				if g.Expected == nil || *g.Expected != w.exp {
					t.Errorf("%d: expected=%v (기대 %q)", i, g.Expected, w.exp)
				}
				if g.Actual == nil || *g.Actual != w.act {
					t.Errorf("%d: actual=%v (기대 %q)", i, g.Actual, w.act)
				}
			}
		})
	}
}

// TestFormatMismatchIsRejected 는 docx 와 pptx 를 비교하면 거절하는지 본다.
// 차이가 아니라 무의미한 입력이다.
func TestFormatMismatchIsRejected(t *testing.T) {
	_, err := diff.Compare(openDoc(t, "form-a.docx"), openDoc(t, "deck-a.pptx"),
		"form-a.docx", "deck-a.pptx", nil)
	if err == nil {
		t.Fatal("docx 와 pptx 가 비교됐다")
	}
	if !errors.Is(err, diff.ErrFormatMismatch) {
		t.Fatalf("ErrFormatMismatch 가 아니다: %v", err)
	}
}

// TestD2FullCounts 는 두 픽스처 쌍의 전체 항목 수를 고정한다.
//
// 기준값은 Go 구현이 아니라 **파이썬으로 따로 짠 비교기**가 낸 것이다
// (설계 §10). 구현이 다른 수를 내면 **수를 고치지 말고 차이를 설명한다** —
// 구현이 낸 값을 받아적으면 테스트가 구현의 거울이 되어 아무것도 검증하지 않는다.
func TestD2FullCounts(t *testing.T) {
	cases := []struct {
		a, b string
		want diff.Summary
	}{
		{"form-a.docx", "form-b.docx", diff.Summary{
			Text: 5, Attr: 2, Elem: 0, Structure: 0,
			PartContent: 0, PartMissing: 0, Total: 7, VolatileOnly: 1}},
		{"deck-a.pptx", "deck-b.pptx", diff.Summary{
			// ppt/presentation.xml 의 자식이 deck-a 는
			// [sldMasterIdLst, sldIdLst, sldSz, notesSz, defaultTextStyle, extLst],
			// deck-b 는 extLst 가 없다 — extLst 는 최상위 자식이라 정렬이
			// 정확히 deleted 1건을 낸다. 종류만 structure→deleted 로
			// 바뀌었을 뿐 Total 은 13 그대로다.
			Text: 11, Attr: 0, Elem: 0, Structure: 0, Deleted: 1,
			PartContent: 1, PartMissing: 0, Total: 13, VolatileOnly: 12}},
	}
	for _, c := range cases {
		t.Run(c.a, func(t *testing.T) {
			rep, err := diff.Compare(openDoc(t, c.a), openDoc(t, c.b), c.a, c.b, nil)
			if err != nil {
				t.Fatalf("Compare: %v", err)
			}
			if rep.Summary != c.want {
				t.Fatalf("요약이 다르다\n  실제 %+v\n  기대 %+v\n  항목: %+v",
					rep.Summary, c.want, rep.Diffs)
			}
		})
	}
}

// TestBodyItemsComeFirst 는 본문 항목이 계획 밖 항목보다 앞에 오는지 본다.
// 계획 밖 항목이 더 많을 수 있어서(deck 은 3 대 10), 사람이 먼저 봐야 할 것을
// 앞에 둔다.
func TestBodyItemsComeFirst(t *testing.T) {
	rep, err := diff.Compare(openDoc(t, "deck-a.pptx"), openDoc(t, "deck-b.pptx"),
		"deck-a.pptx", "deck-b.pptx", nil)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	seenOther := false
	for _, d := range rep.Diffs {
		if d.Scope != "body" {
			seenOther = true
			continue
		}
		if seenOther {
			t.Fatalf("계획 밖 항목 뒤에 본문 항목이 나왔다: %+v", d)
		}
	}
}

// TestThumbnailFallsBackToPartContent 는 스캔할 수 없는 파트가 part_content
// 항목 하나로 내려가는지 본다.
func TestThumbnailFallsBackToPartContent(t *testing.T) {
	rep, err := diff.Compare(openDoc(t, "deck-a.pptx"), openDoc(t, "deck-b.pptx"),
		"deck-a.pptx", "deck-b.pptx", nil)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	var found *diff.Diff
	for i := range rep.Diffs {
		if rep.Diffs[i].Part == "docProps/thumbnail.jpeg" {
			found = &rep.Diffs[i]
		}
	}
	if found == nil {
		t.Fatal("thumbnail 항목이 없다")
	}
	if found.Kind != "part_content" {
		t.Fatalf("kind=%q (기대 part_content)", found.Kind)
	}
	if found.Detail == "" {
		t.Fatal("detail 이 비었다 — 왜 경로를 못 대는지 말해야 한다")
	}
}

// TestSelectorSkipsNonPlanParts 는 --part 를 주면 계획 밖 비교를 건너뛰는지 본다.
// 슬라이드 하나를 물었는데 레이아웃 이야기를 듣는 것은 답이 아니다 (설계 §3).
func TestSelectorSkipsNonPlanParts(t *testing.T) {
	rep, err := diff.Compare(openDoc(t, "deck-a.pptx"), openDoc(t, "deck-b.pptx"),
		"deck-a.pptx", "deck-b.pptx", []string{"pptx/slide[1]"})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	for _, d := range rep.Diffs {
		if d.Scope != "body" {
			t.Fatalf("선택자를 줬는데 계획 밖 항목이 나왔다: %+v", d)
		}
	}
	if rep.Summary.VolatileOnly != 0 {
		t.Fatalf("volatile_only=%d (기대 0 — 계획 밖을 아예 안 봤어야 한다)", rep.Summary.VolatileOnly)
	}
	if rep.Summary.Total != 1 {
		t.Fatalf("total=%d (기대 1 — 슬라이드 1의 제목 하나)", rep.Summary.Total)
	}
}

// TestPartMissingAndInsertion 은 실제 픽스처에 없는 문단 수가 다른 경우를
// 합성 컨테이너로 실행한다. part_missing 은 만들지 않는다 — 파트 수가 다른
// 경우는 TestPartMissingThreeSitesAndScanAnyFallback 이 다룬다.
//
// LCS 정렬 이전에는 "두 줄"이 끝에 추가되면 "노드 수가 다르다" structure
// 항목이 났다. 지금은 정렬이 "한 줄"을 공통 접두사로 매칭하고 남은 "두 줄"
// 문단 하나를 inserted 서브트리로 정확히 잡는다.
func TestPartMissingAndInsertion(t *testing.T) {
	a := testutil.MinimalDocx([]string{"한 줄"})
	b := testutil.MinimalDocx([]string{"한 줄", "두 줄"})
	pa, err := opc.OpenBytes(a)
	if err != nil {
		t.Fatalf("OpenBytes a: %v", err)
	}
	pb, err := opc.OpenBytes(b)
	if err != nil {
		t.Fatalf("OpenBytes b: %v", err)
	}
	da, err := parts.Open(pa)
	if err != nil {
		t.Fatalf("parts.Open a: %v", err)
	}
	db, err := parts.Open(pb)
	if err != nil {
		t.Fatalf("parts.Open b: %v", err)
	}
	rep, err := diff.Compare(da, db, "a.docx", "b.docx", nil)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if rep.Summary.Inserted != 1 {
		t.Fatalf("inserted=%d (기대 1 — '두 줄' 문단이 끝에 추가됐다): %+v", rep.Summary.Inserted, rep.Diffs)
	}
	if rep.Summary.Structure != 0 {
		t.Fatalf("structure=%d (기대 0 — 정렬이 '두 줄'을 정확히 삽입으로 잡는다): %+v",
			rep.Summary.Structure, rep.Diffs)
	}
	var ins *diff.Diff
	for i := range rep.Diffs {
		if rep.Diffs[i].Kind == "inserted" {
			ins = &rep.Diffs[i]
		}
	}
	if ins == nil || ins.Path != "document/body[1]/p[2]" {
		t.Fatalf("inserted 항목이 없거나 경로가 다르다: %+v", rep.Diffs)
	}
}

// slideCT 는 합성 pptx 컨테이너에서 슬라이드 파트의 ContentType 이다.
const slideCT = "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"

// miniPptx 는 최소 pptx 컨테이너를 만든다. internal/parts/plan_test.go 의
// pptxOf 와 같은 목적이지만, 그 헬퍼는 parts_test 패키지 밖에서 못 써서
// (요청 브리프가 지적한 대로) 여기 diff_test 패키지 안에 따로 둔다.
//
// contentTypesXML·relsXML 을 파라미터로 받는 이유: expected·actual 두 문서가
// 이 두 파트를 **바이트까지 동일하게** 공유하게 해서(같은 문자열을 그대로
// 넘긴다), compareOtherParts 의 1단(바이트 비교)이 이 둘을 조용히 걸러내고
// 테스트가 진짜 보려는 신호(part_missing·ScanAny 폴백)에 노이즈를 안 보탠다.
func miniPptx(t *testing.T, contentTypesXML, sldIdsXML, relsXML string, entries map[string]string) *parts.Document {
	t.Helper()
	all := map[string]string{
		"[Content_Types].xml": contentTypesXML,
		"ppt/presentation.xml": `<?xml version="1.0"?>` +
			`<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<p:sldIdLst>` + sldIdsXML + `</p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			relsXML + `</Relationships>`,
	}
	for n, c := range entries {
		all[n] = c
	}
	p, err := opc.OpenBytes(testutil.ZipOf(all))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	return d
}

// TestPartMissingThreeSitesAndScanAnyFallback 은 최종 리뷰 Important 지적을
// 잠근다 — treeOf 의 ScanAny 폴백 분기와 part_missing 을 만드는 세 지점이
// 전부 커버리지 0이었다(TestPartMissingAndInsertion(개명 전 이름
// TestPartMissingAndStructure)은 이름과 달리 part_missing 을 하나도 만들지
// 않는다 — 문단 수가 다른 경우만 다룬다).
//
// 합성 컨테이너 하나로 넷을 동시에 건다:
//
//  1. expected 계획에 있는 파트가 actual 컨테이너에 물리적으로도 없다
//     (Compare 의 sel 루프 — compare.go:37)
//  2. expected 컨테이너에만 있는 계획 밖 파트 (compareOtherParts 첫 루프 — compare.go:107)
//  3. actual 컨테이너에만 있는 파트 (compareOtherParts 마지막 루프 — compare.go:150)
//  4. expected 계획에는 있지만 actual 계획에는 없는(=actual 의 sldIdLst 가
//     안 가리키는) 파트가 actual 컨테이너에 파일로는 남아있다 — treeOf 가
//     actual.Resolve 실패 후 ScanAny 로 폴백한다 (compare.go:65-70)
//
// expected: 슬라이드 3장(slide1~3) 이 sldIdLst 에도, 파일로도 있다.
// actual: sldIdLst 는 slide1 하나만 가리킨다(진짜 슬라이드 1장짜리 덱). 그
// 위에 slide2.xml 은 파일 자체가 없고(→ 1), slide3.xml 은 파일로는 남아있되
// sldIdLst 가 안 가리키는 오펀이다(→ 4). docProps/core.xml(expected 전용,
// → 2), docProps/app.xml(actual 전용, → 3) 을 더한다.
func TestPartMissingThreeSitesAndScanAnyFallback(t *testing.T) {
	sharedCT := `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Override PartName="/ppt/slides/slide1.xml" ContentType="` + slideCT + `"/>` +
		`<Override PartName="/ppt/slides/slide2.xml" ContentType="` + slideCT + `"/>` +
		`<Override PartName="/ppt/slides/slide3.xml" ContentType="` + slideCT + `"/>` +
		`</Types>`
	sharedRels := `<Relationship Id="rId1" Target="slides/slide1.xml"/>` +
		`<Relationship Id="rId2" Target="slides/slide2.xml"/>` +
		`<Relationship Id="rId3" Target="slides/slide3.xml"/>`

	exp := miniPptx(t, sharedCT,
		`<p:sldId id="1" r:id="rId1"/><p:sldId id="2" r:id="rId2"/><p:sldId id="3" r:id="rId3"/>`,
		sharedRels,
		map[string]string{
			"ppt/slides/slide1.xml": `<p:sld/>`,
			"ppt/slides/slide2.xml": `<p:sld/>`, // actual 에는 파일 자체가 없다 → part_missing #1
			"ppt/slides/slide3.xml": `<p:sld><p:cSld><p:t>세번째</p:t></p:cSld></p:sld>`,
			"docProps/core.xml":     `<coreProps>expected 전용</coreProps>`, // → part_missing #2
		})
	act := miniPptx(t, sharedCT,
		`<p:sldId id="1" r:id="rId1"/>`, // 진짜 슬라이드 1장짜리 덱
		sharedRels,
		map[string]string{
			"ppt/slides/slide1.xml": `<p:sld/>`,
			// slide2.xml 없음 — expected 에만 있는 본문 파트
			"ppt/slides/slide3.xml": `<p:sld><p:cSld><p:t>달라진세번째</p:t></p:cSld></p:sld>`, // 오펀 — sldIdLst 가 안 가리킨다
			"docProps/app.xml":      `<appProps>actual 전용</appProps>`,                    // → part_missing #3
		})

	rep, err := diff.Compare(exp, act, "exp.pptx", "act.pptx", nil)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	// 실측(관찰 후 단언): part_missing 3(위 넷 중 1·2·3) + text 1(slide3, 폴백
	// 경유) + deleted 2. ppt/presentation.xml 도 계획 밖 파트라 compareOtherParts
	// 를 지나는데, expected 의 sldIdLst 는 sldId 3개(id=1,2,3), actual 은 1개
	// (id=1)다. 정렬은 sldId[1]을 앞 공통으로 매칭하고 남은 sldId[2]·sldId[3]을
	// alignMiddle 의 'd' 구간(위치로는 하나로 뭉친 구간)에 담는데, 그 구간을
	// 서브트리 단위로 펼치는 루프(compare.go alignChildren 의 'd' 분기)가 구간
	// 안 서브트리 개수만큼 항목을 낸다 — sldId 는 형제이지 하나의 서브트리가
	// 아니므로 2건이다. LCS 이전에는 이 차이가 "노드 수가 다르다" structure
	// 하나로 뭉뚱그려졌지만 지금은 무엇이 없어졌는지(sldId 2개)를 정확히 말해
	// Total 이 5에서 6으로 는다. [Content_Types].xml 과 rels 는 두 문서가 같은
	// 문자열을 공유해 바이트 동일 — 1단에서 걸러져 항목이 안 남는다.
	want := diff.Summary{
		Text: 1, Attr: 0, Elem: 0, Structure: 0, Deleted: 2,
		PartContent: 0, PartMissing: 3, Total: 6, VolatileOnly: 0,
	}
	if rep.Summary != want {
		t.Fatalf("요약이 다르다\n  실제 %+v\n  기대 %+v\n  항목: %+v", rep.Summary, want, rep.Diffs)
	}

	// part_missing 3건 — 세 지점이 각각 하나씩 냈다.
	if rep.Summary.PartMissing != 3 {
		t.Fatalf("part_missing=%d (기대 3): %+v", rep.Summary.PartMissing, rep.Diffs)
	}
	gotMissing := map[string]bool{}
	for _, d := range rep.Diffs {
		if d.Kind == "part_missing" {
			gotMissing[d.Part] = true
		}
	}
	for _, want := range []string{"ppt/slides/slide2.xml", "docProps/core.xml", "docProps/app.xml"} {
		if !gotMissing[want] {
			t.Errorf("part_missing 에 %s 가 없다: %+v", want, rep.Diffs)
		}
	}

	// ScanAny 폴백: slide3.xml 은 actual 의 계획에 없어 treeOf 가 actual.Resolve
	// 실패 후 ScanAny 로 폴백한다. 폴백이 안 됐다면(예: 조용히 건너뛰거나 에러로
	// 죽었다면) 아래 text 항목이 나올 수 없다 — 이 항목의 존재 자체가 폴백이
	// 실행돼 내용까지 비교했다는 증거다.
	var slide3Text *diff.Diff
	for i := range rep.Diffs {
		if rep.Diffs[i].Kind == "text" && rep.Diffs[i].Part == "ppt/slides/slide3.xml" {
			slide3Text = &rep.Diffs[i]
		}
	}
	if slide3Text == nil {
		t.Fatalf("slide3.xml 의 text 항목이 없다 — ScanAny 폴백이 실행됐다면 나와야 한다: %+v", rep.Diffs)
	}
	// scope 는 여전히 "body" 다 — ScanAny 로 스캔했더라도 이 파트는 expected
	// 계획에 있는 본문 파트이고, Compare 의 sel 루프가 그 사실을 근거로
	// compareTrees 를 "body" scope 로 부른다(compare.go:49). ScanAny 는 스캔
	// 방법을 바꿀 뿐 scope 판정과는 무관하다.
	if slide3Text.Scope != "body" {
		t.Errorf("scope=%q (기대 body — ScanAny 로 스캔해도 expected 계획에 있는 본문 파트다)", slide3Text.Scope)
	}
	if slide3Text.Expected == nil || *slide3Text.Expected != "세번째" {
		t.Errorf("expected=%v (기대 %q)", slide3Text.Expected, "세번째")
	}
	if slide3Text.Actual == nil || *slide3Text.Actual != "달라진세번째" {
		t.Errorf("actual=%v (기대 %q)", slide3Text.Actual, "달라진세번째")
	}
}

// TestD3LocalityOfPatchedDocument 는 apply 로 한 곳만 고친 문서를 원본과
// 비교하면 그 한 곳만 나오는지 본다.
//
// 설계 §5 의 핵심 주장을 증명한다 — 주 용도(원본 vs panto 가 패치한 것)에서는
// I2(국소성) 덕에 계획 밖 파트가 바이트 동일이라 노이즈가 0 이다. 이것이 말이
// 아니라 테스트여야 §5 의 3단 거르기가 정당해진다.
func TestD3LocalityOfPatchedDocument(t *testing.T) {
	src := filepath.Join("..", "..", "testdata", "real", "deck-a.pptx")
	orig, err := opc.Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	patched, err := opc.OpenBytes(orig.Source())
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	const target = "sld/cSld[1]/spTree[1]/sp[1]/txBody[1]/p[1]/r[1]/t[1]"
	errs, err := patch.Apply(patched, patch.Patch{Ops: []patch.Op{{
		Op: "setText", Part: "pptx/slide[2]", Path: target, Text: patch.Str("바뀐 제목"),
	}}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("패치가 거절됐다: %+v", errs)
	}
	out, err := patched.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	np, err := opc.OpenBytes(out)
	if err != nil {
		t.Fatalf("결과 열기: %v", err)
	}
	ed, err := parts.Open(orig)
	if err != nil {
		t.Fatalf("parts.Open 원본: %v", err)
	}
	ad, err := parts.Open(np)
	if err != nil {
		t.Fatalf("parts.Open 결과: %v", err)
	}

	rep, err := diff.Compare(ed, ad, "원본", "패치본", nil)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	want := diff.Summary{Text: 1, Total: 1}
	if rep.Summary != want {
		t.Fatalf("요약이 다르다\n  실제 %+v\n  기대 %+v\n  항목: %+v",
			rep.Summary, want, rep.Diffs)
	}
	d := rep.Diffs[0]
	if d.Scope != "body" || d.Part != "ppt/slides/slide2.xml" || d.Path != target {
		t.Fatalf("엉뚱한 곳을 짚는다: %+v", d)
	}
	if d.Actual == nil || *d.Actual != "바뀐 제목" {
		t.Fatalf("actual=%v (기대 %q)", d.Actual, "바뀐 제목")
	}
}

// TestL1InsertionHonesty 는 문단 하나를 삽입했을 때 **거짓 text 항목이 없는지**
// 본다. 이 슬라이스의 존재 이유다.
//
// 위치 정렬은 삽입 뒤의 모든 노드를 서로 다른 것과 짝지어서, 문단 하나를
// 끼워 넣었을 뿐인데 "셋째 줄 → 새로 낀 줄" 이라는 text 항목을 냈다. 아무도
// 그 텍스트를 바꾸지 않았다 — 그 출력을 믿는 에이전트는 정확히 틀린 패치를 쓴다.
func TestL1InsertionHonesty(t *testing.T) {
	two := testutil.MinimalDocx([]string{"첫 줄", "셋째 줄"})
	three := testutil.MinimalDocx([]string{"첫 줄", "새로 낀 줄", "셋째 줄"})
	pa, err := opc.OpenBytes(two)
	if err != nil {
		t.Fatalf("OpenBytes two: %v", err)
	}
	pb, err := opc.OpenBytes(three)
	if err != nil {
		t.Fatalf("OpenBytes three: %v", err)
	}
	da, err := parts.Open(pa)
	if err != nil {
		t.Fatalf("parts.Open two: %v", err)
	}
	db, err := parts.Open(pb)
	if err != nil {
		t.Fatalf("parts.Open three: %v", err)
	}
	rep, err := diff.Compare(da, db, "two.docx", "three.docx", nil)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if rep.Summary.Text != 0 {
		t.Fatalf("삽입인데 text 항목이 %d개다 — 거짓말이다: %+v", rep.Summary.Text, rep.Diffs)
	}
	if rep.Summary.Inserted != 1 {
		t.Fatalf("inserted=%d (기대 1 — 문단 하나가 서브트리 하나다): %+v",
			rep.Summary.Inserted, rep.Diffs)
	}
	if rep.Summary.Structure != 0 {
		t.Fatalf("정렬이 됐는데 structure 가 %d개다: %+v", rep.Summary.Structure, rep.Diffs)
	}
	var ins *diff.Diff
	for i := range rep.Diffs {
		if rep.Diffs[i].Kind == "inserted" {
			ins = &rep.Diffs[i]
		}
	}
	if ins == nil {
		t.Fatal("inserted 항목이 없다")
	}
	// 경로는 **실제 기준**이다 — 삽입된 문단은 실제 문서의 p[2] 다.
	if ins.Path != "document/body[1]/p[2]" {
		t.Fatalf("inserted 경로가 %q (기대 %q)", ins.Path, "document/body[1]/p[2]")
	}
	if ins.Detail == "" {
		t.Fatal("detail 이 비었다 — 서브트리가 몇 노드인지 말해야 한다")
	}
}
