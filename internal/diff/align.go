package diff

import (
	"crypto/sha256"
	"sort"

	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// node 는 정렬을 위해 트리 모양을 되살린 노드다.
//
// xmlscan 은 평평한 pre-order 를 준다 — 바이트 범위를 다루는 데는 그게 맞지만
// 형제 목록을 정렬하려면 부모·자식이 필요하다.
type node struct {
	xmlscan.Node
	kids []*node
	sig  string // 서브트리 해시 (§3)
	size int    // 자기 자신 포함 서브트리 노드 수
}

// buildTree 는 평평한 pre-order 를 트리로 되살린다.
//
// 부모·자식은 **Span 포함 관계**로 구한다 — pre-order 에서 노드 i 의 서브트리는
// Span 이 Span[i] 안에 들어가는 뒤쪽 연속 구간이다. 경로 문자열 접두사로도
// 되지만(`부모 + "/"` 로 검사하면 p[1] 이 p[10] 과 헷갈리지 않는다) Span 이
// 이미 있고 모호함이 없다.
//
// 노드가 없으면 nil 이다.
func buildTree(t *xmlscan.Tree) *node {
	if len(t.Nodes) == 0 {
		return nil
	}
	var build func(i int) (*node, int) // 노드와 "다음 형제의 인덱스"
	build = func(i int) (*node, int) {
		n := &node{Node: t.Nodes[i], size: 1}
		j := i + 1
		for j < len(t.Nodes) && t.Nodes[j].Span.Start < t.Nodes[i].Span.End {
			k, next := build(j)
			n.kids = append(n.kids, k)
			n.size += k.size
			j = next
		}
		return n, j
	}
	root, _ := build(0)
	return root
}

// sign 은 서브트리 해시를 계산해 트리 전체에 채운다.
//
// 들어가는 것: 요소 타입 + 직접 텍스트 + 안정 속성 + 자식들의 해시.
//
// **안정 속성은 본문 비교와 정확히 같은 규칙이다** — attrMap 을 지나므로
// 휘발성(parts.StableAttrs)과 네임스페이스 선언이 함께 빠진다. 두 곳이 갈라지면
// "해시는 같은데 비교는 다르다"는 모순이 생긴다.
//
// 휘발성이 해시에 들어가면 **pptx 레이아웃은 영영 매칭되지 않는다** —
// creationId 가 문서마다 다르기 때문이다. 그러면 정렬이 통째로 무너진다.
//
// 구분자(0x00·0x01)를 넣는 이유: 이어붙이기만 하면 ("ab","c") 와 ("a","bc") 가
// 같은 해시를 낸다.
func sign(n *node) {
	h := sha256.New()
	h.Write([]byte(n.Type))
	h.Write([]byte{0})
	h.Write([]byte(n.Text))
	h.Write([]byte{0})
	for _, a := range stableAttrTriples(n.Node) {
		h.Write([]byte(a[0]))
		h.Write([]byte{1})
		h.Write([]byte(a[1]))
		h.Write([]byte{1})
		h.Write([]byte(a[2]))
		h.Write([]byte{0})
	}
	for _, k := range n.kids {
		sign(k)
		h.Write([]byte(k.sig))
	}
	n.sig = string(h.Sum(nil))
}

// stableAttrTriples 는 해시에 넣을 속성을 (NS, 로컬명, 값) 으로 결정론적
// 순서로 낸다. 정렬은 compareAttrs 와 같다 — 로컬명 우선, 같으면 NS 차순.
func stableAttrTriples(n xmlscan.Node) [][3]string {
	m := attrMap(n)
	out := make([][3]string, 0, len(m))
	for k, v := range m {
		out = append(out, [3]string{k[0], k[1], v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][1] != out[j][1] {
			return out[i][1] < out[j][1]
		}
		return out[i][0] < out[j][0]
	})
	return out
}
