package tmpl

import (
	"fmt"
	"strconv"

	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/wml"
)

// Extract 는 같은 양식 문서 N벌에서 템플릿과 스키마를 뽑는다.
// pkgs[0] 이 베이스이며 템플릿은 베이스를 기반으로 만들어진다.
//
// 반환된 []patch.Error 가 비어있지 않으면 템플릿·스키마는 nil 이다.
func Extract(pkgs []*opc.Package, names []string) (*opc.Package, *Schema, []patch.Error, error) {
	if len(pkgs) < 2 {
		return nil, nil, []patch.Error{{
			Path:   dump.ScannedPart,
			Reason: "too_few_documents",
			Detail: fmt.Sprintf("문서 %d벌 — 가변부를 판별하려면 2벌 이상이 필요하다", len(pkgs)),
		}}, nil
	}
	if len(names) != len(pkgs) {
		return nil, nil, nil, fmt.Errorf("문서 %d개에 이름 %d개", len(pkgs), len(names))
	}

	trees := make([]*wml.Tree, len(pkgs))
	for i, p := range pkgs {
		content, err := p.Part(dump.ScannedPart)
		if err != nil {
			return nil, nil, nil, err
		}
		tr, err := wml.Scan(content)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", names[i], err)
		}
		trees[i] = tr
	}

	base := trees[0]

	// 1) 구조 정렬 — 경로 집합이 완전히 일치해야 한다
	for i := 1; i < len(trees); i++ {
		if e := diffStructure(base, trees[i], names[0], names[i]); e != nil {
			return nil, nil, []patch.Error{*e}, nil
		}
	}

	// 2) 노드별 비교 — 경로가 같으므로 인덱스가 정렬돼 있다
	var keys []Key
	var ops []patch.Op
	for idx, bn := range base.Nodes {
		if e := diffMarkup(bn, trees, idx, names); e != nil {
			return nil, nil, []patch.Error{*e}, nil
		}
		if bn.Type != "t" {
			continue
		}
		varies := false
		for i := 1; i < len(trees); i++ {
			if trees[i].Nodes[idx].Text != bn.Text {
				varies = true
				break
			}
		}
		if !varies {
			continue
		}
		key := "k" + strconv.Itoa(len(keys)+1)
		samples := make([]string, len(trees))
		for i := range trees {
			samples[i] = trees[i].Nodes[idx].Text
		}
		keys = append(keys, Key{Key: key, Path: bn.Path, Samples: samples})
		ops = append(ops, patch.Op{Op: "setText", Path: bn.Path, Text: "{{" + key + "}}"})
	}

	// 3) 베이스의 사본에 패치를 적용해 템플릿을 만든다
	tp, err := opc.OpenBytes(pkgs[0].Source())
	if err != nil {
		return nil, nil, nil, err
	}
	errs, err := patch.Apply(tp, patch.Patch{Hash: tp.Hash, Ops: ops})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(errs) > 0 {
		return nil, nil, errs, nil
	}

	return tp, &Schema{Base: names[0], Hash: pkgs[0].Hash, Keys: keys}, nil, nil
}

// diffStructure 는 두 트리의 경로 순열이 같은지 본다.
func diffStructure(a, b *wml.Tree, an, bn string) *patch.Error {
	n := len(a.Nodes)
	if len(b.Nodes) < n {
		n = len(b.Nodes)
	}
	for i := 0; i < n; i++ {
		if a.Nodes[i].Path != b.Nodes[i].Path {
			return &patch.Error{
				Path:   a.Nodes[i].Path,
				Reason: "structure_mismatch",
				Detail: fmt.Sprintf("%s 는 %s, %s 는 %s", an, a.Nodes[i].Path, bn, b.Nodes[i].Path),
			}
		}
	}
	if len(a.Nodes) != len(b.Nodes) {
		longer, name, short := a, an, len(b.Nodes)
		if len(b.Nodes) > len(a.Nodes) {
			longer, name, short = b, bn, len(a.Nodes)
		}
		return &patch.Error{
			Path:   longer.Nodes[short].Path,
			Reason: "structure_mismatch",
			Detail: fmt.Sprintf("%s 에만 있는 노드 (노드 수 %d vs %d)", name, len(a.Nodes), len(b.Nodes)),
		}
	}
	return nil
}

// diffMarkup 은 요소 자신의 마크업(타입 + 휘발성 제외 속성)이 같은지 본다.
// 자손의 텍스트는 여기서 보지 않는다 — 그건 가변부 판별의 몫이다.
func diffMarkup(bn wml.Node, trees []*wml.Tree, idx int, names []string) *patch.Error {
	baseAttrs := stableAttrs(bn)
	for i := 1; i < len(trees); i++ {
		other := trees[i].Nodes[idx]
		if other.Type != bn.Type {
			return &patch.Error{
				Path:   bn.Path,
				Reason: "nontext_diff",
				Detail: fmt.Sprintf("%s 는 %s, %s 는 %s", names[0], bn.Type, names[i], other.Type),
			}
		}
		otherAttrs := stableAttrs(other)
		if len(otherAttrs) != len(baseAttrs) {
			return &patch.Error{
				Path:   bn.Path,
				Reason: "nontext_diff",
				Detail: fmt.Sprintf("속성 수가 다르다 (%s: %d, %s: %d)", names[0], len(baseAttrs), names[i], len(otherAttrs)),
			}
		}
		for j := range baseAttrs {
			if baseAttrs[j] != otherAttrs[j] {
				return &patch.Error{
					Path:   bn.Path,
					Reason: "nontext_diff",
					Detail: fmt.Sprintf("속성 %s: %s 는 %q, %s 는 %q",
						baseAttrs[j].Name, names[0], baseAttrs[j].Value, names[i], otherAttrs[j].Value),
				}
			}
		}
	}
	return nil
}

// stableAttrs 는 휘발성 속성을 뺀 속성 목록이다. 원문 순서를 유지한다.
func stableAttrs(n wml.Node) []wml.Attr {
	out := make([]wml.Attr, 0, len(n.Attrs))
	for _, a := range n.Attrs {
		if isVolatile(a.Name) {
			continue
		}
		out = append(out, a)
	}
	return out
}
