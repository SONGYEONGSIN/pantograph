package patch

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/wml"
)

// xmlEscaper 는 텍스트 노드에서 의미를 갖는 세 글자만 이스케이프한다.
// xml.EscapeText 는 개행·탭까지 문자 참조로 바꿔 원본에 없던 바이트를 만든다.
var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

type splice struct {
	span wml.Span
	repl []byte
	path string
}

// Apply 는 패치를 적용한다.
//
// 반환된 []Error 가 비어있지 않으면 패키지는 **손대지 않은 상태**다.
// error 는 내부 오류(종료 코드 2)이며, 이때도 패키지는 수정되지 않는다.
func Apply(p *opc.Package, pt Patch) ([]Error, error) {
	if pt.Hash != "" && pt.Hash != p.Hash {
		return []Error{{
			Path:   dump.ScannedPart,
			Reason: "hash_mismatch",
			Detail: fmt.Sprintf("패치 hash=%s, 문서 hash=%s", pt.Hash, p.Hash),
		}}, nil
	}

	content, err := p.Part(dump.ScannedPart)
	if err != nil {
		return nil, err
	}
	tree, err := wml.Scan(content)
	if err != nil {
		return nil, err
	}

	// 1) 모든 op 을 검증한다. 하나라도 실패하면 아무것도 적용하지 않는다.
	var errs []Error
	splices := make([]splice, 0, len(pt.Ops))
	for _, op := range pt.Ops {
		n, ok := tree.Lookup(op.Path)
		if !ok {
			errs = append(errs, Error{
				Path:   op.Path,
				Reason: "path_not_found",
				Detail: nearbyHint(tree, op.Path),
			})
			continue
		}
		switch op.Op {
		case "replaceRaw":
			splices = append(splices, splice{span: n.Span, repl: []byte(op.XML), path: op.Path})
		case "setText":
			if n.Type != "t" {
				errs = append(errs, Error{
					Path:   op.Path,
					Reason: "type_mismatch",
					Detail: fmt.Sprintf("setText 는 w:t 에만 쓸 수 있다 (대상 타입: %s)", n.Type),
				})
				continue
			}
			// 앞뒤 공백은 xml:space="preserve" 가 있어야만 허용한다.
			// 없는데 속성을 붙여주면 원본에 없던 바이트가 생겨 I4a 가 깨진다.
			if strings.TrimSpace(op.Text) != op.Text {
				if v, ok := n.Attr("space"); !ok || v != "preserve" {
					errs = append(errs, Error{
						Path:   op.Path,
						Reason: "whitespace_needs_preserve",
						Detail: `대상 w:t 에 xml:space="preserve" 가 없어 앞뒤 공백을 넣을 수 없다. replaceRaw 를 쓸 것`,
					})
					continue
				}
			}
			// Inner 만 교체한다 — 시작 태그의 속성을 건드리면 I4a 가 깨진다.
			splices = append(splices, splice{
				span: n.Inner,
				repl: []byte(xmlEscaper.Replace(op.Text)),
				path: op.Path,
			})
		default:
			errs = append(errs, Error{
				Path:   op.Path,
				Reason: "unknown_op",
				Detail: fmt.Sprintf("알 수 없는 연산: %s (setText | replaceRaw)", op.Op),
			})
		}
	}
	if len(errs) > 0 {
		return errs, nil
	}

	// 스플라이스가 없으면(빈 패치) 파트를 건드리지 않는다 — dirty 로 표시하면
	// Package.Write 가 내용이 같아도 재압축해 바이트가 달라진다 (I1 위반).
	if len(splices) == 0 {
		return nil, nil
	}

	// 2) 겹침 검사
	sort.Slice(splices, func(i, j int) bool { return splices[i].span.Start < splices[j].span.Start })
	for i := 1; i < len(splices); i++ {
		if splices[i].span.Start < splices[i-1].span.End {
			return []Error{{
				Path:   splices[i].path,
				Reason: "overlap",
				Detail: fmt.Sprintf("%s 의 구간과 겹친다", splices[i-1].path),
			}}, nil
		}
	}

	// 3) 내림차순 적용 — 앞에서부터 하면 뒤 구간의 오프셋이 밀린다
	out := make([]byte, len(content))
	copy(out, content)
	for i := len(splices) - 1; i >= 0; i-- {
		s := splices[i]
		var buf bytes.Buffer
		buf.Grow(len(out) - s.span.Len() + len(s.repl))
		buf.Write(out[:s.span.Start])
		buf.Write(s.repl)
		buf.Write(out[s.span.End:])
		out = buf.Bytes()
	}

	// 4) 결과 재스캔 — 바이트를 잘라 붙였으므로 파서가 막아주지 않는다
	if _, err := wml.Scan(out); err != nil {
		return nil, fmt.Errorf("적용 결과가 유효한 XML 이 아니다 (롤백함): %w", err)
	}

	return nil, p.Replace(dump.ScannedPart, out)
}

// nearbyHint 는 경로를 못 찾았을 때 형제 개수를 알려준다.
func nearbyHint(tree *wml.Tree, path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return ""
	}
	parent, leaf := path[:i], path[i+1:]
	j := strings.Index(leaf, "[")
	if j < 0 {
		return ""
	}
	name := leaf[:j]
	count := 0
	for _, n := range tree.Nodes {
		if strings.HasPrefix(n.Path, parent+"/"+name+"[") &&
			!strings.Contains(n.Path[len(parent)+1:], "/") {
			count++
		}
	}
	return fmt.Sprintf("%s 아래 %s 는 %d개", parent, name, count)
}
