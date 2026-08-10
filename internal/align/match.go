package align

// Pair 는 정렬이 짝지은 두 노드다. A 는 첫 트리, B 는 둘째 트리의 것이다.
type Pair struct{ A, B *Node }

// Match 는 두 트리를 정렬해 짝지어진 노드 쌍과 한쪽에만 있는 서브트리를 낸다.
//
// **짝짓기 규칙은 diff 의 alignChildren 과 같아야 한다** — 'e'(서브트리가 통째로
// 같다)와 'r'(양쪽에 남아 위치로 짝지어졌다)이 매칭이고, 'i'·'d' 는 한쪽에만
// 있는 것이다. 두 곳이 갈라지면 diff 와 tmpl 이 같은 문서에 다른 답을 낸다 —
// tmpl 설계 T5 가 그것을 테스트로 잠근다.
//
// **'e' 구간도 내려가서 쌍을 만든다.** diff 는 볼 게 없어서 안 내려가지만
// tmpl 은 "이 노드가 **모든** 문서에서 매칭됐는가" 를 알아야 한다 — 어떤
// 문서에서는 'e' 이고 다른 문서에서는 'r' 인 노드는 둘 다에서 매칭된 것이고
// 키 후보다. 'e' 서브트리는 양쪽 모양이 같으므로 나란히 내려가면 된다.
//
// 타입이 다른 짝은 그 안쪽을 짝짓지 않는다 — 서로 다른 요소의 자식을 비교하면
// 뜻이 없다(diff 의 comparePair 와 같은 규칙).
//
// 상한 초과로 정렬을 포기한 형제 목록도 위치로 짝지어진다 — Siblings 가 그때
// 'r' 구간 하나를 내기 때문이다. 호출자가 그 사실을 알아야 하면 Siblings 를
// 직접 부르라.
func Match(a, b *Node) (pairs []Pair, onlyA, onlyB []*Node) {
	if a == nil || b == nil {
		if a != nil {
			onlyA = append(onlyA, a)
		}
		if b != nil {
			onlyB = append(onlyB, b)
		}
		return pairs, onlyA, onlyB
	}

	var walk func(x, y *Node)
	walk = func(x, y *Node) {
		pairs = append(pairs, Pair{A: x, B: y})
		if x.Type != y.Type {
			return
		}
		ops, _ := Siblings(x.Kids, y.Kids)
		for _, o := range ops {
			switch o.Tag {
			case 'e', 'r':
				la, lb := o.AEnd-o.AStart, o.BEnd-o.BStart
				m := la
				if lb < m {
					m = lb
				}
				for k := 0; k < m; k++ {
					walk(x.Kids[o.AStart+k], y.Kids[o.BStart+k])
				}
				// 'e' 는 길이가 같아 꼬리가 없다. 'r' 은 남을 수 있다.
				for i := o.AStart + m; i < o.AEnd; i++ {
					onlyA = append(onlyA, x.Kids[i])
				}
				for j := o.BStart + m; j < o.BEnd; j++ {
					onlyB = append(onlyB, y.Kids[j])
				}
			case 'i':
				for j := o.BStart; j < o.BEnd; j++ {
					onlyB = append(onlyB, y.Kids[j])
				}
			case 'd':
				for i := o.AStart; i < o.AEnd; i++ {
					onlyA = append(onlyA, x.Kids[i])
				}
			}
		}
	}
	walk(a, b)
	return pairs, onlyA, onlyB
}
