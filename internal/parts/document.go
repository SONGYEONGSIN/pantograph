package parts

import (
	"fmt"
	"path"

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

func (d *Document) Lookup(name, nodePath string) (xmlscan.Node, bool) {
	t, err := d.Tree(name)
	if err != nil {
		return xmlscan.Node{}, false
	}
	return t.Lookup(nodePath)
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

// Select 는 --part 선택자들을 계획의 부분집합으로 푼다.
// 선택자가 없으면 계획 전체다. 합집합이며 계획 순서를 유지한다.
// 어느 선택자도 파트를 하나도 못 고르면 거절한다 — 조용한 빈 덤프는 오타를 숨긴다.
func (d *Document) Select(sels []string) ([]Part, error) {
	if len(sels) == 0 {
		return d.Parts(), nil
	}

	picked := make(map[string]bool)
	for _, sel := range sels {
		before := len(picked)

		if name, ok := d.Resolve(sel); ok {
			picked[name] = true
		} else {
			for _, pt := range d.plan {
				ok, err := path.Match(sel, pt.Name)
				if err != nil {
					return nil, fmt.Errorf("선택자 %q 가 올바른 glob 이 아니다: %w", sel, err)
				}
				if ok {
					picked[pt.Name] = true
				}
			}
		}

		if len(picked) == before {
			return nil, fmt.Errorf("선택자 %q 가 아무 파트도 고르지 못했다", sel)
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
