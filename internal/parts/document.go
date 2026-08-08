package parts

import (
	"fmt"
	"path"
	"strings"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// Document 는 파트 지도와 지연 스캔 캐시를 묶은 것이다.
// 파트를 가로지르는 조회는 전부 여기를 지난다.
type Document struct {
	pkg    *opc.Package
	format string
	plan   []Part
	byName map[string]Part
	byRef  map[string]Part
	trees  map[string]*xmlscan.Tree
	order  []string // 스캔한 순서 — Loaded() 의 결정성을 위해
}

func Open(p *opc.Package) (*Document, error) {
	format, plan, err := Plan(p)
	if err != nil {
		return nil, err
	}
	d := &Document{
		pkg:    p,
		format: format,
		plan:   plan,
		byName: make(map[string]Part, len(plan)),
		byRef:  make(map[string]Part, len(plan)),
		trees:  make(map[string]*xmlscan.Tree),
	}
	for _, pt := range plan {
		d.byName[pt.Name] = pt
		if pt.Ref != "" {
			d.byRef[pt.Ref] = pt
		}
	}
	return d, nil
}

func (d *Document) Format() string { return d.format }

// Hash 는 컨테이너 전체의 sha256 이다.
func (d *Document) Hash() string { return d.pkg.Hash }

// Names 는 컨테이너의 전 엔트리다 (스캔 대상만이 아니다).
func (d *Document) Names() []string { return d.pkg.Names() }

func (d *Document) Parts() []Part {
	out := make([]Part, len(d.plan))
	copy(out, d.plan)
	return out
}

// Tree 는 파트를 스캔한다. 처음 요청될 때만 압축을 풀고, 결과는 캐시된다.
// 50장 덱에서 3장만 고치면 3장만 풀린다.
func (d *Document) Tree(name string) (*xmlscan.Tree, error) {
	if t, ok := d.trees[name]; ok {
		return t, nil
	}
	pt, ok := d.byName[name]
	if !ok {
		return nil, fmt.Errorf("스캔 대상 파트가 아니다: %s", name)
	}
	content, err := d.pkg.Part(name)
	if err != nil {
		return nil, err
	}
	t, err := xmlscan.Scan(content, pt.Root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	for i := range t.Nodes {
		t.Nodes[i].Part = name
	}
	d.trees[name] = t
	d.order = append(d.order, name)
	return t, nil
}

// Loaded 는 실제로 스캔된 파트를 스캔 순서로 돌려준다. 지연 로딩 검증용이다.
func (d *Document) Loaded() []string {
	out := make([]string, len(d.order))
	copy(out, d.order)
	return out
}

// Exists 는 파트가 컨테이너에 있는지 본다 (스캔 대상 여부와 무관하다).
func (d *Document) Exists(name string) bool {
	for _, n := range d.pkg.Names() {
		if n == name {
			return true
		}
	}
	return false
}

// Resolve 는 선택자 하나를 물리 파트명으로 푼다.
// 논리 참조를 먼저 정확 일치로 보고, 없으면 물리 파트명으로 본다 —
// pptx/slide[3] 의 [ 가 glob 문자 클래스로 오독되지 않도록 이 순서가 필요하다.
func (d *Document) Resolve(sel string) (string, bool) {
	if pt, ok := d.byRef[sel]; ok {
		return pt.Name, true
	}
	if pt, ok := d.byName[sel]; ok {
		return pt.Name, true
	}
	return "", false
}

// SelectError 는 선택자가 파트를 하나도 못 골랐을 때의 이유다.
// Reason 은 spec §8 의 어휘 그대로이며 CLI 가 그대로 stdout JSON 에 싣는다.
type SelectError struct {
	Sel    string // 문제의 선택자
	Reason string // part_not_found | ref_not_found | part_not_scannable
	Detail string
}

func (e *SelectError) Error() string { return e.Detail }

// Reject 는 못 푼 선택자의 실패 사유를 가른다.
//
// dump 의 `--part` 와 patch 의 `op.part` 는 같은 질문을 한다 — 이 선택자가
// 어느 파트인가. 판정이 두 곳에 있으면 같은 입력에 두 답이 나온다
// (`ppt/theme/theme1.xml` 이 한쪽에서는 "아무것도 못 골랐다", 다른 쪽에서는
// "컨테이너에 있으나 스캔 대상이 아니다"). 그래서 세 갈래 판정은 여기 한 곳에만 둔다.
//
// 논리 참조 모양인지를 먼저 본다 — 그 모양이면 물리 파트로 존재할 수 없으므로
// 존재 여부를 묻는 것 자체가 무의미하다.
func (d *Document) Reject(sel string) *SelectError {
	switch {
	case isRefShaped(sel):
		return &SelectError{Sel: sel, Reason: "ref_not_found",
			Detail: fmt.Sprintf("논리 참조 %q 가 풀리지 않는다", sel)}
	case d.Exists(sel):
		return &SelectError{Sel: sel, Reason: "part_not_scannable",
			Detail: fmt.Sprintf("%s 는 컨테이너에 있으나 스캔 대상이 아니다", sel)}
	default:
		return &SelectError{Sel: sel, Reason: "part_not_found",
			Detail: fmt.Sprintf("선택자 %q 가 가리키는 파트가 문서에 없다", sel)}
	}
}

// isRefShaped 는 선택자가 논리 참조 모양인지 본다 ("pptx/slide[3]", "docx/document").
// 접두사가 포맷 이름이므로 포맷을 아는 이 패키지에 있어야 한다.
func isRefShaped(s string) bool {
	return strings.HasPrefix(s, "pptx/") || strings.HasPrefix(s, "docx/")
}

// Select 는 --part 선택자들을 계획의 부분집합으로 푼다.
// 선택자가 없으면 계획 전체다. 합집합이며 계획 순서를 유지한다.
// 어느 선택자도 파트를 하나도 못 고르면 거절한다 — 조용한 빈 덤프는 오타를 숨긴다.
func (d *Document) Select(sels []string) ([]Part, error) {
	if len(sels) == 0 {
		return d.Parts(), nil
	}

	picked := make(map[string]bool)
	for _, sel := range sels {
		// picked 는 선택자 전체가 공유하는 합집합이라, 그 크기 변화로는
		// "이 선택자가 뭘 골랐나"를 판정할 수 없다 — 앞 선택자가 이미 고른
		// 파트를 뒤 선택자가 다시 가리키면 picked 는 안 늘지만 그 선택자는
		// 여전히 유효하다. 그래서 이 선택자만의 매치 여부를 따로 센다.
		matched := false

		if name, ok := d.Resolve(sel); ok {
			picked[name] = true
			matched = true
		} else {
			for _, pt := range d.plan {
				ok, err := path.Match(sel, pt.Name)
				if err != nil {
					// glob 문법 오류는 어떤 파트도 고를 수 없다는 뜻이라 사유는
					// 다른 실패와 같은 세 갈래를 지난다. 문법 오류라는 사실만 덧붙인다.
					se := d.Reject(sel)
					se.Detail += fmt.Sprintf(" (glob 문법 오류: %v)", err)
					return nil, se
				}
				if ok {
					picked[pt.Name] = true
					matched = true
				}
			}
		}

		if !matched {
			return nil, d.Reject(sel)
		}
	}

	out := make([]Part, 0, len(picked))
	for _, pt := range d.plan { // 맵이 아니라 계획 순서로 — 결정성
		if picked[pt.Name] {
			out = append(out, pt)
		}
	}
	return out, nil
}
