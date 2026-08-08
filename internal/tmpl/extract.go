package tmpl

import (
	"fmt"
	"strconv"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// Extract 는 같은 양식 문서 N벌에서 템플릿과 스키마를 뽑는다.
// pkgs[0] 이 베이스이며 템플릿은 베이스를 기반으로 만들어진다.
//
// 반환된 []patch.Error 가 비어있지 않으면 템플릿·스키마는 nil 이다.
func Extract(pkgs []*opc.Package, names []string) (*opc.Package, *Schema, []patch.Error, error) {
	if len(pkgs) < 2 {
		return nil, nil, []patch.Error{{
			Path:   firstPartName(pkgs),
			Reason: "too_few_documents",
			Detail: fmt.Sprintf("문서 %d벌 — 가변부를 판별하려면 2벌 이상이 필요하다", len(pkgs)),
		}}, nil
	}
	if len(names) != len(pkgs) {
		return nil, nil, nil, fmt.Errorf("문서 %d개에 이름 %d개", len(pkgs), len(names))
	}

	docs := make([]*parts.Document, len(pkgs))
	for i, p := range pkgs {
		d, err := parts.Open(p)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", names[i], err)
		}
		docs[i] = d
	}

	base := docs[0]
	basePlan := base.Parts()

	// 1) 파트 집합 비교 — 계획의 (Name, Ref, Root) 열이 완전히 일치해야 한다.
	//    노드 비교는 파트가 대응된다는 전제 위에서만 의미가 있으므로 이걸 먼저 본다.
	for i := 1; i < len(docs); i++ {
		if e := diffPartSet(basePlan, docs[i].Parts(), names[0], names[i]); e != nil {
			return nil, nil, []patch.Error{*e}, nil
		}
	}

	// 2) 파트별로 구조 정렬 → diffMarkup → 가변부 판별.
	//    키 번호는 파트를 가로질러 이어진다 — 스키마의 데이터 파일이
	//    {"k1": ..., "k2": ...} 형태의 평평한 맵이라 파트별로 번호가 겹치면 충돌한다.
	var keys []Key
	var ops []patch.Op
	for _, pt := range basePlan {
		trees := make([]*xmlscan.Tree, len(docs))
		for i, d := range docs {
			tr, err := d.Tree(pt.Name)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%s: %w", names[i], err)
			}
			trees[i] = tr
		}
		baseTree := trees[0]

		for i := 1; i < len(trees); i++ {
			if e := diffStructure(baseTree, trees[i], names[0], names[i]); e != nil {
				return nil, nil, []patch.Error{*e}, nil
			}
		}

		for idx, bn := range baseTree.Nodes {
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
			keys = append(keys, Key{Key: key, Part: pt.Name, Path: bn.Path, Samples: samples})
			ops = append(ops, patch.Op{Op: "setText", Part: pt.Name, Path: bn.Path, Text: "{{" + key + "}}"})
		}
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

// firstPartName 은 too_few_documents 에러의 Path 로 쓸 파트명을 낸다.
// 문서가 하나뿐이면 그 문서의 첫 본문 파트, 아예 없으면 빈 문자열이다.
// 파트를 못 여는(미지원 포맷) 경우도 조용히 빈 문자열로 넘어간다 — 이 에러의
// 요점은 "문서 수가 부족하다"지 포맷 판정이 아니다.
func firstPartName(pkgs []*opc.Package) string {
	if len(pkgs) != 1 {
		return ""
	}
	d, err := parts.Open(pkgs[0])
	if err != nil {
		return ""
	}
	ps := d.Parts()
	if len(ps) == 0 {
		return ""
	}
	return ps[0].Name
}

// diffPartSet 은 두 문서의 파트 계획이 같은지 본다 — Name·Ref·Root 열 전부.
// 파트 집합이 다르면(포맷이 다르거나 파트 수가 다르면) 최초로 갈린 파트를 지목한다.
func diffPartSet(a, b []parts.Part, an, bn string) *patch.Error {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return &patch.Error{
				Path:   a[i].Name,
				Reason: "structure_mismatch",
				Detail: fmt.Sprintf("%s 는 %+v, %s 는 %+v", an, a[i], bn, b[i]),
			}
		}
	}
	if len(a) != len(b) {
		longer, name, short := a, an, len(b)
		if len(b) > len(a) {
			longer, name, short = b, bn, len(a)
		}
		return &patch.Error{
			Path:   longer[short].Name,
			Reason: "structure_mismatch",
			Detail: fmt.Sprintf("%s 에만 있는 파트 (파트 수 %d vs %d)", name, len(a), len(b)),
		}
	}
	return nil
}

// diffStructure 는 두 트리의 경로 순열이 같은지 본다.
func diffStructure(a, b *xmlscan.Tree, an, bn string) *patch.Error {
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

// diffMarkup 은 요소 자신의 마크업(타입 + 휘발성 제외 속성 + 텍스트 요소가 아닌
// 요소의 직접 텍스트)이 같은지 본다.
// 자손의 텍스트는 여기서 보지 않는다 — 그건 가변부 판별의 몫이다.
func diffMarkup(bn xmlscan.Node, trees []*xmlscan.Tree, idx int, names []string) *patch.Error {
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
		// 텍스트 요소(로컬명 t)가 아닌 요소의 텍스트도 비교한다.
		//
		// 가변부 판별은 로컬명 t 만 본다. 여기서 걸러내지 않으면 w:instrText
		// (메일머지·페이지 번호·상호참조·TOC 를 Word 가 인코딩하는 요소) 같은
		// 다른 요소의 텍스트 차이가 어느 쪽 검사에도 안 걸려 D₁ 의 것이 조용히
		// 채택된다. 그러면 I4a 가 무의미해진다 (spec §8).
		//
		// 스캐너는 CharData 를 가장 안쪽 프레임에만 쌓으므로, 여기 걸리는 것은
		// 요소가 **직접** 품은 텍스트뿐이다. 비단말 요소에서는 요소 사이 공백이
		// 그것인데, Word 는 document.xml 을 들여쓰기 없이 쓰므로 보통 빈 문자열이다.
		// 들여쓰기가 서로 다른 문서는 여기서 거절된다 — 조용히 한쪽을 고르는 것보다 낫다.
		// 문구는 포맷 특정 요소 이름을 대지 않는다 — 텍스트 요소는 docx 가 w:t,
		// pptx 가 a:t 다. 대신 손에 든 노드의 Type 을 그대로 말한다.
		if bn.Type != "t" && other.Text != bn.Text {
			return &patch.Error{
				Path:   bn.Path,
				Reason: "nontext_diff",
				Detail: fmt.Sprintf("가변부로 다루지 않는 요소 %s 의 직접 텍스트가 다르다 (%s 는 %q, %s 는 %q)",
					bn.Type, names[0], bn.Text, names[i], other.Text),
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
// 요소 자체가 VolatileElements 에 있으면 그 요소의 속성만 통째로 비교에서
// 뺀다 — Type·직접 텍스트·자손은 diffMarkup·diffStructure 가 그대로 본다.
func stableAttrs(n xmlscan.Node) []xmlscan.Attr {
	if VolatileElements[n.Type] {
		return nil
	}
	out := make([]xmlscan.Attr, 0, len(n.Attrs))
	for _, a := range n.Attrs {
		if isVolatile(a.Name) {
			continue
		}
		out = append(out, a)
	}
	return out
}
