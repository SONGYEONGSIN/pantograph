package align

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// tree 는 (경로, 타입, 텍스트) 목록으로 정렬용 트리를 만든다.
// Span 은 부모·자식 관계를 만드는 재료이므로 실제 값을 준다 — BuildTree 가
// Span 포함으로 트리를 세우기 때문에 0 으로 두면 전부 한 노드로 뭉친다.
func tree(spec ...[3]string) *Node {
	nodes := make([]xmlscan.Node, len(spec))
	for i, s := range spec {
		nodes[i] = xmlscan.Node{Path: s[0], Type: s[1], Text: s[2]}
	}
	// Span 은 뒤에서 앞으로 채운다 — 각 노드는 **경로가 자기 밑에 있는**
	// 뒤쪽 연속 노드들을 품는다. 이 계산은 손으로 검산했다: body/p[1]/t[1],
	// body/p[2]/t[1] 같은 형제 구조에서 BuildTree 가 정확히 두 문단을 만든다.
	for i := len(nodes) - 1; i >= 0; i-- {
		end := i*10 + 10
		for j := i + 1; j < len(nodes); j++ {
			if len(nodes[j].Path) > len(nodes[i].Path) &&
				nodes[j].Path[:len(nodes[i].Path)+1] == nodes[i].Path+"/" {
				end = j*10 + 10
			} else {
				break
			}
		}
		nodes[i].Span = xmlscan.Span{Start: i * 10, End: end}
	}
	n := BuildTree(&xmlscan.Tree{Nodes: nodes})
	Sign(n)
	return n
}

// paths 는 노드 목록의 경로를 뽑는다. 기대값과 대조하기 위한 것이다.
func paths(ns []*Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Path
	}
	return out
}

// TestMatchPairsEqualAndReplace 는 'e'(통째로 같음)와 'r'(위치 짝짓기) **둘 다**
// 매칭으로 다루는지 본다.
//
// 'r' 을 빼면 tmpl 의 가변 키가 하나도 안 나온다 — 텍스트가 다른 문단은 서브트리
// 해시가 달라 'e' 가 아니라 'r' 구간에 들어가기 때문이다(설계 §5).
// 'e' 를 안 내려가면 "모든 문서에서 매칭됐는가" 를 답할 수 없다.
func TestMatchPairsEqualAndReplace(t *testing.T) {
	a := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "같음"},
		[3]string{"body/p[2]", "p", ""},
		[3]string{"body/p[2]/t[1]", "t", "기대"},
	)
	b := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "같음"},
		[3]string{"body/p[2]", "p", ""},
		[3]string{"body/p[2]/t[1]", "t", "실제"},
	)
	res := Match(a, b)
	if len(res.OnlyA) != 0 || len(res.OnlyB) != 0 {
		t.Fatalf("구조가 같은데 한쪽에만 있는 것이 나왔다: A=%v B=%v", paths(res.OnlyA), paths(res.OnlyB))
	}
	// 노드 5개가 전부 짝지어져야 한다 — 'e' 서브트리(p[1]) 안쪽도 포함이다.
	if len(res.Pairs) != 5 {
		t.Fatalf("짝이 %d개 (기대 5): %+v", len(res.Pairs), res.Pairs)
	}
	got := map[string]string{}
	for _, p := range res.Pairs {
		got[p.A.Path] = p.B.Path
	}
	for _, want := range []string{"body", "body/p[1]", "body/p[1]/t[1]", "body/p[2]", "body/p[2]/t[1]"} {
		if got[want] != want {
			t.Errorf("%s 가 %q 와 짝지어졌다 (기대 자기 자신)", want, got[want])
		}
	}
}

// TestMatchReportsOnlySideSubtrees 는 한쪽에만 있는 것을 **서브트리 단위로**
// 내는지 본다. 노드마다 내면 문단 하나에 여러 건이 된다.
func TestMatchReportsOnlySideSubtrees(t *testing.T) {
	a := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "하나"},
	)
	b := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "하나"},
		[3]string{"body/p[2]", "p", ""},
		[3]string{"body/p[2]/t[1]", "t", "새로"},
	)
	res := Match(a, b)
	if len(res.OnlyA) != 0 {
		t.Fatalf("기대에만 있는 것이 나왔다: %v", paths(res.OnlyA))
	}
	if len(res.OnlyB) != 1 {
		t.Fatalf("실제에만 있는 서브트리가 %d개 (기대 1 — 문단 하나): %v", len(res.OnlyB), paths(res.OnlyB))
	}
	if res.OnlyB[0].Path != "body/p[2]" {
		t.Fatalf("경로가 %q (기대 body/p[2])", res.OnlyB[0].Path)
	}
	if res.OnlyB[0].Size != 2 {
		t.Fatalf("서브트리 노드 수가 %d (기대 2 — p 와 t)", res.OnlyB[0].Size)
	}
	if len(res.Pairs) != 3 {
		t.Fatalf("짝이 %d개 (기대 3 — body, p[1], t[1]): %v", len(res.Pairs), res.Pairs)
	}
}

// TestMatchStopsAtTypeMismatch 는 타입이 다른 짝의 **안쪽은 짝짓지 않는지** 본다.
// diff 의 comparePair 와 같은 규칙이다 — 서로 다른 요소의 자식을 비교하면
// 뜻이 없다.
func TestMatchStopsAtTypeMismatch(t *testing.T) {
	a := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "안쪽"},
	)
	b := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/tbl[1]", "tbl", ""},
		[3]string{"body/tbl[1]/t[1]", "t", "안쪽"},
	)
	res := Match(a, b)
	// body 와 (p ↔ tbl) 둘만 짝지어져야 한다 — 그 안쪽은 안 본다.
	if len(res.Pairs) != 2 {
		t.Fatalf("짝이 %d개 (기대 2 — 루트와 타입 불일치 쌍까지): %+v", len(res.Pairs), res.Pairs)
	}
}

// TestMatchCappedReportsPositionalFallback 은 리뷰어의 Minor 지적을 잠근다 —
// Siblings 가 상한(MaxCells)을 넘어 위치 정렬로 떨어지면 그 사실이
// MatchResult.Capped 에 실려야 한다. 안 실리면 호출자(tmpl)가 앵커 없는
// 우연한 위치 일치를 가변 키로 오인할 수 있다 — align.go 의 Siblings 계약이
// "조용히 위치 정렬로 떨어지면 사용자는 정렬이 돌았다고 믿는다" 고 못박은
// 지점이다.
//
// internal/diff 의 TestCapExceededProducesStructureNotSilentPositionalFallback
// 과 같은 방식이다 — 형제 2001개씩(2001×2001=4,004,001 > MaxCells)을 전부
// 다른 텍스트로 만들어 앞뒤 공통 잘라내기가 전혀 안 먹게 한다.
func TestMatchCappedReportsPositionalFallback(t *testing.T) {
	const n = 2001 // 2001 × 2001 = 4,004,001 > MaxCells(4,000,000)
	mk := func(prefix string) *Node {
		nodes := make([]xmlscan.Node, 0, n+1)
		nodes = append(nodes, xmlscan.Node{
			Path: "body", Type: "body",
			Span: xmlscan.Span{Start: 0, End: n*10 + 10},
		})
		for i := 0; i < n; i++ {
			start := 1 + i*10
			nodes = append(nodes, xmlscan.Node{
				Path: fmt.Sprintf("body/p[%d]", i+1), Type: "p",
				Text: prefix + strconv.Itoa(i),
				Span: xmlscan.Span{Start: start, End: start + 5},
			})
		}
		root := BuildTree(&xmlscan.Tree{Nodes: nodes})
		Sign(root)
		return root
	}
	a := mk("A")
	b := mk("B") // 접두사가 달라 하나도 안 겹친다 — 앞뒤 잘라내기가 안 먹는다

	res := Match(a, b)

	if len(res.Capped) != 1 {
		t.Fatalf("Capped 가 %d개 (기대 1 — body 하나): %v", len(res.Capped), paths(res.Capped))
	}
	if res.Capped[0].Path != "body" {
		t.Fatalf("Capped 경로가 %q (기대 body)", res.Capped[0].Path)
	}
	// 상한을 넘어도 비교를 멈추지 않는다 — 위치로 짝지어진 Pairs 는 여전히
	// 나온다(diff 쪽 L4 와 같은 정신).
	if len(res.Pairs) != n+1 {
		t.Fatalf("Pairs 가 %d개 (기대 %d — body + 형제 %d개, 위치로 짝지어졌다)", len(res.Pairs), n+1, n)
	}
}

// TestMatchNilBoundaries 는 Match 의 nil 계약을 잠근다.
//
// tmpl.Extract 는 파트에 노드가 하나도 없어(align.BuildTree 가 nil 을 돌려줄
// 때) 생기는 nil 트리를 Match 에 그대로 넘긴다 — 최종 리뷰가 지목한 Critical
// 수정이 이 계약에 기댄다. Match 자신은 이미 nil 을 안전하게 처리하지만,
// 그 계약을 어긴 것은 호출부(extract.go)였고 이 테스트 공백이 그걸 못
// 잡은 이유였다. 특히 Match(nil, x) 가 x 를 통째로 OnlyB 에 담는지가
// 핵심이다 — base 가 0노드일 때 상대 문서의 내용이 unrepresented 로
// 신고되는 경로가 여기 달려있다.
func TestMatchNilBoundaries(t *testing.T) {
	x := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "내용"},
	)

	t.Run("둘_다_nil", func(t *testing.T) {
		res := Match(nil, nil)
		if len(res.Pairs) != 0 || len(res.OnlyA) != 0 || len(res.OnlyB) != 0 {
			t.Fatalf("Match(nil, nil) 이 비어있지 않다: %+v", res)
		}
	})

	t.Run("a가_nil", func(t *testing.T) {
		res := Match(nil, x)
		if len(res.Pairs) != 0 {
			t.Fatalf("Pairs 가 %d개 (기대 0): %+v", len(res.Pairs), res.Pairs)
		}
		if len(res.OnlyA) != 0 {
			t.Fatalf("OnlyA 가 %d개 (기대 0): %v", len(res.OnlyA), paths(res.OnlyA))
		}
		if len(res.OnlyB) != 1 || res.OnlyB[0] != x {
			t.Fatalf("OnlyB 가 x 하나를 통째로 담지 않았다: %v", paths(res.OnlyB))
		}
	})

	t.Run("b가_nil", func(t *testing.T) {
		res := Match(x, nil)
		if len(res.Pairs) != 0 {
			t.Fatalf("Pairs 가 %d개 (기대 0): %+v", len(res.Pairs), res.Pairs)
		}
		if len(res.OnlyB) != 0 {
			t.Fatalf("OnlyB 가 %d개 (기대 0): %v", len(res.OnlyB), paths(res.OnlyB))
		}
		if len(res.OnlyA) != 1 || res.OnlyA[0] != x {
			t.Fatalf("OnlyA 가 x 하나를 통째로 담지 않았다: %v", paths(res.OnlyA))
		}
	})
}
