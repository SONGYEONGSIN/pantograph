package patch

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// xmlEscaper 는 텍스트 노드에서 의미를 갖는 세 글자만 이스케이프한다.
// xml.EscapeText 는 개행·탭까지 문자 참조로 바꿔 원본에 없던 바이트를 만든다.
// 이미 이스케이프된 입력(예: "&amp;")은 다시 이스케이프된다("&amp;amp;") —
// *op.Text 는 항상 순수 텍스트(디코딩된 값)로 취급하므로 의도된 동작이다.
var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

type splice struct {
	span xmlscan.Span
	repl []byte
	path string
}

// pending 은 검증까지 통과했지만 아직 쓰지 않은 파트 버퍼 하나다.
type pending struct {
	name string
	out  []byte
}

// Apply 는 패치를 적용한다. 파트가 여럿이어도 전부 적용되거나 전무다.
//
// 반환된 []Error 가 비어있지 않으면 패키지는 **손대지 않은 상태**다.
// error 는 보통 내부 오류(종료 코드 2)이지만, 입력 부류일 수도 있다 —
// *opc.UnsupportedError(파트를 풀 때 만난 미지원 압축 방식)와
// parts.ErrUnsupportedFormat(Plan 의 모든 실패)이 그렇다. 그 둘을 reason 으로
// 옮기는 것은 CLI 의 분류기 한 곳이 맡는다 — 여기서 따로 매핑하면 같은 오류에
// 두 표가 생긴다. 어느 경우든 패키지는 수정되지 않는다.
func Apply(p *opc.Package, pt Patch) ([]Error, error) {
	if pt.Hash != "" && pt.Hash != p.Hash {
		return []Error{{Reason: "hash_mismatch",
			Detail: fmt.Sprintf("패치 hash=%s, 문서 hash=%s", pt.Hash, p.Hash)}}, nil
	}

	doc, err := parts.Open(p)
	if err != nil {
		return nil, err
	}

	buffers, errs, err := resolveAndBuffer(doc, pt.Ops)
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return errs, nil
	}
	if len(buffers) == 0 {
		return nil, nil // 빈 패치 — 아무것도 건드리지 않는다 (I1)
	}

	// 모든 버퍼를 검증한 뒤에야 쓴다.
	// 파트 A 만 Replace 하고 B 가 깨지면 문서가 반쯤 바뀐다.
	for _, b := range buffers {
		if err := p.Replace(b.name, b.out); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// resolveAndBuffer 는 op 을 파트별로 갈라 각 파트를 검증·스플라이스해 버퍼로
// 만든다. Replace 는 부르지 않는다 — Apply 와 PartsLoadedBy 가 이 함수 하나를
// 공유하므로 "op 이 지목한 파트만 스캔한다"는 지연 로딩 계약이 두 곳에서
// 따로 구현되어 어긋날 여지가 없다.
func resolveAndBuffer(doc *parts.Document, ops []Op) ([]pending, []Error, error) {
	// 1) op 을 파트별로 가른다. 파트 해석 실패는 여기서 모은다.
	byPart := map[string][]Op{}
	var errs []Error
	for _, op := range ops {
		name, e := resolvePart(doc, op)
		if e != nil {
			errs = append(errs, *e)
			continue
		}
		byPart[name] = append(byPart[name], op)
	}
	if len(errs) > 0 {
		return nil, errs, nil
	}
	if len(byPart) == 0 {
		return nil, nil, nil // 빈 패치 — 아무것도 건드리지 않는다 (I1)
	}

	// 2) 파트별로 검증하고 버퍼를 만든다. 아직 아무것도 쓰지 않는다.
	//    맵이 아니라 계획 순서로 돌아 결정성을 지킨다.
	var buffers []pending
	for _, part := range doc.Parts() {
		partOps, ok := byPart[part.Name]
		if !ok {
			continue
		}
		tree, err := doc.Tree(part.Name)
		if err != nil {
			return nil, nil, err
		}
		out, es := spliceOne(tree, part, partOps)
		if len(es) > 0 {
			return nil, es, nil
		}
		buffers = append(buffers, pending{name: part.Name, out: out})
	}
	return buffers, nil, nil
}

// resolvePart 는 op 의 Part 를 물리 파트명으로 푼다.
// 에러가 파트와 경로 중 어디서 났는지 구분해서 말한다 — 에이전트의 재시도에 필요하다.
//
// 못 푼 선택자의 세 갈래 판정(part_not_found / ref_not_found / part_not_scannable)은
// parts.Document.Reject 가 맡는다 — dump 의 `--part` 가 같은 판정을 쓰므로 같은
// 입력에 두 답이 나오지 않는다.
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
	se := doc.Reject(op.Part)
	return "", &Error{Path: op.Path, Reason: se.Reason, Detail: se.Detail}
}

// checkFields 는 연산과 필드가 맞는지 본다.
//
// Op 는 합집합 타입이다 — Text 는 setText 전용, XML 은 replaceRaw 전용이고
// delete 는 둘 다 쓰지 않는다. 이 계약을 검사하지 않으면 필드를 잘못 고르거나
// 빠뜨린 패치가 조용히 내용을 지운다 (설계 §1).
//
// 순서가 진단의 품질이다: 안 쓰는 필드를 먼저 본다. setText 에 xml 만 준
// 입력은 "값을 빠뜨렸다"가 아니라 "필드를 잘못 골랐다"이기 때문이다 (설계 §3.2).
func checkFields(op Op) *Error {
	switch op.Op {
	case "setText":
		if op.XML != nil {
			return &Error{Path: op.Path, Reason: "unused_field",
				Detail: "setText 는 xml 을 쓰지 않는다 — 텍스트는 text 에 준다"}
		}
		if op.Text == nil {
			return &Error{Path: op.Path, Reason: "missing_text",
				Detail: "setText 에 text 가 없다"}
		}
	case "replaceRaw":
		if op.Text != nil {
			return &Error{Path: op.Path, Reason: "unused_field",
				Detail: "replaceRaw 는 text 를 쓰지 않는다 — 마크업은 xml 에 준다"}
		}
		if op.XML == nil {
			return &Error{Path: op.Path, Reason: "missing_xml",
				Detail: "replaceRaw 에 xml 이 없다"}
		}
		// 조각은 요소를 하나 이상 담아야 한다. 빈 문자열만 보면 " " · 주석 ·
		// 텍스트 같은 요소 없는 조각이 그대로 통과해 노드를 지우고 ok:true 를
		// 낸다 — "" 와 " " 는 보이지 않는 한 바이트 차이인데 결과가 정반대였다.
		//
		// 이유를 나누지 않는다: 이 입력들의 처방이 전부 같다
		// ("진짜 내용을 주거나 delete 를 써라").
		if ok, err := hasElement(*op.XML); !ok && err == nil {
			return &Error{Path: op.Path, Reason: "empty_xml",
				Detail: "replaceRaw 의 xml 에 요소가 하나도 없다 — 노드를 지우려면 delete 를 쓸 것"}
		}
	case "delete":
		if op.Text != nil || op.XML != nil {
			return &Error{Path: op.Path, Reason: "unused_field",
				Detail: "delete 는 text·xml 을 쓰지 않는다 — 지울 노드는 path 로만 지목한다"}
		}
	}
	return nil
}

// hasElement 는 조각에 요소가 하나라도 있는지 본다. 첫 StartElement 에서 멈춘다.
//
// **읽기지 재직렬화가 아니다** — 토큰만 훑고 조각의 바이트는 그대로 스플라이스된다.
//
// xmlscan.Scan 을 쓰지 않는 이유: 조각은 최상위 요소가 여럿일 수 있는데
// (예: 문단 하나를 둘로 늘리는 `<w:p/><w:p/>`) Scan 은 루트 별칭 하나만 부여해
// 둘째를 경로 충돌로 거절한다. 정당한 패치가 막힌다.
//
// 요소를 만나기 전에 디코더가 깨지면 (false, err) 를 낸다. 호출자는 그때
// 거절하지 않는다 — 문법이 깨진 조각에 "요소가 없다"고 답하면 입력을 잘못
// 설명하는 셈이고, 균형 잡힌 구간을 균형 잡히지 않은 조각으로 바꾼 결과는
// 반드시 깨지므로 스플라이스 후 재스캔이 invalid_xml 로 정확히 잡는다.
func hasElement(frag string) (bool, error) {
	dec := xml.NewDecoder(strings.NewReader(frag))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if _, ok := tok.(xml.StartElement); ok {
			return true, nil
		}
	}
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

		// 필드 정합을 경로 조회보다 먼저 본다. 필드가 틀렸다는 건 경로가
		// 존재하든 말든 사실이고, 경로까지 틀린 패치에서 path_not_found 만
		// 보여주면 사용자가 경로를 고친 뒤 두 번째 오류를 만난다.
		if e := checkFields(op); e != nil {
			errs = append(errs, *e)
			continue
		}

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
			splices = append(splices, splice{span: n.Span, repl: []byte(*op.XML), path: op.Path})
		case "delete":
			// 루트를 지우면 파트가 XML 선언만 남은 파일이 된다. 재스캔이 이를
			// empty_part 로 잡을 텐데, 사용자가 주지 않은 XML 을 재스캔으로
			// 책임지기보다 사전에 거절하는 쪽이 낫다. 루트는 "할 수 없다",
			// 다른 노드는 "할 수 있다"는 뜻의 분리된 거절이다.
			if op.Path == part.Root {
				errs = append(errs, Error{
					Path:   op.Path,
					Reason: "delete_root",
					Detail: fmt.Sprintf("루트 노드 %s 는 지울 수 없다 — 파트가 빈 파일이 된다", part.Root),
				})
				continue
			}
			// 요소 전체를 폭 0 으로 치환한다. 앞뒤 공백·개행은 건드리지 않는다 —
			// 인접 바이트를 먹으면 어디까지 지웠는지가 흐려지고 I2 논증이 약해진다.
			splices = append(splices, splice{span: n.Span, repl: nil, path: op.Path})
		case "setText":
			// 거절 문구는 포맷 특정 요소 이름을 대지 않는다 — 같은 텍스트 요소가
			// docx 에서는 w:t, pptx 에서는 a:t 다. setText 의 규칙은 "Word 의
			// w:t 여야 한다"가 아니라 "지목된 노드가 텍스트 요소(로컬명 t)여야
			// 한다"이며, 구체 예가 필요한 자리에는 손에 든 노드의 Type 을 쓴다.
			if n.Type != "t" {
				errs = append(errs, Error{
					Path:   op.Path,
					Reason: "type_mismatch",
					Detail: fmt.Sprintf("setText 는 텍스트 요소(로컬명 t)에만 쓸 수 있다 (대상 타입: %s)", n.Type),
				})
				continue
			}
			// self-closing 텍스트 요소는 시작/종료 태그가 하나로 합쳐져 있어 '안쪽'이 없다.
			// xmlscan.Scan 은 이때 Inner 를 요소 바로 뒤의 폭 0 위치로 준다 — 여기 스플라이스하면
			// 텍스트가 요소 밖(형제 위치)에 삽입돼 well-formed 지만 의미가 깨진다.
			if n.Inner.Start >= n.Span.End {
				errs = append(errs, Error{
					Path:   op.Path,
					Reason: "self_closing_target",
					Detail: fmt.Sprintf("대상 %s 가 self-closing 이라 텍스트를 넣을 위치가 없다. replaceRaw 를 쓸 것", n.Type),
				})
				continue
			}
			// 앞뒤 공백은 xml:space="preserve" 가 있어야만 허용한다.
			// 없는데 속성을 붙여주면 원본에 없던 바이트가 생겨 I4a 가 깨진다.
			// 네임스페이스까지 본다 — 로컬명만 보면 아무 네임스페이스의
			// space 속성이나 xml:space 로 통과한다.
			if strings.TrimSpace(*op.Text) != *op.Text {
				if v, ok := n.AttrNS(xmlscan.XMLNS, "space"); !ok || v != "preserve" {
					errs = append(errs, Error{
						Path:   op.Path,
						Reason: "whitespace_needs_preserve",
						Detail: fmt.Sprintf(`대상 %s 에 xml:space="preserve" 가 없어 앞뒤 공백을 넣을 수 없다. replaceRaw 를 쓸 것`, n.Type),
					})
					continue
				}
			}
			// Inner 만 교체한다 — 시작 태그의 속성을 건드리면 I4a 가 깨진다.
			splices = append(splices, splice{
				span: n.Inner,
				repl: []byte(xmlEscaper.Replace(*op.Text)),
				path: op.Path,
			})
		default:
			errs = append(errs, Error{
				Path:   op.Path,
				Reason: "unknown_op",
				Detail: fmt.Sprintf("알 수 없는 연산: %s (setText | replaceRaw | delete)", op.Op),
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
	reason, detail := badResult(out, part.Root)
	if reason != "" {
		return nil, []Error{{
			Path:   blame(content, splices, part.Root),
			Reason: reason,
			Detail: detail,
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
		reason, _ := badResult(buf.Bytes(), rootAlias)
		if reason != "" {
			return s.path
		}
	}
	return splices[0].path
}

// badResult 는 스플라이스 결과가 파트로서 성립하지 않는 이유를 돌려준다.
// 성립하면 reason 이 빈 문자열이다.
func badResult(out []byte, rootAlias string) (reason, detail string) {
	tree, err := xmlscan.Scan(out, rootAlias)
	if err != nil {
		// Scan 이 에러를 내면 XML 파싱 실패다.
		return "invalid_xml", fmt.Sprintf("적용 결과가 유효한 XML 이 아니다 (문서는 손대지 않았다): %v", err)
	}
	// Scan 이 성공해도 요소가 하나도 없으면 파트가 성립하지 않는다.
	//
	// **이 갈래는 지금 CLI 입력으로는 도달할 수 없다.** 논증: 파트를 노드 0개로
	// 만드는 길은 둘뿐인데 둘 다 앞에서 막힌다 — replaceRaw 의 조각은 요소를
	// 하나 이상 담아야 하고(checkFields 의 empty_xml), 루트 delete 는
	// delete_root 가 먼저 거절한다. 그래도 지우지 않는다: 이건 내용을 지우는
	// 경로의 마지막 그물이고, "지금 도달 불가"는 "불필요"가 아니라 앞의 두
	// 검사에 의존하는 조건부 사실이다. 둘 중 하나가 느슨해지면 이 그물이 유일한
	// 방어가 된다. 시험은 apply_internal_test.go 가 술어를 직접 불러서 한다.
	if len(tree.Nodes) == 0 {
		return "empty_part", "적용 결과에 요소가 하나도 없다 (문서는 손대지 않았다)"
	}
	return "", ""
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
// Apply 와 같은 resolveAndBuffer 를 불러 지연 스캔 루프 자체를 관찰한다 —
// 따로 다시 구현하면 두 로직이 갈라져도 이 테스트가 못 잡는다.
// 패키지를 변경하지 않는다 — 검증만 하고 버린다.
func PartsLoadedBy(p *opc.Package, pt Patch) []string {
	doc, err := parts.Open(p)
	if err != nil {
		return nil
	}
	resolveAndBuffer(doc, pt.Ops)
	return doc.Loaded()
}
