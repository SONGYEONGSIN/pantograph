package tmpl

import (
	"fmt"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
)

// placeholder 는 키의 자리표시자 문자열이다.
func placeholder(key string) string { return "{{" + key + "}}" }

// resolveKeyPart 는 키의 Part 를 물리 파트명으로 푼다. 비어 있으면 본문 파트가
// 하나인 문서에 한해 그것으로 간주한다 — patch.Op.Part 의 하위호환 규칙과 같다
// (spec: "비어 있으면 본문 파트가 하나인 문서에 한해 그것으로 간주한다").
// Extract 가 만든 스키마는 Part 를 항상 채우므로 이 분기를 안 타지만, 손으로
// 쓴 단일 파트 문서용 스키마는 Part 없이도 동작해야 한다.
func resolveKeyPart(doc *parts.Document, part string) (string, bool) {
	if part == "" {
		ps := doc.Parts()
		if len(ps) == 1 {
			return ps[0].Name, true
		}
		return "", false
	}
	return doc.Resolve(part)
}

// Values 는 문서에서 스키마 키의 실제 값을 뽑는다.
// I4 검증("원래 값으로 되채우면 원본이 나온다")의 입력을 만든다.
func Values(p *opc.Package, sch *Schema) (map[string]string, error) {
	doc, err := parts.Open(p)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(sch.Keys))
	for _, k := range sch.Keys {
		partName, ok := resolveKeyPart(doc, k.Part)
		if !ok {
			return nil, fmt.Errorf("%s: 파트를 찾을 수 없음 (%q)", k.Key, k.Part)
		}
		// doc.Lookup 을 쓰지 않는다 — 그건 파트 스캔 실패(미지원 압축 방식 등)를
		// bool 로 삼켜버려서, 호출자(CLI)가 *opc.UnsupportedError 를 가려
		// unsupported_container 로 보고하는 경로가 막힌다. Tree 의 원본 error 를
		// 그대로 돌려준다.
		tree, err := doc.Tree(partName)
		if err != nil {
			return nil, err
		}
		n, ok := tree.Lookup(k.Path)
		if !ok {
			return nil, fmt.Errorf("%s: 경로 없음 (%s / %s)", k.Key, partName, k.Path)
		}
		out[k.Key] = n.Text
	}
	return out, nil
}

// Fill 은 템플릿의 자리표시자를 데이터로 채운다. tp 를 제자리에서 수정한다.
//
// 새 엔진이 아니다 — setText 패치를 만들어 patch.Apply 에 넘긴다.
func Fill(tp *opc.Package, sch *Schema, data map[string]string) ([]patch.Error, error) {
	doc, err := parts.Open(tp)
	if err != nil {
		return nil, err
	}

	var errs []patch.Error
	ops := make([]patch.Op, 0, len(sch.Keys))
	for _, k := range sch.Keys {
		v, ok := data[k.Key]
		if !ok {
			errs = append(errs, patch.Error{
				Path:   k.Path,
				Reason: "missing_key",
				Detail: fmt.Sprintf("데이터에 %s 가 없다", k.Key),
			})
			continue
		}
		partName, ok := resolveKeyPart(doc, k.Part)
		if !ok {
			errs = append(errs, patch.Error{
				Path:   k.Path,
				Reason: "template_drift",
				Detail: fmt.Sprintf("템플릿에 파트 %q 가 없다", k.Part),
			})
			continue
		}
		// Values 와 같은 이유로 doc.Lookup 이 아니라 doc.Tree 를 직접 쓴다 —
		// 파트 스캔 실패(미지원 압축 방식 등)는 여기서 error 로 곧장 돌려줘야
		// CLI 가 unsupported_container 로 구분해 보고할 수 있다.
		tree, err := doc.Tree(partName)
		if err != nil {
			return nil, err
		}
		n, ok := tree.Lookup(k.Path)
		if !ok {
			errs = append(errs, patch.Error{
				Path:   k.Path,
				Reason: "template_drift",
				Detail: fmt.Sprintf("템플릿에 %s 경로가 없다", k.Path),
			})
			continue
		}
		if n.Text != placeholder(k.Key) {
			errs = append(errs, patch.Error{
				Path:   k.Path,
				Reason: "template_drift",
				Detail: fmt.Sprintf("자리표시자 %s 를 기대했는데 %q 다", placeholder(k.Key), n.Text),
			})
			continue
		}
		ops = append(ops, patch.Op{Op: "setText", Part: partName, Path: k.Path, Text: patch.Str(v)})
	}
	if len(errs) > 0 {
		return errs, nil
	}

	// 템플릿 파일은 사용자가 따로 열므로 hash 대조는 하지 않는다.
	return patch.Apply(tp, patch.Patch{Ops: ops})
}
