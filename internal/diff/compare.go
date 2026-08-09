package diff

import (
	"errors"
	"fmt"
	"sort"

	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// ErrFormatMismatch 는 서로 다른 포맷을 비교하려 할 때다.
// 차이가 아니라 무의미한 입력이라 CLI 가 코드 1 로 낸다.
var ErrFormatMismatch = errors.New("포맷이 다르다")

// Compare 는 두 문서의 차이를 끝까지 센다.
// 에러를 내는 것은 비교 자체가 불가능할 때뿐이다 — 차이는 에러가 아니다.
//
// sels 는 본문 파트 비교 범위를 좁히는 선택자이며 **expected 에 대해** 푼다.
// 기대가 기준이므로 무엇을 비교할지는 기대가 정한다.
func Compare(expected, actual *parts.Document, expName, actName string, sels []string) (*Report, error) {
	if expected.Format() != actual.Format() {
		return nil, fmt.Errorf("%w: %s 는 %s, %s 는 %s",
			ErrFormatMismatch, expName, expected.Format(), actName, actual.Format())
	}
	sel, err := expected.Select(sels)
	if err != nil {
		return nil, err
	}

	rep := &Report{Expected: expName, Actual: actName, Diffs: []Diff{}}
	inActual := nameSet(actual.Names())

	for _, pt := range sel {
		if !inActual[pt.Name] {
			rep.add(Diff{Kind: "part_missing", Part: pt.Name,
				Detail: fmt.Sprintf("%s 에만 있다", expName)})
			continue
		}
		et, err := expected.Tree(pt.Name)
		if err != nil {
			return nil, err
		}
		at, err := treeOf(actual, pt.Name)
		if err != nil {
			return nil, err
		}
		compareTrees(rep, "body", pt.Name, et, at)
	}
	return rep, nil
}

// treeOf 는 파트를 스캔한다. 그 파트가 상대 문서의 **계획에도** 있으면 Tree 를,
// 없으면 ScanAny 를 쓴다 — 두 문서의 계획이 다를 수 있고, 그때 Tree 는
// "스캔 대상 파트가 아니다"로 거절한다.
func treeOf(d *parts.Document, name string) (*xmlscan.Tree, error) {
	if _, ok := d.Resolve(name); ok {
		return d.Tree(name)
	}
	return d.ScanAny(name)
}

func nameSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// compareTrees 는 두 트리를 위치 정렬(index 대 index)로 비교한다.
//
// 경로가 갈리면 structure 항목 하나를 내고 **이 파트만** 멈춘다. 정렬 없이
// 계속 가면 그 뒤 전부가 어긋난 쌍이라 항목이 폭발한다. 다른 파트는 계속 본다 —
// 문단 하나 때문에 문서 전체를 못 보게 되면 보정 루프에서 쓸 수 없다.
//
// LCS 정렬로 삽입·삭제·변경을 구분하는 것은 범위 밖이다(설계 §4). 그때 갈아끼울
// 곳은 여기 노드 쌍을 만드는 부분뿐이고, 아래 비교는 그대로 쓴다.
func compareTrees(rep *Report, scope, part string, a, b *xmlscan.Tree) {
	n := len(a.Nodes)
	if len(b.Nodes) < n {
		n = len(b.Nodes)
	}
	for i := 0; i < n; i++ {
		x, y := a.Nodes[i], b.Nodes[i]
		if x.Path != y.Path {
			rep.add(Diff{Kind: "structure", Scope: scope, Part: part, Path: x.Path,
				Detail: fmt.Sprintf("경로가 갈린다 — 기대 %s, 실제 %s. 이 파트의 이후 노드는 비교하지 않았다",
					x.Path, y.Path)})
			return
		}
		if x.Type != y.Type {
			rep.add(Diff{Kind: "elem", Scope: scope, Part: part, Path: x.Path,
				Expected: ptr(x.Type), Actual: ptr(y.Type)})
			continue
		}
		// 요소가 직접 품은 텍스트다. w:t·a:t 만이 아니라 <Words>17</Words> 도
		// 여기 걸린다 — docProps 를 비교하려면 그래야 한다.
		// 공백을 다듬지 않는다: xml:space="preserve" 인 w:t 의 끝 공백은 내용이다.
		if x.Text != y.Text {
			rep.add(Diff{Kind: "text", Scope: scope, Part: part, Path: x.Path,
				Expected: ptr(x.Text), Actual: ptr(y.Text)})
		}
		compareAttrs(rep, scope, part, x, y)
	}
	if len(a.Nodes) != len(b.Nodes) {
		rep.add(Diff{Kind: "structure", Scope: scope, Part: part,
			Path: lastPath(a, b, n),
			Detail: fmt.Sprintf("노드 수가 다르다 — 기대 %d개, 실제 %d개. 앞의 %d개까지만 비교했다",
				len(a.Nodes), len(b.Nodes), n)})
	}
}

// lastPath 는 노드 수가 갈릴 때 가리킬 경로다 — 더 긴 쪽의 첫 초과 노드.
func lastPath(a, b *xmlscan.Tree, n int) string {
	if len(a.Nodes) > n {
		return a.Nodes[n].Path
	}
	if len(b.Nodes) > n {
		return b.Nodes[n].Path
	}
	return ""
}

// compareAttrs 는 속성 이름의 합집합을 돌며 다른 것마다 항목을 하나씩 낸다.
// 게이트는 "속성 수가 다르다" 한 줄로 끝냈지만 그건 어느 속성인지 말하지 않는다.
//
// 네임스페이스 선언(NS == "xmlns")은 뺀다 — 같은 네임스페이스를 다른 접두사로
// 선언한 두 파일은 XML 로서 같다. 내용이 아니다 (설계 §6).
func compareAttrs(rep *Report, scope, part string, x, y xmlscan.Node) {
	xa := attrMap(x)
	ya := attrMap(y)
	names := make([]string, 0, len(xa)+len(ya))
	for k := range xa {
		names = append(names, k)
	}
	for k := range ya {
		if _, ok := xa[k]; !ok {
			names = append(names, k)
		}
	}
	sort.Strings(names) // 맵 순회 순서가 새면 출력이 비결정적이 된다 (I3)

	for _, k := range names {
		xv, xok := xa[k]
		yv, yok := ya[k]
		if xok && yok && xv == yv {
			continue
		}
		d := Diff{Kind: "attr", Scope: scope, Part: part, Path: x.Path, Attr: k}
		if xok {
			d.Expected = ptr(xv)
		}
		if yok {
			d.Actual = ptr(yv)
		}
		rep.add(d)
	}
}

// attrMap 은 휘발성과 네임스페이스 선언을 뺀 속성을 로컬명으로 색인한다.
func attrMap(n xmlscan.Node) map[string]string {
	m := make(map[string]string)
	for _, a := range parts.StableAttrs(n) {
		if a.NS == "xmlns" {
			continue
		}
		m[a.Name] = a.Value
	}
	return m
}
