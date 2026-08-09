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
