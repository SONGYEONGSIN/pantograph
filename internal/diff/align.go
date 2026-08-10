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

// maxCells 는 LCS DP 표의 칸 수 상한이다.
//
// 표가 형제 수의 곱이라 입력 XML 이 작업량·메모리를 곱으로 부풀릴 수 있다 —
// plan.go 가 sldIdLst 중복으로 이미 막아둔 부류와 같다. 400만 칸이 int32 로
// 16MB 인 것은 **정사각형**(len(a)==len(b))일 때뿐이다 — 실제 할당은
// (len(a)+1)*(len(b)+1) 이라 한쪽이 극단으로 치우치면(예: 400만 대 1) 상한
// 판정(len(a)*len(b))을 통과하고도 약 800만 칸(약 30.5MB)까지 잡을 수 있다.
// 형제 하나가 2000개까지 정상 처리한다는 것은 정사각형 기준이며(실측 최대치
// form-a 의 376개보다 5배 넉넉하다), 그 경우엔 16MB 근사치가 맞다.
const maxCells = 4_000_000

// op 는 정렬 결과의 한 구간이다. 인덱스는 원래 형제 목록 기준이다.
//
//	'e' 두 서브트리가 통째로 같다 — 내려가지 않는다
//	'i' 실제에만 있다 — inserted
//	'd' 기대에만 있다 — deleted
//	'r' 양쪽에 남았다 — 위치로 짝짓고 재귀한다
type op struct {
	tag                        byte
	aStart, aEnd, bStart, bEnd int
}

// alignSiblings 는 두 형제 목록을 정렬한다.
//
// 두 번째 반환값은 **상한을 넘어 정렬을 포기했는지**다. 호출자는 그 사실을
// 항목으로 내야 한다 — 조용히 위치 정렬로 떨어지면 사용자는 정렬이 돌았다고
// 믿는다.
func alignSiblings(a, b []*node) ([]op, bool) {
	// 앞 공통 — 실제 문서는 대부분 여기서 대부분이 걸린다.
	p := 0
	for p < len(a) && p < len(b) && a[p].sig == b[p].sig {
		p++
	}
	// 뒤 공통. 앞에서 이미 센 것을 다시 세지 않도록 남은 길이로 막는다.
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s].sig == b[len(b)-1-s].sig {
		s++
	}

	var ops []op
	if p > 0 {
		ops = append(ops, op{tag: 'e', aStart: 0, aEnd: p, bStart: 0, bEnd: p})
	}
	mid, capped := alignMiddle(a[p:len(a)-s], b[p:len(b)-s], p, p)
	ops = append(ops, mid...)
	if s > 0 {
		ops = append(ops, op{tag: 'e',
			aStart: len(a) - s, aEnd: len(a), bStart: len(b) - s, bEnd: len(b)})
	}
	return ops, capped
}

// alignMiddle 은 공통 앞뒤를 걷어낸 가운데 구간을 정렬한다.
// offA·offB 는 원래 목록에서의 시작 위치이며 반환 인덱스는 원래 기준이다.
func alignMiddle(a, b []*node, offA, offB int) ([]op, bool) {
	switch {
	case len(a) == 0 && len(b) == 0:
		return nil, false
	case len(a) == 0:
		return []op{{tag: 'i', aStart: offA, aEnd: offA, bStart: offB, bEnd: offB + len(b)}}, false
	case len(b) == 0:
		return []op{{tag: 'd', aStart: offA, aEnd: offA + len(a), bStart: offB, bEnd: offB}}, false
	case len(a) > maxCells/len(b):
		// len(a)*len(b) 로 직접 판정하지 않는 이유: len(a)·len(b) 가 둘 다
		// 커지면 그 곱이 32비트 int 에서 오버플로해(음수로 wrap) 이 가드를
		// 무력화할 수 있다. len(b) != 0 은 위 case len(b) == 0 이 보장한다.
		//
		// 상한 초과 — 위치 짝짓기용 r 구간 하나로 낸다. 호출자가 앞에서부터
		// 짝지으므로 이 구간의 결과는 옛 위치 정렬과 같아진다.
		return []op{{tag: 'r',
			aStart: offA, aEnd: offA + len(a), bStart: offB, bEnd: offB + len(b)}}, true
	}

	// LCS 길이표. dp[i][j] = a[i:] 와 b[j:] 의 최장 공통 부분열 길이.
	w := len(b) + 1
	dp := make([]int32, (len(a)+1)*w)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			switch {
			case a[i].sig == b[j].sig:
				dp[i*w+j] = dp[(i+1)*w+j+1] + 1
			case dp[(i+1)*w+j] >= dp[i*w+j+1]:
				dp[i*w+j] = dp[(i+1)*w+j]
			default:
				dp[i*w+j] = dp[i*w+j+1]
			}
		}
	}

	// 표를 되짚어 매칭 쌍을 모은다. 동점이면 a 를 먼저 버린다 —
	// 어느 쪽을 고르든 길이는 같지만 **한쪽으로 고정해야 결정론적이다**(I3).
	var matches [][2]int
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i].sig == b[j].sig:
			matches = append(matches, [2]int{i, j})
			i++
			j++
		case dp[(i+1)*w+j] >= dp[i*w+j+1]:
			i++
		default:
			j++
		}
	}

	ops := make([]op, 0, len(matches)*2+1)
	pa, pb := 0, 0
	gap := func(ai, bj int) {
		switch {
		case ai > pa && bj > pb:
			ops = append(ops, op{tag: 'r',
				aStart: offA + pa, aEnd: offA + ai, bStart: offB + pb, bEnd: offB + bj})
		case ai > pa:
			ops = append(ops, op{tag: 'd',
				aStart: offA + pa, aEnd: offA + ai, bStart: offB + pb, bEnd: offB + pb})
		case bj > pb:
			ops = append(ops, op{tag: 'i',
				aStart: offA + pa, aEnd: offA + pa, bStart: offB + pb, bEnd: offB + bj})
		}
	}
	for _, m := range matches {
		gap(m[0], m[1])
		// 붙어 있는 equal 은 한 구간으로 합친다 — 노드마다 구간을 내면
		// 호출자가 볼 것 없는 구간을 수천 개 받는다.
		if k := len(ops) - 1; k >= 0 && ops[k].tag == 'e' &&
			ops[k].aEnd == offA+m[0] && ops[k].bEnd == offB+m[1] {
			ops[k].aEnd++
			ops[k].bEnd++
		} else {
			ops = append(ops, op{tag: 'e',
				aStart: offA + m[0], aEnd: offA + m[0] + 1,
				bStart: offB + m[1], bEnd: offB + m[1] + 1})
		}
		pa, pb = m[0]+1, m[1]+1
	}
	gap(len(a), len(b))
	return ops, false
}
