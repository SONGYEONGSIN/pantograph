package diff_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/diff"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
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
