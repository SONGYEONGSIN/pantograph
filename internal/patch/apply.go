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
// 이미 이스케이프된 입력(예: "&amp;")은 다시 이스케이프된다("&amp;amp;") —
// op.Text 는 항상 순수 텍스트(디코딩된 값)로 취급하므로 의도된 동작이다.
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
	seen := make(map[string]bool, len(pt.Ops))
	for _, op := range pt.Ops {
		// 같은 경로에 연산이 둘 이상이면 거절한다.
		//
		// 겹침 검사(splices[i].start < splices[i-1].end)는 폭 0 구간을 못 잡는다:
		// 빈 <w:t></w:t> 의 안쪽은 [p,p) 라 p < p 가 거짓이 되어 둘 다 통과하고,
		// 같은 지점에 두 번 스플라이스돼 텍스트가 조용히 이어붙는다.
		// 게다가 sort.Slice 는 안정 정렬이 아니라 이어붙는 순서도 정의되지 않는다 (I3).
		if seen[op.Path] {
			errs = append(errs, Error{
				Path:   op.Path,
				Reason: "duplicate_path",
				Detail: "같은 경로를 가리키는 연산이 둘 이상이다 — 적용 순서가 정의되지 않는다",
			})
			continue
		}
		seen[op.Path] = true

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
			// self-closing <w:t/> 는 시작/종료 태그가 하나로 합쳐져 있어 '안쪽'이 없다.
			// wml.Scan 은 이때 Inner 를 요소 바로 뒤의 폭 0 위치로 준다 — 여기 스플라이스하면
			// 텍스트가 w:t 밖(형제 위치)에 삽입돼 well-formed 지만 의미가 깨진다.
			if n.Inner.Start >= n.Span.End {
				errs = append(errs, Error{
					Path:   op.Path,
					Reason: "self_closing_target",
					Detail: "대상 w:t 가 self-closing 이라 텍스트를 넣을 위치가 없다. replaceRaw 를 쓸 것",
				})
				continue
			}
			// 앞뒤 공백은 xml:space="preserve" 가 있어야만 허용한다.
			// 없는데 속성을 붙여주면 원본에 없던 바이트가 생겨 I4a 가 깨진다.
			// 네임스페이스까지 본다 — 로컬명만 보면 아무 네임스페이스의
			// space 속성이나 xml:space 로 통과한다.
			if strings.TrimSpace(op.Text) != op.Text {
				if v, ok := n.AttrNS(wml.XMLNS, "space"); !ok || v != "preserve" {
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

	// 4) 결과 재스캔 — 바이트를 잘라 붙였으므로 파서가 막아주지 않는다.
	//
	// 실패해도 되돌릴 것이 없다: 스플라이스는 지역 버퍼 out 에만 쌓았고
	// p.Replace 는 아래에서 처음 호출된다. 트랜잭션을 연 적이 없으니 닫을 것도 없다.
	//
	// 결함은 전적으로 호출자가 준 XML 에 있으므로 입력 오류(코드 1)로 보고한다.
	// 내부 오류(코드 2)로 보내면, 종료 코드로 재시도 여부를 판단하는 에이전트가
	// "패치를 고쳐 다시 시도"가 아니라 "도구가 고장났으니 포기"로 잘못 분기한다 (spec §9).
	if _, err := wml.Scan(out); err != nil {
		return []Error{{
			Path:   blame(content, splices),
			Reason: "invalid_xml",
			Detail: fmt.Sprintf("적용 결과가 유효한 XML 이 아니다 (문서는 손대지 않았다): %v", err),
		}}, nil
	}

	return nil, p.Replace(dump.ScannedPart, out)
}

// blame 은 결과 XML 을 깨뜨린 op 의 경로를 짚는다.
//
// 스플라이스를 하나씩만 적용해 재스캔한다 — 단독으로 적용해도 깨지는 것이 장본인이다.
// 실패 경로에서만 도는 계산이다. 모두 단독으로는 유효한데 조합에서만 깨지는 경우는
// 첫 스플라이스를 지목한다 (경로 없는 에러를 내느니 결정론적으로 하나를 짚는다).
func blame(content []byte, splices []splice) string {
	for _, s := range splices {
		var buf bytes.Buffer
		buf.Grow(len(content) - s.span.Len() + len(s.repl))
		buf.Write(content[:s.span.Start])
		buf.Write(s.repl)
		buf.Write(content[s.span.End:])
		if _, err := wml.Scan(buf.Bytes()); err != nil {
			return s.path
		}
	}
	return splices[0].path
}

// nearbyHint 는 경로를 못 찾았을 때 형제 개수를 알려준다.
// 형태를 못 알아본 경로는 왜 힌트를 못 주는지 말한다 — 빈 detail 로 침묵하지 않는다.
func nearbyHint(tree *wml.Tree, path string) string {
	const shape = `경로 형태가 "부모/이름[n]" 이 아니라 형제 수를 셀 수 없다 (루트는 "word")`
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return shape
	}
	parent, leaf := path[:i], path[i+1:]
	j := strings.Index(leaf, "[")
	if j < 0 {
		return shape
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
