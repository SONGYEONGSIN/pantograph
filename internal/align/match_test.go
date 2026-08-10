package align

import (
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
	pairs, onlyA, onlyB := Match(a, b)
	if len(onlyA) != 0 || len(onlyB) != 0 {
		t.Fatalf("구조가 같은데 한쪽에만 있는 것이 나왔다: A=%v B=%v", paths(onlyA), paths(onlyB))
	}
	// 노드 5개가 전부 짝지어져야 한다 — 'e' 서브트리(p[1]) 안쪽도 포함이다.
	if len(pairs) != 5 {
		t.Fatalf("짝이 %d개 (기대 5): %+v", len(pairs), pairs)
	}
	got := map[string]string{}
	for _, p := range pairs {
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
	pairs, onlyA, onlyB := Match(a, b)
	if len(onlyA) != 0 {
		t.Fatalf("기대에만 있는 것이 나왔다: %v", paths(onlyA))
	}
	if len(onlyB) != 1 {
		t.Fatalf("실제에만 있는 서브트리가 %d개 (기대 1 — 문단 하나): %v", len(onlyB), paths(onlyB))
	}
	if onlyB[0].Path != "body/p[2]" {
		t.Fatalf("경로가 %q (기대 body/p[2])", onlyB[0].Path)
	}
	if onlyB[0].Size != 2 {
		t.Fatalf("서브트리 노드 수가 %d (기대 2 — p 와 t)", onlyB[0].Size)
	}
	if len(pairs) != 3 {
		t.Fatalf("짝이 %d개 (기대 3 — body, p[1], t[1]): %v", len(pairs), pairs)
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
	pairs, _, _ := Match(a, b)
	// body 와 (p ↔ tbl) 둘만 짝지어져야 한다 — 그 안쪽은 안 본다.
	if len(pairs) != 2 {
		t.Fatalf("짝이 %d개 (기대 2 — 루트와 타입 불일치 쌍까지): %+v", len(pairs), pairs)
	}
}
