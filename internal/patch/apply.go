package patch

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// xmlEscaper 는 텍스트 노드에서 의미를 갖는 세 글자만 이스케이프한다.
// xml.EscapeText 는 개행·탭까지 문자 참조로 바꿔 원본에 없던 바이트를 만든다.
// 이미 이스케이프된 입력(예: "&amp;")은 다시 이스케이프된다("&amp;amp;") —
// op.Text 는 항상 순수 텍스트(디코딩된 값)로 취급하므로 의도된 동작이다.
var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

type splice struct {
	span xmlscan.Span
	repl []byte
	path string
}

// Apply 는 패치를 적용한다. 파트가 여럿이어도 전부 적용되거나 전무다.
//
// 반환된 []Error 가 비어있지 않으면 패키지는 **손대지 않은 상태**다.
// error 는 보통 내부 오류(종료 코드 2)이지만, *opc.UnsupportedError 일 수도 있다 —
// 파트를 풀 때 미지원 압축 방식을 만나면 난다. CLI 는 이를 코드 1
// (unsupported_container)로 매핑한다. 어느 경우든 패키지는 수정되지 않는다.
func Apply(p *opc.Package, pt Patch) ([]Error, error) {
	if pt.Hash != "" && pt.Hash != p.Hash {
		return []Error{{Reason: "hash_mismatch",
			Detail: fmt.Sprintf("패치 hash=%s, 문서 hash=%s", pt.Hash, p.Hash)}}, nil
	}

	doc, err := parts.Open(p)
	if err != nil {
		if errors.Is(err, parts.ErrUnsupportedFormat) {
			return []Error{{Reason: "unsupported_format", Detail: err.Error()}}, nil
		}
		return nil, err
	}

	// 1) op 을 파트별로 가른다. 파트 해석 실패는 여기서 모은다.
	byPart := map[string][]Op{}
	var errs []Error
	for _, op := range pt.Ops {
		name, e := resolvePart(doc, op)
		if e != nil {
			errs = append(errs, *e)
			continue
		}
		byPart[name] = append(byPart[name], op)
	}
	if len(errs) > 0 {
		return errs, nil
	}
	if len(byPart) == 0 {
		return nil, nil // 빈 패치 — 아무것도 건드리지 않는다 (I1)
	}

	// 2) 파트별로 검증하고 버퍼를 만든다. 아직 아무것도 쓰지 않는다.
	//    맵이 아니라 계획 순서로 돌아 결정성을 지킨다.
	type pending struct {
		name string
		out  []byte
	}
	var buffers []pending
	for _, part := range doc.Parts() {
		ops, ok := byPart[part.Name]
		if !ok {
			continue
		}
		tree, err := doc.Tree(part.Name)
		if err != nil {
			return nil, err
		}
		out, es := spliceOne(tree, part, ops)
		if len(es) > 0 {
			return es, nil
		}
		buffers = append(buffers, pending{name: part.Name, out: out})
	}

	// 3) 모든 버퍼를 검증한 뒤에야 쓴다.
	//    파트 A 만 Replace 하고 B 가 깨지면 문서가 반쯤 바뀐다.
	for _, b := range buffers {
		if err := p.Replace(b.name, b.out); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// resolvePart 는 op 의 Part 를 물리 파트명으로 푼다.
// 에러가 파트와 경로 중 어디서 났는지 구분해서 말한다 — 에이전트의 재시도에 필요하다.
func resolvePart(doc *parts.Document, op Op) (string, *Error) {
	if op.Part == "" {
		ps := doc.Parts()
		if len(ps) == 1 {
			return ps[0].Name, nil
		}
		return "", &Error{Path: op.Path, Reason: "part_not_found",
			Detail: fmt.Sprintf("본문 파트가 %d개다 — op 에 part 를 명시해야 한다", len(ps))}
	}
	if name, ok := doc.Resolve(op.Part); ok {
		return name, nil
	}
	// 논리 참조 모양인가로 사유를 가른다
	if isRefShaped(op.Part) {
		return "", &Error{Path: op.Path, Reason: "ref_not_found",
			Detail: fmt.Sprintf("논리 참조 %q 가 풀리지 않는다", op.Part)}
	}
	if doc.Exists(op.Part) {
		return "", &Error{Path: op.Path, Reason: "part_not_scannable",
			Detail: fmt.Sprintf("%s 는 컨테이너에 있으나 스캔 대상이 아니다", op.Part)}
	}
	return "", &Error{Path: op.Path, Reason: "part_not_found",
		Detail: fmt.Sprintf("파트 %q 가 문서에 없다", op.Part)}
}

// isRefShaped 는 선택자가 논리 참조 모양인지 본다 ("pptx/slide[3]", "docx/document").
func isRefShaped(s string) bool {
	return strings.HasPrefix(s, "pptx/") || strings.HasPrefix(s, "docx/")
}

// spliceOne 은 파트 하나에 그 파트의 op 들을 적용해 스플라이스된 버퍼를 낸다.
// 반환은 (out, nil) 또는 (nil, errs) 다 — 에러가 있으면 버퍼를 만들지 않는다.
func spliceOne(tree *xmlscan.Tree, part parts.Part, ops []Op) ([]byte, []Error) {
	content := tree.Src

	// 1) 모든 op 을 검증한다. 하나라도 실패하면 아무것도 적용하지 않는다.
	var errs []Error
	splices := make([]splice, 0, len(ops))
	seen := make(map[string]bool, len(ops))
	for _, op := range ops {
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
			// xmlscan.Scan 은 이때 Inner 를 요소 바로 뒤의 폭 0 위치로 준다 — 여기 스플라이스하면
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
				if v, ok := n.AttrNS(xmlscan.XMLNS, "space"); !ok || v != "preserve" {
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
		return nil, errs
	}

	// 2) 겹침 검사
	sort.Slice(splices, func(i, j int) bool { return splices[i].span.Start < splices[j].span.Start })
	for i := 1; i < len(splices); i++ {
		if splices[i].span.Start < splices[i-1].span.End {
			return nil, []Error{{
				Path:   splices[i].path,
				Reason: "overlap",
				Detail: fmt.Sprintf("%s 의 구간과 겹친다", splices[i-1].path),
			}}
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
	// Apply 는 모든 버퍼를 검증한 뒤에야 Replace 를 부른다. 트랜잭션을 연 적이
	// 없으니 닫을 것도 없다.
	//
	// 결함은 전적으로 호출자가 준 XML 에 있으므로 입력 오류(코드 1)로 보고한다.
	// 내부 오류(코드 2)로 보내면, 종료 코드로 재시도 여부를 판단하는 에이전트가
	// "패치를 고쳐 다시 시도"가 아니라 "도구가 고장났으니 포기"로 잘못 분기한다 (spec §9).
	if _, err := xmlscan.Scan(out, part.Root); err != nil {
		return nil, []Error{{
			Path:   blame(content, splices, part.Root),
			Reason: "invalid_xml",
			Detail: fmt.Sprintf("적용 결과가 유효한 XML 이 아니다 (문서는 손대지 않았다): %v", err),
		}}
	}

	return out, nil
}

// blame 은 결과 XML 을 깨뜨린 op 의 경로를 짚는다.
//
// 스플라이스를 하나씩만 적용해 재스캔한다 — 단독으로 적용해도 깨지는 것이 장본인이다.
// 실패 경로에서만 도는 계산이다. 모두 단독으로는 유효한데 조합에서만 깨지는 경우는
// 첫 스플라이스를 지목한다 (경로 없는 에러를 내느니 결정론적으로 하나를 짚는다).
func blame(content []byte, splices []splice, rootAlias string) string {
	for _, s := range splices {
		var buf bytes.Buffer
		buf.Grow(len(content) - s.span.Len() + len(s.repl))
		buf.Write(content[:s.span.Start])
		buf.Write(s.repl)
		buf.Write(content[s.span.End:])
		if _, err := xmlscan.Scan(buf.Bytes(), rootAlias); err != nil {
			return s.path
		}
	}
	return splices[0].path
}

// nearbyHint 는 경로를 못 찾았을 때 형제 개수를 알려준다.
// 형태를 못 알아본 경로는 왜 힌트를 못 주는지 말한다 — 빈 detail 로 침묵하지 않는다.
func nearbyHint(tree *xmlscan.Tree, path string) string {
	const shape = `경로 형태가 "부모/이름[n]" 이 아니라 형제 수를 셀 수 없다 (루트 경로엔 인덱스가 없다)`
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

// PartsLoadedBy 는 이 패치를 적용할 때 실제로 스캔되는 파트를 돌려준다.
// 지연 로딩이 주석이 아니라 계약임을 테스트가 고정하기 위한 것이다.
// 패키지를 변경하지 않는다 — 검증만 하고 버린다.
func PartsLoadedBy(p *opc.Package, pt Patch) []string {
	doc, err := parts.Open(p)
	if err != nil {
		return nil
	}
	for _, op := range pt.Ops {
		name, e := resolvePart(doc, op)
		if e != nil {
			continue
		}
		if _, err := doc.Tree(name); err != nil {
			continue
		}
	}
	return doc.Loaded()
}
