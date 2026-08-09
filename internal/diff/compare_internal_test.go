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

// TestAttrMapKeepsNamespaceCollidingLocalNames 는 최종 리뷰 Critical 지적을
// 잠근다 — attrMap 이 로컬명만으로 색인해 <p:sldId id="256" r:id="rId2"/> 같은
// 마크업에서 속성이 조용히 사라지는 문제.
//
// xmlscan.Attr 은 로컬명만 Name 에 담고 접두사는 버린다(scan.go 주석). 그래서
// "id"(네임스페이스 없음, 슬라이드 정체성)와 "r:id"(relationships 네임스페이스,
// 관계 참조)가 스캔 후에는 둘 다 Name=="id"로 남는다. attrMap 이 로컬명만으로
// 맵을 채우면 원문 순서상 뒤에 오는 것이 앞을 덮어써 슬라이드 정체성 id가
// 통째로 비교에서 빠진다 — 그런데도 diff 는 "차이 없음"을 낸다.
//
// 수정 전(로컬명 키)에는 이 테스트가 0개 항목을 낸다: 두 Attrs 슬라이스 모두
// map[string]string{"id": "rId2"}로 좁혀지고(원문 순서상 r:id 가 나중이라 그
// 값이 남는다), 두 문서의 r:id 값이 같으므로 차이가 안 보인다.
func TestAttrMapKeepsNamespaceCollidingLocalNames(t *testing.T) {
	const relNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

	x := xmlscan.Node{
		Path: "presentation/sldIdLst[1]/sldId[1]",
		Type: "sldId",
		Attrs: []xmlscan.Attr{
			{Name: "id", Value: "256"},             // 슬라이드 정체성 (네임스페이스 없음)
			{Name: "id", NS: relNS, Value: "rId2"}, // r:id — 관계 참조. 값은 두 쪽이 같다
		},
	}
	y := xmlscan.Node{
		Path: "presentation/sldIdLst[1]/sldId[1]",
		Type: "sldId",
		Attrs: []xmlscan.Attr{
			{Name: "id", Value: "999"},             // 값이 256→999로 바뀐 슬라이드 정체성
			{Name: "id", NS: relNS, Value: "rId2"}, // r:id 는 그대로
		},
	}

	rep := &Report{}
	compareAttrs(rep, "body", "ppt/presentation.xml", x, y)
	if len(rep.Diffs) != 1 {
		t.Fatalf("id 속성 값이 256→999로 바뀌었는데 %d개 항목이 나왔다 (기대 1개): %+v", len(rep.Diffs), rep.Diffs)
	}
	d := rep.Diffs[0]
	if d.Kind != "attr" || d.Attr != "id" {
		t.Fatalf("잘못된 항목: %+v", d)
	}
	if d.Expected == nil || *d.Expected != "256" {
		t.Fatalf("expected=%v (기대 %q)", d.Expected, "256")
	}
	if d.Actual == nil || *d.Actual != "999" {
		t.Fatalf("actual=%v (기대 %q)", d.Actual, "999")
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
