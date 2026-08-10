package diff

import (
	"fmt"
	"strconv"
	"strings"
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
// align.BuildTree 는 Span 포함 관계로 부모·자식을 구성하므로(align 패키지 주석) 노드마다
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
	// subtreeDiff 의 방향 문구("기대"/"실제")가 뒤바뀌어도 위 어떤 단언도
	// 못 잡는다 — detail 내용까지 확인해 그 방향을 잠근다.
	if !strings.Contains(del.Detail, "기대") {
		t.Fatalf("deleted 항목의 detail 이 '기대' 를 안 담는다: %q", del.Detail)
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

// TestAlignChildrenAsymmetricReplaceTail 은 'r' 구간이 앵커 없이 위치로
// 짝지어지고 양쪽 길이가 다를 때 남는 꼬리(deleted 또는 inserted)를 커버한다.
//
// 이전에는 la != lb 인 'r' 구간을 타는 테스트가 하나도 없었다 —
// TestCompareTreesAlignsAcrossDeletionAndFindsDownstreamText 의 'r' 구간은
// 항상 1대1(la==lb==1)이라 꼬리 루프가 실행되지 않는다. 그래서
// min(la,lb) 를 la 로 바꾸는 변이(꼬리 시작점 계산이 틀렸는데도 안 걸린다)가
// 인덱스 초과 패닉을 내는데도 CI 를 통과했다(보고서 참고).
//
// 두 트리 모두 공통 앞머리(Anchor)로 시작해 align.Siblings 의 공통 접두사
// 잘라내기를 통과시키고, 그 뒤 요소들은 서로 sig 가 겹치지 않게(모두 다른
// 텍스트) 만들어 LCS 매칭이 하나도 안 생기게 한다 — 그러면
// gap(len(a), len(b)) 가 직접 앵커 없는 'r' 구간 하나를 낸다
// (ai>pa && bj>pb).
func TestAlignChildrenAsymmetricReplaceTail(t *testing.T) {
	anchor := xmlscan.Node{Path: "document/p[1]", Type: "p", Text: "고정",
		Span: xmlscan.Span{Start: 1, End: 10}}
	root := func() xmlscan.Node {
		return xmlscan.Node{Path: "document", Type: "document", Span: xmlscan.Span{Start: 0, End: 200}}
	}
	para := func(idx int, text string) xmlscan.Node {
		start := 1 + idx*10
		return xmlscan.Node{Path: fmt.Sprintf("document/p[%d]", idx+1), Type: "p", Text: text,
			Span: xmlscan.Span{Start: start, End: start + 9}}
	}

	t.Run("기대3_실제1", func(t *testing.T) {
		a := &xmlscan.Tree{Nodes: []xmlscan.Node{root(), anchor,
			para(1, "차이있음-기대"), para(2, "지워질2"), para(3, "지워질3")}}
		b := &xmlscan.Tree{Nodes: []xmlscan.Node{root(), anchor,
			para(1, "차이있음-실제")}}

		rep := &Report{}
		compareTrees(rep, "body", "test.xml", a, b)

		if rep.Summary.Deleted != 2 {
			t.Fatalf("deleted=%d (기대 2 — 짝 없는 기대 꼬리 p[3]·p[4]): %+v", rep.Summary.Deleted, rep.Diffs)
		}
		if rep.Summary.Inserted != 0 {
			t.Fatalf("inserted=%d (기대 0): %+v", rep.Summary.Inserted, rep.Diffs)
		}
		if rep.Summary.Structure != 1 {
			t.Fatalf("structure=%d (기대 1 — la=3 lb=1, 앵커 없이 위치로 짝지었다): %+v", rep.Summary.Structure, rep.Diffs)
		}
		if rep.Summary.Text != 1 {
			t.Fatalf("text=%d (기대 1 — 짝지어진 p[2] 쌍의 텍스트 차이): %+v", rep.Summary.Text, rep.Diffs)
		}
	})

	t.Run("기대1_실제3", func(t *testing.T) {
		a := &xmlscan.Tree{Nodes: []xmlscan.Node{root(), anchor,
			para(1, "기대만있음")}}
		b := &xmlscan.Tree{Nodes: []xmlscan.Node{root(), anchor,
			para(1, "실제전용1"), para(2, "실제전용2"), para(3, "실제전용3")}}

		rep := &Report{}
		compareTrees(rep, "body", "test.xml", a, b)

		if rep.Summary.Inserted != 2 {
			t.Fatalf("inserted=%d (기대 2 — 짝 없는 실제 꼬리 p[3]·p[4]): %+v", rep.Summary.Inserted, rep.Diffs)
		}
		if rep.Summary.Deleted != 0 {
			t.Fatalf("deleted=%d (기대 0): %+v", rep.Summary.Deleted, rep.Diffs)
		}
		if rep.Summary.Structure != 1 {
			t.Fatalf("structure=%d (기대 1 — la=1 lb=3, 앵커 없이 위치로 짝지었다): %+v", rep.Summary.Structure, rep.Diffs)
		}
		if rep.Summary.Text != 1 {
			t.Fatalf("text=%d (기대 1 — 짝지어진 p[2] 쌍의 텍스트 차이): %+v", rep.Summary.Text, rep.Diffs)
		}
	})
}

// TestCapExceededProducesStructureNotSilentPositionalFallback 은 최종 리뷰
// Important 지적(I-1)을 잠근다 — L4("정렬이 상한을 넘으면 위치 정렬로
// 떨어지되 그 사실을 structure 항목으로 알린다")가 alignChildren 의
// `if capped { rep.add(...) }` 이음매에서 실제로 지켜지는지, compareTrees
// 경로로 직접 확인한다.
//
// align_test.go 의 TestAlignSiblingsCapFallsBackAndSaysSo 는 align.Siblings 가
// capped=true 를 **돌려주는지**만 본다 — 그 값이 실제로 structure 항목이
// 되는지는 이 파일 어디에도 없었다. 리뷰어가 `if capped` 를
// `if false && capped` 로 바꾸고 전체 스위트를 돌려보니 초록으로 끝났다 —
// L4 를 통째로 삭제해도 아무도 몰랐다는 뜻이다. 이 테스트가 그 이음매를
// 잠근다(RED 확인은 보고서 참조).
//
// 형제 2001개씩(2001×2001 = 4,004,001 > align.MaxCells)을 전부 다른 텍스트로
// 만들어 앞뒤 공통 잘라내기가 전혀 안 먹게 한다. capped 로 떨어지면
// alignChildren 은 structure 항목 하나를 내고, 남은 상한 초과 구간은 위치로
// 짝지어 재귀한다 — 형제 2001개가 전부 자리별로 텍스트만 다르므로 재귀가 text
// 항목 2001개를 낸다.
func TestCapExceededProducesStructureNotSilentPositionalFallback(t *testing.T) {
	const n = 2001 // 2001 × 2001 = 4,004,001 > align.MaxCells(4,000,000)
	mk := func(prefix string) *xmlscan.Tree {
		nodes := make([]xmlscan.Node, 0, n+1)
		nodes = append(nodes, xmlscan.Node{
			Path: "document", Type: "document",
			Span: xmlscan.Span{Start: 0, End: n*10 + 10},
		})
		for i := 0; i < n; i++ {
			start := 1 + i*10
			nodes = append(nodes, xmlscan.Node{
				Path: fmt.Sprintf("document/p[%d]", i+1), Type: "p",
				Text: prefix + strconv.Itoa(i),
				Span: xmlscan.Span{Start: start, End: start + 5},
			})
		}
		return &xmlscan.Tree{Nodes: nodes}
	}
	a := mk("A")
	b := mk("B") // 접두사가 달라 하나도 안 겹친다 — 앞뒤 잘라내기가 안 먹는다

	rep := &Report{}
	compareTrees(rep, "body", "test.xml", a, b)

	if rep.Summary.Structure != 1 {
		t.Fatalf("structure=%d (기대 1 — 상한 초과를 알려야 한다): %+v", rep.Summary.Structure, rep.Diffs)
	}
	if rep.Summary.Text != n {
		t.Fatalf("text=%d (기대 %d — 위치로 짝지어진 형제마다 텍스트가 다르다)", rep.Summary.Text, n)
	}
}

// TestComparePairTypeMismatchDisclosesDiscardedWeight 는 최종 리뷰 Important
// 지적(I-2)을 잠근다 — 타입이 다르면 그 서브트리를 통째로 버리는데, 그 사실의
// 무게(버려진 노드 수)가 elem 항목의 detail 에 실리는지 본다.
//
// 리뷰어 재현: 루트 아래 p(자식 3개, 텍스트 가/나/다) 대 tbl(자식 3개,
// A/B/C) 를 비교하면 옛 코드는 elem 1건에 detail="" 을 낸다 — 자식 3개의
// 텍스트 차이가 통째로 사라졌다는 사실을 아무도 말하지 않는다. deleted·
// inserted 는 detail 에 "노드 %d개"로 버려진 무게를 알리는데 같은 무게를
// 버리는 elem 만 침묵하면, 소비자는 total 1 을 보고 "거의 같다"로 읽는다.
func TestComparePairTypeMismatchDisclosesDiscardedWeight(t *testing.T) {
	mk := func(childType string, texts ...string) *xmlscan.Tree {
		nodes := []xmlscan.Node{
			{Path: "document", Type: "document", Span: xmlscan.Span{Start: 0, End: 100}},
			{Path: "document/" + childType + "[1]", Type: childType, Span: xmlscan.Span{Start: 1, End: 50}},
		}
		for i, txt := range texts {
			start := 2 + i*9
			nodes = append(nodes, xmlscan.Node{
				Path: fmt.Sprintf("document/%s[1]/t[%d]", childType, i+1), Type: "t", Text: txt,
				Span: xmlscan.Span{Start: start, End: start + 5},
			})
		}
		return &xmlscan.Tree{Nodes: nodes}
	}
	a := mk("p", "가", "나", "다")
	b := mk("tbl", "A", "B", "C")

	rep := &Report{}
	compareTrees(rep, "body", "test.xml", a, b)

	if rep.Summary.Elem != 1 {
		t.Fatalf("elem=%d (기대 1): %+v", rep.Summary.Elem, rep.Diffs)
	}
	if rep.Summary.Total != 1 {
		t.Fatalf("total=%d (기대 1 — 자식 비교로 새는 항목이 없어야 한다): %+v", rep.Summary.Total, rep.Diffs)
	}
	d := rep.Diffs[0]
	if d.Kind != "elem" {
		t.Fatalf("kind=%q (기대 elem)", d.Kind)
	}
	if d.Detail == "" {
		t.Fatal("detail 이 비었다 — 버려진 서브트리 크기를 알려야 한다")
	}
	// 두 서브트리 모두 자기 자신(1) + 자식 3개 = 4 노드다.
	if !strings.Contains(d.Detail, "4노드") {
		t.Fatalf("detail 이 버려진 노드 수(4)를 담지 않는다: %q", d.Detail)
	}
}
