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

// TestCompareTreesAlignsAcrossDeletionAndFindsDownstreamText 는
// TestCompareTreesDivergingPathStopsAtStructure(최종 리뷰 Important 지적으로
// 커버리지 0이던 "경로가 갈리면 멈춘다" 분기를 잠갔던 테스트)를 대체한다 — 그
// 분기 자체가 이 슬라이스로 없어졌다. LCS 정렬은 갈린 지점을 건너뛰고 뒤를
// 계속 본다.
//
// 옛 테스트처럼 1대1 타입 교체(p → tbl)는 여기 안 쓴다 — 개수가 같은 교체는
// alignMiddle 의 gap() 이 항상 하나의 'r' 구간으로 묶어 comparePair 로 넘기고,
// 타입이 다르면 elem 하나를 내고 내려가지 않는다(설계 결정 — 서로 다른 요소의
// 자식을 비교하면 항목만 늘고 뜻이 없다). 그래서 1대1 교체로는 inserted/deleted
// 가 나올 수 없다 — 개수가 달라야 한다.
//
// 대신 a 에만 있는 문단 하나("사라질문단")를 심어 개수를 갈리게 하고, 그 뒤에
// 양쪽에 동일한 "고정문단"을 재동기화 지점으로 둔 다음, 그 뒤에 텍스트만 다른
// "바뀐값" 문단을 둔다. "고정문단"이 없으면 개수가 안 맞는 가운데 구간 전체가
// 위치로 뭉뚱그려진 'r' 구간 하나가 되어 "사라질문단"과 "바뀐값-실제"가 잘못
// 짝지어진다 — LCS 가 "고정문단"을 정확히 매칭해야 "사라질문단"은 단독 deleted
// 로, "바뀐값" 쌍은 단독 'r'(재귀 비교) 로 갈린다.
//
// 세 가지를 확인한다:
//
//	(a) structure 가 0건이다 — 상한을 넘지 않는 한 정렬은 포기하지 않는다.
//	(b) 갈린 지점(a 에만 있는 "사라질문단")이 deleted 로 잡힌다.
//	(c) 그 뒤에 심어둔 텍스트 차이가 발견된다 — 옛 코드라면 갈린 지점에서
//	    멈춰 이 텍스트 차이를 절대 볼 수 없었다.
//
// buildTree 는 Span 포함 관계로 부모·자식을 구성하므로(align.go 주석) 노드마다
// 실제 Span 을 채운다 — Path 문자열만으로는 트리가 재구성되지 않는다.
func TestCompareTreesAlignsAcrossDeletionAndFindsDownstreamText(t *testing.T) {
	a := &xmlscan.Tree{Nodes: []xmlscan.Node{
		{Path: "document", Type: "document", Span: xmlscan.Span{Start: 0, End: 100}},
		{Path: "document/p[1]", Type: "p", Text: "첫문단", Span: xmlscan.Span{Start: 1, End: 10}},
		{Path: "document/p[2]", Type: "p", Text: "사라질문단", Span: xmlscan.Span{Start: 11, End: 20}}, // a 에만 있다
		{Path: "document/p[3]", Type: "p", Text: "고정문단", Span: xmlscan.Span{Start: 21, End: 30}},  // 재동기화 지점
		{Path: "document/p[4]", Type: "p", Text: "바뀐값-기대", Span: xmlscan.Span{Start: 31, End: 40}},
	}}
	b := &xmlscan.Tree{Nodes: []xmlscan.Node{
		{Path: "document", Type: "document", Span: xmlscan.Span{Start: 0, End: 100}},
		{Path: "document/p[1]", Type: "p", Text: "첫문단", Span: xmlscan.Span{Start: 1, End: 10}},
		{Path: "document/p[2]", Type: "p", Text: "고정문단", Span: xmlscan.Span{Start: 21, End: 30}},
		{Path: "document/p[3]", Type: "p", Text: "바뀐값-실제", Span: xmlscan.Span{Start: 31, End: 40}},
	}}

	rep := &Report{}
	compareTrees(rep, "body", "test.xml", a, b)

	if rep.Summary.Structure != 0 {
		t.Fatalf("structure=%d (기대 0 — 상한을 안 넘었으니 정렬을 포기하지 않는다): %+v",
			rep.Summary.Structure, rep.Diffs)
	}
	if rep.Summary.Deleted != 1 {
		t.Fatalf("deleted=%d (기대 1 — a 에만 있는 '사라질문단'): %+v", rep.Summary.Deleted, rep.Diffs)
	}
	var del *Diff
	for i := range rep.Diffs {
		if rep.Diffs[i].Kind == "deleted" {
			del = &rep.Diffs[i]
		}
	}
	if del == nil || del.Path != "document/p[2]" {
		t.Fatalf("deleted 항목이 없거나 경로가 다르다: %+v", rep.Diffs)
	}

	if rep.Summary.Text != 1 {
		t.Fatalf("text=%d (기대 1 — 갈린 지점 뒤 '바뀐값' 문단의 텍스트 차이): %+v",
			rep.Summary.Text, rep.Diffs)
	}
	var txt *Diff
	for i := range rep.Diffs {
		if rep.Diffs[i].Kind == "text" {
			txt = &rep.Diffs[i]
		}
	}
	if txt == nil {
		t.Fatal("text 항목이 없다 — 갈린 지점 뒤가 비교되지 않았다는 뜻이다")
	}
	if txt.Expected == nil || *txt.Expected != "바뀐값-기대" {
		t.Fatalf("expected=%v (기대 %q)", txt.Expected, "바뀐값-기대")
	}
	if txt.Actual == nil || *txt.Actual != "바뀐값-실제" {
		t.Fatalf("actual=%v (기대 %q)", txt.Actual, "바뀐값-실제")
	}
	if len(rep.Diffs) != 2 {
		t.Fatalf("항목이 %d개다 (기대 2 — deleted 1 + text 1): %+v", len(rep.Diffs), rep.Diffs)
	}
}
