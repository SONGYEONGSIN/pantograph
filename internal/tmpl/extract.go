package tmpl

import (
	"fmt"
	"strconv"

	"github.com/SONGYEONGSIN/pantograph/internal/align"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// Extract 는 같은 양식 문서 N벌에서 템플릿과 스키마를 뽑는다.
// pkgs[0] 이 베이스이며 템플릿은 베이스를 기반으로 만들어진다.
//
// allowUnrepresented 가 false 이면 표현 못 하는 서브트리가 하나라도 있을 때
// 거절한다(unrepresented_structure). true 이면 공통부에서만 키를 뽑고 나머지를
// Schema.Unrepresented 에 신고한다(설계 §3).
//
// 반환된 []patch.Error 가 비어있지 않으면 템플릿·스키마는 nil 이다.
func Extract(pkgs []*opc.Package, names []string, allowUnrepresented bool) (*opc.Package, *Schema, []patch.Error, error) {
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

	// 2) 파트별로 base 와 각 문서를 정렬 → diffMarkup → 가변부 판별.
	//    키 번호는 파트를 가로질러 이어진다 — 스키마의 데이터 파일이
	//    {"k1": ..., "k2": ...} 형태의 평평한 맵이라 파트별로 번호가 겹치면 충돌한다.
	var keys []Key
	var ops []patch.Op
	var unrep []Unrepresented

	for _, pt := range basePlan {
		baseTree, err := base.Tree(pt.Name)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", names[0], err)
		}
		// baseRoot 가 nil(파트에 노드가 하나도 없음)이어도 그냥 넘어가지 않는다
		// — 그러면 다른 문서의 내용이 unrepresented 에도 안 걸리고 조용히
		// 사라진다(회귀, 최종 리뷰 Critical). align.Match(nil, root) 는 root
		// 전체를 OnlyB 로 돌려주므로 아래 루프가 자연히 그 문서의 내용을
		// 신고하게 된다.
		baseRoot := align.BuildTree(baseTree)
		if baseRoot != nil {
			align.Sign(baseRoot)
		}

		// 문서마다 base 와 짝짓는다. Extract 는 base 대 각 문서를 따로 보므로
		// 어떤 노드는 doc1 과는 매칭되고 doc2 와는 안 될 수 있다.
		matched := make([]map[*align.Node]*align.Node, len(docs))
		for i := 1; i < len(docs); i++ {
			tr, err := docs[i].Tree(pt.Name)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%s: %w", names[i], err)
			}
			// root 도 nil 일 수 있다(그 문서의 이 파트에 노드가 없음) — 여기서
			// nil 가드 없이 Sign 을 부르면 패닉한다(회귀, 최종 리뷰 Critical,
			// CLI 바이너리로 확정 재현됨). align.Match 는 이미 a==nil||b==nil
			// 을 안전하게 처리하므로 Sign 만 건너뛰면 된다.
			root := align.BuildTree(tr)
			if root != nil {
				align.Sign(root)
			}

			res := align.Match(baseRoot, root)
			m := make(map[*align.Node]*align.Node, len(res.Pairs))
			for _, p := range res.Pairs {
				m[p.A] = p.B
			}
			matched[i] = m

			for _, n := range res.OnlyA {
				unrep = append(unrep, Unrepresented{
					Doc: names[i], Part: pt.Name, Path: n.Path,
					Side: names[0] + " 에만", Nodes: n.Size})
			}
			for _, n := range res.OnlyB {
				unrep = append(unrep, Unrepresented{
					Doc: names[i], Part: pt.Name, Path: n.Path,
					Side: names[i] + " 에만", Nodes: n.Size})
			}

			// Capped 는 "짝지었다" 가 아니라 "같은 자리에 놓였다" 다 — 자식 정렬을
			// 상한 초과로 포기하고 위치로만 묶은 부모 노드들이다. base 좌표계가
			// 정답인 tmpl 에서 그 밑에서 나온 가변 키는 우연한 위치 일치일 수 있어
			// 조용히 신뢰할 수 없다. 그래서 같은 Unrepresented 채널로 신고해
			// 기본 거절에 태운다(실측상 실제 문서의 최대 형제 수 376개는 상한
			// 2000×2000 에 한참 못 미쳐 현재는 이론적 경로다).
			for _, n := range res.Capped {
				unrep = append(unrep, Unrepresented{
					Doc: names[i], Part: pt.Name, Path: n.Path,
					Side: "정렬 상한 초과로 위치 짝짓기(" + names[0] + " vs " + names[i] + ")", Nodes: n.Size})
			}
		}

		// base 를 pre-order 로 돌며 후보를 판정한다.
		// **모든 문서에서 매칭된 노드만** 후보다 — 어느 문서의 값을 sample 로
		// 삼을지 정할 수 없으면 키가 될 수 없다(설계 §5 규칙 1).
		var walk func(n *align.Node) *patch.Error
		walk = func(n *align.Node) *patch.Error {
			nodes := make([]xmlscan.Node, len(docs))
			nodes[0] = n.Node
			all := true
			for i := 1; i < len(docs); i++ {
				o, ok := matched[i][n]
				if !ok {
					all = false
					break
				}
				nodes[i] = o.Node
			}
			if all {
				if e := diffMarkup(nodes, names); e != nil {
					return e
				}
				if n.Type == "t" {
					varies := false
					for i := 1; i < len(docs); i++ {
						if nodes[i].Text != n.Text {
							varies = true
							break
						}
					}
					if varies {
						key := "k" + strconv.Itoa(len(keys)+1)
						samples := make([]string, len(docs))
						for i := range nodes {
							samples[i] = nodes[i].Text
						}
						keys = append(keys, Key{Key: key, Part: pt.Name, Path: n.Path, Samples: samples})
						ops = append(ops, patch.Op{Op: "setText", Part: pt.Name,
							Path: n.Path, Text: patch.Str("{{" + key + "}}")})
					}
				}
			}
			for _, k := range n.Kids {
				if e := walk(k); e != nil {
					return e
				}
			}
			return nil
		}
		// baseRoot 가 nil 이면 후보 자체가 없다 — 위 doc 루프가 이미 그
		// 문서들의 내용을 OnlyB 로 신고했으니 여기서는 더 할 일이 없다.
		if baseRoot != nil {
			if e := walk(baseRoot); e != nil {
				return nil, nil, []patch.Error{*e}, nil
			}
		}
	}

	// 표현 못 하는 것이 있으면 기본은 거절이다 — 단서는 무시할 수 있지만
	// 실패는 무시할 수 없다(설계 §3).
	if len(unrep) > 0 && !allowUnrepresented {
		return nil, nil, []patch.Error{{
			Path:   unrep[0].Part,
			Reason: "unrepresented_structure",
			Detail: fmt.Sprintf("템플릿이 표현하지 못하는 서브트리가 %d개다 (처음: %s 의 %s, %s). "+
				"--allow-unrepresented 를 주면 공통부만 뽑고 나머지를 스키마에 신고한다",
				len(unrep), unrep[0].Doc, unrep[0].Path, unrep[0].Side),
		}}, nil
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

	return tp, &Schema{Base: names[0], Hash: pkgs[0].Hash, Keys: keys, Unrepresented: unrep}, nil, nil
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

// diffMarkup 은 짝지어진 노드들의 마크업이 같은지 본다.
// nodes[0] 이 base 이고 nodes[i] 가 names[i] 문서의 짝이다.
// 본문은 예전 그대로다 — 정렬은 "어느 노드끼리 비교할지" 만 바꾸지
// "무엇을 차이로 볼지" 는 안 바꾼다(설계 §5 규칙 2).
//
// 요소 자신의 마크업(타입 + 휘발성 제외 속성 + 텍스트 요소가 아닌 요소의 직접
// 텍스트)이 같은지 본다. 자손의 텍스트는 여기서 보지 않는다 — 그건 가변부
// 판별의 몫이다.
func diffMarkup(nodes []xmlscan.Node, names []string) *patch.Error {
	bn := nodes[0]
	baseAttrs := parts.StableAttrs(bn)
	for i := 1; i < len(nodes); i++ {
		other := nodes[i]
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
		otherAttrs := parts.StableAttrs(other)
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
