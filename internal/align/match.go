package align

// Pair 는 정렬이 짝지은 두 노드다. A 는 첫 트리, B 는 둘째 트리의 것이다.
type Pair struct{ A, B *Node }

// MatchResult 는 두 트리를 정렬한 결과다.
type MatchResult struct {
	Pairs []Pair  // 짝지어진 노드 쌍
	OnlyA []*Node // a 에만 있는 서브트리의 루트
	OnlyB []*Node // b 에만 있는 서브트리의 루트

	// Capped 는 자식 정렬을 포기하고 위치로 짝지은 부모 노드들이다.
	//
	// Siblings 가 상한(MaxCells)을 넘으면 LCS 를 포기하고 위치로 떨어지는데,
	// 그때 나오는 Pairs 는 "정렬해서 짝지었다" 가 아니라 "그냥 같은 자리끼리
	// 묶었다" 는 뜻이다. 호출자가 그 차이를 모르면 우연한 위치 일치를 진짜
	// 매칭으로 오인한다 — align.go 의 Siblings 계약이 "조용히 위치 정렬로
	// 떨어지면 사용자는 정렬이 돌았다고 믿는다" 고 못박은 지점이다.
	//
	// 상한 초과는 트리의 임의 깊이에서 날 수 있으므로 재귀 전체에서 모은다.
	Capped []*Node
}

// Match 는 두 트리를 정렬해 짝지어진 노드 쌍, 한쪽에만 있는 서브트리, 그리고
// 상한 초과로 위치 정렬에 떨어진 부모 노드를 낸다.
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
// **상한 초과는 Capped 에 실린다.** Siblings 가 정렬을 포기하고 위치로
// 짝지으면(capped=true) 그 부모 노드(x)를 Capped 에 담는다 — 위치를 알아야
// 호출자가 경로로 신고할 수 있다. 조용히 삼키면 호출자(tmpl)가 앵커 없는
// 우연한 위치 일치를 가변 키로 오인할 수 있다.
func Match(a, b *Node) MatchResult {
	var res MatchResult
	if a == nil || b == nil {
		if a != nil {
			res.OnlyA = append(res.OnlyA, a)
		}
		if b != nil {
			res.OnlyB = append(res.OnlyB, b)
		}
		return res
	}

	var walk func(x, y *Node)
	walk = func(x, y *Node) {
		res.Pairs = append(res.Pairs, Pair{A: x, B: y})
		if x.Type != y.Type {
			return
		}
		ops, capped := Siblings(x.Kids, y.Kids)
		if capped {
			res.Capped = append(res.Capped, x)
		}
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
					res.OnlyA = append(res.OnlyA, x.Kids[i])
				}
				for j := o.BStart + m; j < o.BEnd; j++ {
					res.OnlyB = append(res.OnlyB, y.Kids[j])
				}
			case 'i':
				for j := o.BStart; j < o.BEnd; j++ {
					res.OnlyB = append(res.OnlyB, y.Kids[j])
				}
			case 'd':
				for i := o.AStart; i < o.AEnd; i++ {
					res.OnlyA = append(res.OnlyA, x.Kids[i])
				}
			}
		}
	}
	walk(a, b)
	return res
}
