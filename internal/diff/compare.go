package diff

import (
	"bytes"
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

	// 선택자를 줬으면 여기서 끝낸다. 슬라이드 하나를 물었는데 레이아웃 16개
	// 이야기를 듣는 것은 답이 아니다. 그때의 "차이 없음"은 "이 파트에 차이
	// 없음"이지 "재현됐음"이 아니며, 범위를 좁힌 것은 부른 쪽이다 (설계 §3).
	if len(sels) > 0 {
		return rep, nil
	}
	compareOtherParts(rep, expected, actual, expName, actName, inActual)
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

// compareOtherParts 는 계획 밖 파트를 3단으로 거른다 (설계 §5).
//
//  1. 압축 해제 후 바이트 비교 — 같으면 항목 없음
//  2. 다르면 그 파트만 스캔해 본문과 같은 규칙으로 비교
//  3. 스캔이 안 되면(바이너리·파싱 실패) part_content 하나로 내린다
//
// 1단을 압축 바이트로 하지 않는 이유: 그건 "인코딩이 다르다"를 "내용이 다르다"로
// 말하게 되어, 다른 생산자가 같은 내용을 다르게 압축하면 전부 거짓 양성이 된다.
//
// 2단이 없으면 노이즈가 신호를 묻는다 — deck-a vs deck-b 의 계획 밖 차이 16개
// 중 12개(레이아웃 11 + 마스터)가 순전히 휘발성 ID 다. 주 용도(원본 vs panto 가
// 패치한 것)에서는 I2 가 바이트 동일을 보장하므로 1단에서 전부 걸리고 스캔은
// 0번 일어난다.
func compareOtherParts(rep *Report, expected, actual *parts.Document, expName, actName string, inActual map[string]bool) {
	planned := make(map[string]bool)
	for _, pt := range expected.Parts() {
		planned[pt.Name] = true
	}

	names := append([]string(nil), expected.Names()...)
	sort.Strings(names) // 컨테이너 순서가 아니라 사전순 — 사람이 찾기 쉽고 결정론적이다

	for _, n := range names {
		if planned[n] {
			continue // 본문 파트는 이미 봤다
		}
		if !inActual[n] {
			rep.add(Diff{Kind: "part_missing", Part: n,
				Detail: fmt.Sprintf("%s 에만 있다", expName)})
			continue
		}
		xb, err := expected.Bytes(n)
		if err != nil {
			rep.add(Diff{Kind: "part_content", Part: n,
				Detail: fmt.Sprintf("%s 에서 읽지 못했다: %v", expName, err)})
			continue
		}
		yb, err := actual.Bytes(n)
		if err != nil {
			rep.add(Diff{Kind: "part_content", Part: n,
				Detail: fmt.Sprintf("%s 에서 읽지 못했다: %v", actName, err)})
			continue
		}
		if bytes.Equal(xb, yb) {
			continue // 1단
		}
		xt, xerr := expected.ScanAny(n)
		yt, yerr := actual.ScanAny(n)
		if xerr != nil || yerr != nil { // 3단
			rep.add(Diff{Kind: "part_content", Part: n,
				Detail: fmt.Sprintf("스캔할 수 없어 내용만 비교했다 — 다르다 (길이 %d B vs %d B)",
					len(xb), len(yb))})
			continue
		}
		before := len(rep.Diffs) // 2단
		compareTrees(rep, "other", n, xt, yt)
		if len(rep.Diffs) == before {
			// 바이트는 다른데 항목이 하나도 안 나왔다 = 차이가 전부 휘발성이었다.
			// 조용히 넘기면 unzip+diff 로 본 것과 답이 어긋나 보인다.
			rep.Summary.VolatileOnly++
		}
	}

	// 실제에만 있는 파트도 항목이다. 기대에 없는 것을 재현물이 들고 있으면
	// 그것도 차이다.
	inExpected := nameSet(expected.Names())
	actNames := append([]string(nil), actual.Names()...)
	sort.Strings(actNames)
	for _, n := range actNames {
		if !inExpected[n] {
			rep.add(Diff{Kind: "part_missing", Part: n,
				Detail: fmt.Sprintf("%s 에만 있다", actName)})
		}
	}
}

// compareTrees 는 두 트리를 정렬해 비교한다.
//
// 위치 정렬(index 대 index)을 쓰지 않는 이유: 삽입이 하나만 일어나도 그 뒤의
// 모든 노드가 서로 다른 것과 짝지어져, 문단을 끼워 넣었을 뿐인데 "이 텍스트가
// 저 텍스트로 바뀌었다"는 **거짓 항목**을 낸다. 포기하기 전에 거짓말을 한다.
//
// 노드 한 쌍의 비교는 예전 그대로다 — 바뀌는 것은 짝을 만드는 방법뿐이다.
func compareTrees(rep *Report, scope, part string, a, b *xmlscan.Tree) {
	ra, rb := buildTree(a), buildTree(b)
	if ra == nil && rb == nil {
		return
	}
	if ra == nil || rb == nil {
		// 한쪽 파트가 비었다. xmlscan.Scan 은 시작 요소가 하나도 없는
		// XML(선언만 있거나 주석만 있거나 완전히 빈 파일)도 에러 없이 노드
		// 0개 트리로 받아준다 — 실제로 오는 경로다. 계획 밖 파트가 한쪽에서
		// 비면 compareOtherParts 2단(스캔 비교)을 지나 여기 온다. 조용히
		// 넘어가면 빈 파트가 "같다"로 보고된다.
		rep.add(Diff{Kind: "structure", Scope: scope, Part: part,
			Detail: "한쪽 파트에 노드가 없다"})
		return
	}
	sign(ra)
	sign(rb)
	comparePair(rep, scope, part, ra, rb)
}

// comparePair 는 짝지어진 노드 한 쌍을 비교하고 그 자식들을 정렬한다.
func comparePair(rep *Report, scope, part string, x, y *node) {
	if x.Type != y.Type {
		// 같은 자리에 다른 요소가 있다. 그 안을 파고들지 않는다 — 서로 다른
		// 요소의 자식을 비교하면 항목만 늘고 뜻은 없다. 다만 침묵하지는
		// 않는다 — deleted·inserted 가 detail 에 "노드 %d개"로 버려진 무게를
		// 알리듯, elem 도 양쪽 서브트리가 몇 노드였는지 알려야 한다. 안 그러면
		// 실제 문서에서 w:p ↔ w:tbl 교체로 수십~수백 노드가 elem 1건으로
		// 접혀도 소비자는 "거의 같다"로 읽는다.
		rep.add(Diff{Kind: "elem", Scope: scope, Part: part, Path: x.Path,
			Expected: ptr(x.Type), Actual: ptr(y.Type),
			Detail: fmt.Sprintf("타입이 다르다 — 그 안은 비교하지 않았다 (기대 %d노드, 실제 %d노드)", x.size, y.size)})
		return
	}
	// 요소가 직접 품은 텍스트다. w:t·a:t 만이 아니라 <Words>17</Words> 도
	// 여기 걸린다 — docProps 를 비교하려면 그래야 한다.
	// 공백을 다듬지 않는다: xml:space="preserve" 인 w:t 의 끝 공백은 내용이다.
	if x.Text != y.Text {
		rep.add(Diff{Kind: "text", Scope: scope, Part: part, Path: x.Path,
			Expected: ptr(x.Text), Actual: ptr(y.Text)})
	}
	compareAttrs(rep, scope, part, x.Node, y.Node)
	alignChildren(rep, scope, part, x, y)
}

// alignChildren 은 두 노드의 자식 목록을 정렬하고 구간마다 처리한다.
func alignChildren(rep *Report, scope, part string, x, y *node) {
	ops, capped := alignSiblings(x.kids, y.kids)
	if capped {
		rep.add(Diff{Kind: "structure", Scope: scope, Part: part, Path: x.Path,
			Detail: fmt.Sprintf("자식이 너무 많아 정렬을 포기하고 위치로 비교했다 — 기대 %d개, 실제 %d개",
				len(x.kids), len(y.kids))})
	}
	for _, o := range ops {
		switch o.tag {
		case 'e':
			// 서브트리가 통째로 같다. 내려가지 않는다 — 같은 부분은 아예
			// 순회하지 않는다는 뜻이라 부수적으로 빨라진다.
		case 'i':
			for j := o.bStart; j < o.bEnd; j++ {
				rep.add(subtreeDiff("inserted", scope, part, y.kids[j]))
			}
		case 'd':
			for i := o.aStart; i < o.aEnd; i++ {
				rep.add(subtreeDiff("deleted", scope, part, x.kids[i]))
			}
		case 'r':
			// 양쪽에 남았다. 위치로 짝지어 재귀한다 — **이 분기가 기존 결과를
			// 지킨다.** 텍스트만 바뀐 문단은 서브트리 해시가 달라 LCS 매칭에
			// 실패하지만, 같은 구간에 양쪽 다 남아 짝지어지고 재귀하면 그 안에서
			// text 차이를 정확히 찾는다.
			//
			// 다만 이 구간에 매칭 앵커(양쪽에 똑같은 서브트리)가 하나도 없으면
			// 이 슬라이스가 없애려던 거짓말의 창이 다시 열린다 — 삽입만 있으면
			// 뒤에 반드시 앵커가 남아 L1 이 성립하지만, 삽입과 인접 문단 편집이
			// 겹쳐 구간 전체가 앵커 없이 뭉치면 위치로 짝지어진 두 문단이 서로
			// 다른 것일 수 있다. 정확 해시 LCS + 위치 짝짓기로는 원리적으로 못
			// 막는다(유사도 매칭이 필요하며 설계 §3 이 정확 해시를 명시적으로
			// 선택했다) — 알려진 한계이지 이 구현의 결함이 아니다.
			la, lb := o.aEnd-o.aStart, o.bEnd-o.bStart
			m := la
			if lb < m {
				m = lb
			}
			for k := 0; k < m; k++ {
				comparePair(rep, scope, part, x.kids[o.aStart+k], y.kids[o.bStart+k])
			}
			for i := o.aStart + m; i < o.aEnd; i++ {
				rep.add(subtreeDiff("deleted", scope, part, x.kids[i]))
			}
			for j := o.bStart + m; j < o.bEnd; j++ {
				rep.add(subtreeDiff("inserted", scope, part, y.kids[j]))
			}
		}
	}
}

// subtreeDiff 는 서브트리 하나가 통째로 있고 없는 항목을 만든다.
// 경로는 그 서브트리가 실제로 있는 쪽 기준이다 — deleted 면 기대, inserted 면 실제.
func subtreeDiff(kind, scope, part string, n *node) Diff {
	side := "실제"
	if kind == "deleted" {
		side = "기대"
	}
	return Diff{Kind: kind, Scope: scope, Part: part, Path: n.Path,
		Detail: fmt.Sprintf("%s 에만 있는 서브트리 — 노드 %d개", side, n.size)}
}

// compareAttrs 는 속성 이름의 합집합을 돌며 다른 것마다 항목을 하나씩 낸다.
// 게이트는 "속성 수가 다르다" 한 줄로 끝냈지만 그건 어느 속성인지 말하지 않는다.
//
// 네임스페이스 선언(NS == "xmlns")은 뺀다 — 같은 네임스페이스를 다른 접두사로
// 선언한 두 파일은 XML 로서 같다. 내용이 아니다 (설계 §6).
func compareAttrs(rep *Report, scope, part string, x, y xmlscan.Node) {
	xa := attrMap(x)
	ya := attrMap(y)
	names := make([][2]string, 0, len(xa)+len(ya))
	for k := range xa {
		names = append(names, k)
	}
	for k := range ya {
		if _, ok := xa[k]; !ok {
			names = append(names, k)
		}
	}
	// 로컬명 우선, 같으면 NS 차순 — 결정성(I3)과 함께, 항목이 사람이 보기
	// 자연스러운 로컬명 순서로 나오게 한다. 맵 순회 순서가 새면 출력이
	// 비결정적이 된다.
	sort.Slice(names, func(i, j int) bool {
		if names[i][1] != names[j][1] {
			return names[i][1] < names[j][1]
		}
		return names[i][0] < names[j][0]
	})

	for _, k := range names {
		xv, xok := xa[k]
		yv, yok := ya[k]
		if xok && yok && xv == yv {
			continue
		}
		// Attr 필드에는 로컬명만 싣는다(k[1]) — 소비자는 "id" 를 보고 싶지
		// "{http://…}id" 를 보고 싶은 게 아니다. 어느 네임스페이스의 id 인지는
		// path 와 part 가 이미 좁혀준다.
		d := Diff{Kind: "attr", Scope: scope, Part: part, Path: x.Path, Attr: k[1]}
		if xok {
			d.Expected = ptr(xv)
		}
		if yok {
			d.Actual = ptr(yv)
		}
		rep.add(d)
	}
}

// attrMap 은 휘발성과 네임스페이스 선언을 뺀 속성을 (네임스페이스, 로컬명)
// 짝으로 색인한다.
//
// 로컬명만으로 색인하면 안 되는 이유: <p:sldId id="256" r:id="rId2"/> 는
// xmlscan.Attr 두 개를 낳는데, 접두사를 버리는 xmlscan 설계(scan.go 주석 —
// "접두사는 문서마다 다를 수 있어 보존하지 않는다") 때문에 **둘 다
// Name=="id"** 다 — 하나는 슬라이드 정체성(네임스페이스 없음), 하나는
// relationships 네임스페이스의 관계 참조다. 로컬명 키 맵에 넣으면 원문 순서상
// 뒤에 오는 쪽이 앞을 덮어써 슬라이드 정체성 id 가 비교에서 통째로 빠지고,
// 그런데도 diff 는 "차이 없음"을 낸다(최종 리뷰 Critical).
//
// NS 를 키에 넣어도 compareAttrs 가 소비자에게 보여주는 Attr 필드에는 로컬명만
// 싣는다(k[1]) — 어느 네임스페이스인지는 사람이 몰라도 되고, path·part 가
// 이미 위치를 좁혀준다.
func attrMap(n xmlscan.Node) map[[2]string]string {
	m := make(map[[2]string]string)
	for _, a := range parts.StableAttrs(n) {
		if a.NS == "xmlns" {
			continue
		}
		m[[2]string{a.NS, a.Name}] = a.Value
	}
	return m
}
