package diff

import (
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// TestCompareAttrsIgnoresNamespaceDeclarations 는 설계 §6 의 "네임스페이스
// 선언은 비교에서 뺀다" 규칙을 잠근다.
//
// 실제 픽스처(form-a/b.docx, deck-a/b.pptx)는 word/document.xml 의 네임스페이스
// 선언 접두사가 완전히 같아 이 규칙이 없어도 D2 가 통과한다 — 규칙을
// 지워도 테스트가 안 알아챈다. 그래서 접두사가 다른 xmlns 선언을 합성해
// compareAttrs 를 직접 부른다 (package diff 내부 테스트라 소문자 함수를
// 그대로 쓸 수 있다).
func TestCompareAttrsIgnoresNamespaceDeclarations(t *testing.T) {
	x := xmlscan.Node{
		Path: "document",
		Type: "document",
		Attrs: []xmlscan.Attr{
			{Name: "w", NS: "xmlns", Value: "http://ns"}, // 접두사 w
			{Name: "val", Value: "same"},                 // 진짜 속성 — 값 같음
		},
	}
	y := xmlscan.Node{
		Path: "document",
		Type: "document",
		Attrs: []xmlscan.Attr{
			{Name: "x", NS: "xmlns", Value: "http://ns"}, // 같은 네임스페이스, 다른 접두사
			{Name: "val", Value: "same"},
		},
	}

	rep := &Report{}
	compareAttrs(rep, "body", "test.xml", x, y)
	if len(rep.Diffs) != 0 {
		t.Fatalf("네임스페이스 선언 접두사 차이만 있는데 %d개 항목이 나왔다: %+v", len(rep.Diffs), rep.Diffs)
	}

	// 필터가 진짜 속성까지 함께 삼키지 않는지 — 같은 두 노드에 진짜 속성
	// 차이를 하나 더하면 그건 항목으로 나와야 한다.
	x2 := xmlscan.Node{
		Path: "document",
		Type: "document",
		Attrs: []xmlscan.Attr{
			{Name: "w", NS: "xmlns", Value: "http://ns"},
			{Name: "val", Value: "a"},
		},
	}
	y2 := xmlscan.Node{
		Path: "document",
		Type: "document",
		Attrs: []xmlscan.Attr{
			{Name: "x", NS: "xmlns", Value: "http://ns"},
			{Name: "val", Value: "b"}, // 진짜 값이 다르다
		},
	}
	rep2 := &Report{}
	compareAttrs(rep2, "body", "test.xml", x2, y2)
	if len(rep2.Diffs) != 1 {
		t.Fatalf("진짜 속성 차이가 걸러졌다 — %d개 항목 (기대 1개): %+v", len(rep2.Diffs), rep2.Diffs)
	}
	if rep2.Diffs[0].Kind != "attr" || rep2.Diffs[0].Attr != "val" {
		t.Fatalf("잘못된 항목이 나왔다: %+v", rep2.Diffs[0])
	}
}

// TestCompareTreesPreservesTrailingWhitespace 는 설계 §6/브리프의 "텍스트
// 공백을 다듬지 않는다 — xml:space=\"preserve\" 인 w:t 의 끝 공백은 내용이다"
// 규칙을 잠근다.
//
// D2 가 비교하는 6개 경로 중 끝 공백이 걸린 노드가 하나도 없어 TrimSpace 를
// 끼워 넣어도 기존 테스트는 못 알아챈다. 그래서 끝 공백만 다른 합성 노드로
// compareTrees 를 직접 부른다.
func TestCompareTreesPreservesTrailingWhitespace(t *testing.T) {
	a := &xmlscan.Tree{Nodes: []xmlscan.Node{
		{Path: "document/p[1]/r[1]/t[1]", Type: "t", Text: "값 "}, // 끝 공백 있음
	}}
	b := &xmlscan.Tree{Nodes: []xmlscan.Node{
		{Path: "document/p[1]/r[1]/t[1]", Type: "t", Text: "값"}, // 끝 공백 없음
	}}

	rep := &Report{}
	compareTrees(rep, "body", "test.xml", a, b)
	if len(rep.Diffs) != 1 {
		t.Fatalf("끝 공백 차이가 무시됐다 — %d개 항목 (기대 1개): %+v", len(rep.Diffs), rep.Diffs)
	}
	if rep.Diffs[0].Kind != "text" {
		t.Fatalf("kind=%q (기대 text)", rep.Diffs[0].Kind)
	}
	if rep.Diffs[0].Expected == nil || *rep.Diffs[0].Expected != "값 " {
		t.Fatalf("expected=%v (기대 %q)", rep.Diffs[0].Expected, "값 ")
	}
	if rep.Diffs[0].Actual == nil || *rep.Diffs[0].Actual != "값" {
		t.Fatalf("actual=%v (기대 %q)", rep.Diffs[0].Actual, "값")
	}
}
