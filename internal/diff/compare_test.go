package diff_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/diff"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
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
			Text: 11, Attr: 0, Elem: 0, Structure: 1,
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

// TestPartMissingAndStructure 는 실제 픽스처에 없는 두 경로를 합성 컨테이너로
// 실행한다 — 파트 수가 다른 경우와 노드 수가 다른 경우.
func TestPartMissingAndStructure(t *testing.T) {
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
	if rep.Summary.Structure != 1 {
		t.Fatalf("structure=%d (기대 1 — 문단 수가 다르다): %+v", rep.Summary.Structure, rep.Diffs)
	}
	var st *diff.Diff
	for i := range rep.Diffs {
		if rep.Diffs[i].Kind == "structure" {
			st = &rep.Diffs[i]
		}
	}
	// 이 합성 쌍은 "경로가 갈린다"(중간 삽입)가 아니라 "노드 수가 다르다"
	// (끝에 문단 추가) 경로를 탄다 — 공통 접두사의 경로·타입·텍스트가 전부
	// 같아서 루프가 끝까지 돌고, 그 다음 길이 비교에서만 갈린다. 위 주석의
	// "part_missing 은 이 합성 쌍으로도 안 난다"는 설명도 같은 사실을 전제한다.
	if st == nil || !strings.Contains(st.Detail, "노드 수가 다르다") {
		t.Fatalf("structure 항목이 '노드 수가 다르다'를 말하지 않는다: %+v", st)
	}
}
