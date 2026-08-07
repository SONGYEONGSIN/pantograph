package tmpl

import (
	"fmt"

	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/wml"
)

// placeholder 는 키의 자리표시자 문자열이다.
func placeholder(key string) string { return "{{" + key + "}}" }

// Values 는 문서에서 스키마 키의 실제 값을 뽑는다.
// I4 검증("원래 값으로 되채우면 원본이 나온다")의 입력을 만든다.
func Values(p *opc.Package, sch *Schema) (map[string]string, error) {
	content, err := p.Part(dump.ScannedPart)
	if err != nil {
		return nil, err
	}
	tree, err := wml.Scan(content)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(sch.Keys))
	for _, k := range sch.Keys {
		n, ok := tree.Lookup(k.Path)
		if !ok {
			return nil, fmt.Errorf("%s: 경로 없음 (%s)", k.Key, k.Path)
		}
		out[k.Key] = n.Text
	}
	return out, nil
}

// Fill 은 템플릿의 자리표시자를 데이터로 채운다. tp 를 제자리에서 수정한다.
//
// 새 엔진이 아니다 — setText 패치를 만들어 patch.Apply 에 넘긴다.
func Fill(tp *opc.Package, sch *Schema, data map[string]string) ([]patch.Error, error) {
	content, err := tp.Part(dump.ScannedPart)
	if err != nil {
		return nil, err
	}
	tree, err := wml.Scan(content)
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
		ops = append(ops, patch.Op{Op: "setText", Path: k.Path, Text: v})
	}
	if len(errs) > 0 {
		return errs, nil
	}

	// 템플릿 파일은 사용자가 따로 열므로 hash 대조는 하지 않는다.
	return patch.Apply(tp, patch.Patch{Ops: ops})
}
