package tmpl_test

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
	"github.com/SONGYEONGSIN/pantograph/internal/tmpl"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

func pkgs(t *testing.T, forms ...[]string) ([]*opc.Package, []string) {
	t.Helper()
	var out []*opc.Package
	var names []string
	for i, f := range forms {
		p, err := opc.OpenBytes(testutil.MinimalDocx(f))
		if err != nil {
			t.Fatalf("OpenBytes[%d]: %v", i, err)
		}
		out = append(out, p)
		names = append(names, string(rune('a'+i))+".docx")
	}
	return out, names
}

func TestExtractFindsVariableParts(t *testing.T) {
	ps, names := pkgs(t,
		[]string{"청구서", "홍길동", "합계"},
		[]string{"청구서", "김철수", "합계"},
	)

	tp, sch, errs, err := tmpl.Extract(ps, names, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("에러: %+v", errs)
	}
	if len(sch.Keys) != 1 {
		t.Fatalf("키 %d개, 기대 1개: %+v", len(sch.Keys), sch.Keys)
	}
	k := sch.Keys[0]
	if k.Key != "k1" {
		t.Fatalf("키 이름 %q, 기대 %q", k.Key, "k1")
	}
	if k.Part != "word/document.xml" {
		t.Fatalf("키 파트 %q, 기대 %q", k.Part, "word/document.xml")
	}
	if k.Path != "document/body[1]/p[2]/r[1]/t[1]" {
		t.Fatalf("키 경로 %q", k.Path)
	}
	if len(k.Samples) != 2 || k.Samples[0] != "홍길동" || k.Samples[1] != "김철수" {
		t.Fatalf("샘플이 부정확하다: %+v", k.Samples)
	}
	if sch.Base != "a.docx" {
		t.Fatalf("Base = %q", sch.Base)
	}
	if sch.Hash != ps[0].Hash {
		t.Fatalf("Hash 가 베이스 문서의 것이 아니다")
	}

	content, err := tp.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("{{k1}}")) {
		t.Fatalf("템플릿에 {{k1}} 이 없다: %s", content)
	}
	if !bytes.Contains(content, []byte("청구서")) || !bytes.Contains(content, []byte("합계")) {
		t.Fatalf("고정부가 사라졌다: %s", content)
	}
	if bytes.Contains(content, []byte("홍길동")) {
		t.Fatalf("가변부가 남아있다: %s", content)
	}
}

func TestExtractAssignsKeysInDocumentOrder(t *testing.T) {
	ps, names := pkgs(t,
		[]string{"A1", "고정", "B1"},
		[]string{"A2", "고정", "B2"},
	)
	_, sch, errs, err := tmpl.Extract(ps, names, false)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}
	if len(sch.Keys) != 2 {
		t.Fatalf("키 %d개, 기대 2개", len(sch.Keys))
	}
	if sch.Keys[0].Key != "k1" || sch.Keys[0].Part != "word/document.xml" || sch.Keys[0].Path != "document/body[1]/p[1]/r[1]/t[1]" {
		t.Fatalf("k1 이 문서 순서의 첫 가변부가 아니다: %+v", sch.Keys[0])
	}
	if sch.Keys[1].Key != "k2" || sch.Keys[1].Part != "word/document.xml" || sch.Keys[1].Path != "document/body[1]/p[3]/r[1]/t[1]" {
		t.Fatalf("k2 가 부정확하다: %+v", sch.Keys[1])
	}
}

// TestExtractRejectsStructureMismatch 는 Task 3 이전에는 diffStructure(경로
// 순열이 완전히 같아야 함)로 structure_mismatch 를 냈다. 이 태스크가 그 게이트를
// 정렬로 갈아끼우면서, 문단 수가 다른 이 픽스처는 이제 정렬 자체는 성공하고
// 대신 표현 못 하는 서브트리가 생겨 unrepresented_structure 로 거절된다(플래그가
// false 이므로) — 거절된다는 결론은 같고 사유만 바뀐다. T2 가 이 새 경로를
// 전담해서 시험하므로 이 테스트는 사유만 새 것으로 갱신한다.
func TestExtractRejectsStructureMismatch(t *testing.T) {
	ps, names := pkgs(t,
		[]string{"A", "B", "C"},
		[]string{"A", "B"}, // 문단 수가 다르다
	)
	_, _, errs, err := tmpl.Extract(ps, names, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "unrepresented_structure" {
		t.Fatalf("구조 불일치가 거절되지 않았다: %+v", errs)
	}
	if errs[0].Path == "" {
		t.Fatal("갈라진 경로를 지목하지 않았다")
	}
}

// TestExtractRejectsPartSetMismatch 는 포맷이 다른(파트 집합이 다른) 문서 쌍이
// 노드 비교까지 가지 않고 파트 집합 단계에서 거절되는지 본다.
func TestExtractRejectsPartSetMismatch(t *testing.T) {
	a, err := opc.OpenBytes(testutil.MinimalDocx([]string{"고정", "A"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	b, err := opc.Open(filepath.Join("..", "..", "testdata", "real", "deck-a.pptx"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "deck.pptx"}, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "structure_mismatch" {
		t.Fatalf("포맷이 다른 문서가 거절되지 않았다: %+v", errs)
	}
}

func TestExtractIgnoresVolatileAttrs(t *testing.T) {
	// MinimalDocx 는 문단마다 다른 w14:paraId 를 넣지만
	// 두 문서의 같은 위치 문단은 같은 paraId 를 갖는다.
	// 여기서는 paraId 가 달라도 통과하는지 직접 확인한다.
	a, err := opc.OpenBytes(testutil.MinimalDocx([]string{"고정", "가변A"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	b, err := opc.OpenBytes(testutil.MinimalDocx([]string{"고정", "가변B"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	bc, err := b.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	bc = bytes.ReplaceAll(bc, []byte(`w14:paraId="00000001"`), []byte(`w14:paraId="DEADBEEF"`))
	if err := b.Replace("word/document.xml", bc); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	_, sch, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"}, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("휘발성 속성 차이로 거절됐다: %+v", errs)
	}
	if len(sch.Keys) != 1 {
		t.Fatalf("키 %d개, 기대 1개", len(sch.Keys))
	}
}

func TestExtractRejectsNonTextDiff(t *testing.T) {
	a, err := opc.OpenBytes(testutil.MinimalDocx([]string{"고정"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	b, err := opc.OpenBytes(testutil.MinimalDocx([]string{"고정"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	bc, err := b.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	// 휘발성이 아닌 속성을 추가한다
	bc = bytes.ReplaceAll(bc, []byte(`<w:t xml:space="preserve">`), []byte(`<w:t xml:space="default">`))
	if err := b.Replace("word/document.xml", bc); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"}, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "nontext_diff" {
		t.Fatalf("텍스트 외 차이가 거절되지 않았다: %+v", errs)
	}
}

// fieldDoc 은 w:instrText(필드 명령)와 w:t(표시 텍스트)를 갖는 문서를 만든다.
// w:instrText 는 Word 가 메일머지·페이지 번호·상호참조·TOC 를 인코딩하는 요소다.
func fieldDoc(t *testing.T, instr, text string) *opc.Package {
	t.Helper()
	p, err := opc.OpenBytes(testutil.DocxWithBody(
		`<w:p><w:r><w:instrText>` + instr + `</w:instrText></w:r>` +
			`<w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	return p
}

// TestExtractRejectsInstrTextDiff 는 F-C1 회귀 테스트다.
//
// 가변부 판별은 Type == "t" 인 노드만 보고, diffMarkup 은 Type 과 속성만 봤다.
// 그래서 w:t 가 아닌 요소가 나르는 텍스트 차이는 어느 검사에도 안 걸려
// D₁ 의 것이 조용히 채택됐다 — 메일머지 양식이 바로 이 기능의 대표 입력인데도.
// spec §8 이 명시적으로 금지하는 동작이다.
func TestExtractRejectsInstrTextDiff(t *testing.T) {
	a := fieldDoc(t, "MERGEFIELD Name", "홍길동")
	b := fieldDoc(t, "MERGEFIELD Company", "김철수")

	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"}, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "nontext_diff" {
		t.Fatalf("필드 명령의 차이가 거절되지 않았다: %+v", errs)
	}
	if errs[0].Path != "document/body[1]/p[1]/r[1]/instrText[1]" {
		t.Fatalf("갈라진 경로를 정확히 지목하지 않았다: %+v", errs[0])
	}
}

// TestExtractAcceptsIdenticalInstrText 는 같은 필드 명령이면 통과하고 w:t 의
// 차이는 여전히 가변부로 잡히는지 본다 — 위 거절이 과하지 않음을 고정한다.
func TestExtractAcceptsIdenticalInstrText(t *testing.T) {
	a := fieldDoc(t, "MERGEFIELD Name", "홍길동")
	b := fieldDoc(t, "MERGEFIELD Name", "김철수")

	_, sch, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"}, false)
	if err != nil || len(errs) != 0 {
		t.Fatalf("같은 필드 명령인데 거절됐다: err=%v errs=%+v", err, errs)
	}
	if len(sch.Keys) != 1 || sch.Keys[0].Path != "document/body[1]/p[1]/r[2]/t[1]" {
		t.Fatalf("w:t 의 차이가 가변부로 잡히지 않았다: %+v", sch.Keys)
	}
}

// TestExtractRejectsIndentationDiff 는 F-C1 수정의 대가를 문서로 고정한다.
//
// 스캐너는 CharData 를 가장 안쪽 프레임에만 쌓으므로, 요소 사이의 공백은 부모
// 노드의 Text 가 된다. 따라서 document.xml 을 서로 다르게 들여쓴 두 문서는
// 이제 nontext_diff 로 거절된다. Word 는 document.xml 을 들여쓰기 없이 쓰므로
// 실제로는 잘 만나지 않지만, 만나면 조용히 한쪽을 고르는 대신 거절한다.
func TestExtractRejectsIndentationDiff(t *testing.T) {
	a, err := opc.OpenBytes(testutil.DocxWithBody(
		`<w:p><w:r><w:t xml:space="preserve">고정</w:t></w:r></w:p>`))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	b, err := opc.OpenBytes(testutil.DocxWithBody(
		"\n  <w:p>\n    <w:r><w:t xml:space=\"preserve\">고정</w:t></w:r>\n  </w:p>\n"))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}

	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"}, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "nontext_diff" {
		t.Fatalf("들여쓰기가 다른 문서가 거절되지 않았다 (거절이 기대 동작이다): %+v", errs)
	}
}

// TestNontextDiffDetailNamesNoFormatElement 는 nontext_diff 문구가 특정 포맷의
// 요소 이름을 박아넣지 않는지 본다 (patch 의 setText 거절 문구와 같은 부류).
//
// "w:t 밖의 텍스트가 다르다"는 pptx 를 비교할 때 거짓이다 — 슬라이드의 텍스트
// 요소는 `a:t` 다. 이 검사의 규칙은 "Word 의 w:t 밖"이 아니라 "가변부로 다루지
// 않는 요소의 직접 텍스트"이므로, 문구는 손에 든 노드의 Type 을 말해야 한다.
func TestNontextDiffDetailNamesNoFormatElement(t *testing.T) {
	a := fieldDoc(t, "MERGEFIELD Name", "홍길동")
	b := fieldDoc(t, "MERGEFIELD Company", "김철수")

	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"}, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "nontext_diff" {
		t.Fatalf("기대한 사유로 거절되지 않았다: %+v", errs)
	}
	detail := errs[0].Detail
	if detail == "" {
		t.Fatal("Detail 이 비어있다")
	}
	for _, lit := range []string{"w:t", "a:t"} {
		if strings.Contains(detail, lit) {
			t.Fatalf("Detail 이 포맷 특정 요소 이름을 박아넣었다: %q (%s 포함)", detail, lit)
		}
	}
}

func TestExtractRequiresTwoDocuments(t *testing.T) {
	ps, names := pkgs(t, []string{"A"})
	_, _, errs, err := tmpl.Extract(ps, names, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "too_few_documents" {
		t.Fatalf("문서 1벌인데 거절되지 않았다: %+v", errs)
	}
}

// I4a — 베이스 문서에 대한 바이트 수준 가역성
func TestTemplateReversalBase(t *testing.T) {
	forms := [][]string{
		{"청구서", "홍길동", "1,200,000"},
		{"청구서", "김철수", "880,000"},
	}
	ps, names := pkgs(t, forms...)

	tp, sch, errs, err := tmpl.Extract(ps, names, false)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}

	vals, err := tmpl.Values(ps[0], sch)
	if err != nil {
		t.Fatalf("Values: %v", err)
	}

	filled, err := opc.OpenBytes(mustBytes(t, tp))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	fillErrs, err := tmpl.Fill(filled, sch, vals)
	if err != nil || len(fillErrs) != 0 {
		t.Fatalf("Fill: err=%v errs=%+v", err, fillErrs)
	}

	want, err := ps[0].Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	got, err := filled.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("I4a 위반 — 베이스로 되채웠는데 원본과 다르다\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// I4a — 실제 Word 문서. 픽스처가 없으면 FAIL 이다. skip 으로 바꾸지 말 것 (spec §10).
//
// TestTemplateReversalBase 는 프로젝트 자체 생성기가 만든 문서로만 돈다. I4a 는
// 이 설계의 심장인데(spec §3), 실제 Word 산출물에 대해서는 한 번도 평가된 적이 없다.
// 실제 문서가 들어오면 spec §13 의 문자 참조 왕복 한계(&amp; vs &#38;)가 가장 먼저
// 드러날 곳이기도 하다. 그 자리를 비워두면 픽스처가 들어와도 아무도 확인하지 않는다.
func TestTemplateReversalReal(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "real", "*.docx"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(paths) < 2 {
		t.Fatalf("testdata/real/*.docx 가 %d개 — I4a 는 같은 양식의 실제 Word 문서 2벌 이상으로만 의미가 있다 (spec §10)", len(paths))
	}

	ps := make([]*opc.Package, len(paths))
	names := make([]string, len(paths))
	for i, path := range paths {
		p, err := opc.Open(path)
		if err != nil {
			t.Fatalf("Open %s: %v", path, err)
		}
		ps[i] = p
		names[i] = filepath.Base(path)
	}

	tp, sch, errs, err := tmpl.Extract(ps, names, false)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}
	vals, err := tmpl.Values(ps[0], sch)
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	filled, err := opc.OpenBytes(mustBytes(t, tp))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if fe, err := tmpl.Fill(filled, sch, vals); err != nil || len(fe) != 0 {
		t.Fatalf("Fill: err=%v errs=%+v", err, fe)
	}

	want, err := ps[0].Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	got, err := filled.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("I4a 위반 — %s 를 자기 값으로 되채웠는데 원본과 다르다", names[0])
	}
}

// I4a — 실제 PowerPoint 문서. 픽스처가 없으면 FAIL 이다. skip 으로 바꾸지 말 것 (spec §10).
//
// TestTemplateReversalReal 은 docx 만 본다. Task 7 리뷰에서 남은 finding —
// tmpl.Extract 의 키 번호가 파트를 가로질러 이어지는지 시험하는 테스트가
// 없었다: 지금까지 Extract 의 키 루프에 닿는 모든 테스트가 파트 하나짜리
// docx 를 썼으므로, 파트별 루프가 정확히 한 번만 돌아 예전 단일 파트
// 코드와 구별이 안 됐다. deck-a·deck-b 는 슬라이드 3장 각각에서 제목이
// 갈리므로, 여기서 잡히는 키가 파트 여러 개에 걸쳐 있어야만 이 시험이
// 다중 파트를 실제로 건드린 것이다.
func TestPptxTemplateReversalReal(t *testing.T) {
	var ps []*opc.Package
	var names []string
	for _, n := range []string{"deck-a.pptx", "deck-b.pptx"} {
		p, err := opc.Open(filepath.Join("..", "..", "testdata", "real", n))
		if err != nil {
			t.Fatalf("Open %s: %v", n, err)
		}
		ps = append(ps, p)
		names = append(names, n)
	}

	tp, sch, errs, err := tmpl.Extract(ps, names, false)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}
	if len(sch.Keys) == 0 {
		t.Fatal("가변부가 하나도 안 잡혔다")
	}
	// 키가 여러 파트에 걸쳐 있어야 다중 파트 템플릿을 시험한 것이다
	seen := map[string]bool{}
	for _, k := range sch.Keys {
		seen[k.Part] = true
	}
	if len(seen) < 2 {
		t.Fatalf("키가 파트 %d개에만 있다 — 다중 파트를 시험하지 못했다", len(seen))
	}

	vals, err := tmpl.Values(ps[0], sch)
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	filled, err := opc.OpenBytes(mustBytes(t, tp))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if fe, err := tmpl.Fill(filled, sch, vals); err != nil || len(fe) != 0 {
		t.Fatalf("Fill: err=%v errs=%+v", err, fe)
	}

	for _, k := range sch.Keys {
		want, err := ps[0].Part(k.Part)
		if err != nil {
			t.Fatalf("Part: %v", err)
		}
		got, err := filled.Part(k.Part)
		if err != nil {
			t.Fatalf("Part: %v", err)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("I4a 위반 — %s 가 원본과 다르다", k.Part)
		}
	}
}

// I4b — 나머지 문서에 대한 텍스트 수준 일치
func TestTemplateReversalOthersTextLevel(t *testing.T) {
	ps, names := pkgs(t,
		[]string{"청구서", "홍길동", "1,200,000"},
		[]string{"청구서", "김철수", "880,000"},
	)

	tp, sch, errs, err := tmpl.Extract(ps, names, false)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}

	vals, err := tmpl.Values(ps[1], sch)
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	filled, err := opc.OpenBytes(mustBytes(t, tp))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if fe, err := tmpl.Fill(filled, sch, vals); err != nil || len(fe) != 0 {
		t.Fatalf("Fill: err=%v errs=%+v", err, fe)
	}

	wantTexts := textsOf(t, ps[1])
	gotTexts := textsOf(t, filled)
	if len(wantTexts) != len(gotTexts) {
		t.Fatalf("텍스트 노드 수 %d vs %d", len(gotTexts), len(wantTexts))
	}
	for i := range wantTexts {
		if wantTexts[i] != gotTexts[i] {
			t.Fatalf("I4b 위반 — 텍스트 %d: %q, 기대 %q", i, gotTexts[i], wantTexts[i])
		}
	}
}

func TestFillRejectsMissingKey(t *testing.T) {
	ps, names := pkgs(t, []string{"고정", "A"}, []string{"고정", "B"})
	tp, sch, errs, err := tmpl.Extract(ps, names, false)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}
	fe, err := tmpl.Fill(tp, sch, map[string]string{})
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if len(fe) != 1 || fe[0].Reason != "missing_key" {
		t.Fatalf("빠진 키가 거절되지 않았다: %+v", fe)
	}
}

func TestFillRejectsTemplateDrift(t *testing.T) {
	ps, names := pkgs(t, []string{"고정", "A"}, []string{"고정", "B"})
	tp, sch, errs, err := tmpl.Extract(ps, names, false)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}
	// 템플릿에서 자리표시자를 지운다
	c, err := tp.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	c = bytes.ReplaceAll(c, []byte("{{k1}}"), []byte("엉뚱한 값"))
	if err := tp.Replace("word/document.xml", c); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	fe, err := tmpl.Fill(tp, sch, map[string]string{"k1": "X"})
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if len(fe) != 1 || fe[0].Reason != "template_drift" {
		t.Fatalf("템플릿 드리프트가 거절되지 않았다: %+v", fe)
	}
}

// TestT2StructuralDifferenceIsRejectedByDefault 는 구조가 다른 문서를 플래그
// 없이 주면 거절하는지 본다.
//
// 정렬이 들어오면 Extract 가 구조 차이에도 **성공**하게 되는데, 그 성공은
// "이 템플릿은 입력 중 일부를 재현하지 못한다"는 단서가 붙은 성공이다.
// 단서는 무시할 수 있지만 실패는 무시할 수 없다 — 그래서 기본은 거절이다
// (설계 §3).
func TestT2StructuralDifferenceIsRejectedByDefault(t *testing.T) {
	a := testutil.MinimalDocx([]string{"첫 줄", "셋째 줄"})
	b := testutil.MinimalDocx([]string{"첫 줄", "새로 낀 줄", "셋째 줄"})
	pa, err := opc.OpenBytes(a)
	if err != nil {
		t.Fatalf("OpenBytes a: %v", err)
	}
	pb, err := opc.OpenBytes(b)
	if err != nil {
		t.Fatalf("OpenBytes b: %v", err)
	}
	_, _, errs, err := tmpl.Extract([]*opc.Package{pa, pb}, []string{"a.docx", "b.docx"}, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("거절 항목이 %d개 (기대 1): %+v", len(errs), errs)
	}
	if errs[0].Reason != "unrepresented_structure" {
		t.Fatalf("reason=%q (기대 unrepresented_structure)", errs[0].Reason)
	}
	if errs[0].Detail == "" {
		t.Fatal("detail 이 비었다 — 무엇을 표현 못 하는지 말해야 한다")
	}
}

// TestT3AllowFlagExtractsCommonPartAndReportsRest 는 플래그를 주면 공통부에서
// 키를 뽑고 매칭 안 된 서브트리를 빠짐없이 신고하는지 본다.
//
// **샘플 페어링에 대한 주** — 애초 이 테스트는 "셋째 줄"↔"셋째 줄!" 로 짝지어질
// 것으로 짜였지만, 실행해보면 실제로는 "셋째 줄"↔"새로 낀 줄" 로 짝지어지고
// "셋째 줄!" 문단이 unrepresented 로 빠진다. align.Match(건드릴 수 없는 패키지)
// 의 alignMiddle 이 공통 접두사(p[1] "첫 줄") 를 잘라낸 뒤 남은 구간에서 정확히
// 같은 서브트리 해시를 못 찾으면("셋째 줄" 은 "새로 낀 줄"·"셋째 줄!" 어느 쪽과도
// 해시가 다르다) 텍스트 유사도가 아니라 **위치(앞에서부터)** 로 짝짓기 때문이다
// (align.go Siblings/alignMiddle 의 'r' 폴백 — LCS 매치가 하나도 없으면 gap() 이
// 남은 구간 전체를 단일 'r' 오퍼레이션으로 묶고, Match.walk 가 그 안에서
// min(len(a),len(b)) 개를 인덱스 순서로 짝짓는다). 아래 기대값은 이 실측을
// 반영한다 — 문서 삽입 뒤 정렬이 "직관적으로 맞는" 문단을 찾아준다는 보장은
// 없다는 뜻이다.
func TestT3AllowFlagExtractsCommonPartAndReportsRest(t *testing.T) {
	// base 에 2문단, 다른 문서에 3문단(가운데 하나 삽입) + 마지막 문단 텍스트 변경.
	// 공통부의 가변 키는 "셋째 줄"↔"새로 낀 줄" 하나여야 하고(위 주 참조),
	// 매칭 안 된 서브트리는 b 에만 있는 "셋째 줄!" 문단 하나여야 한다.
	a := testutil.MinimalDocx([]string{"첫 줄", "셋째 줄"})
	b := testutil.MinimalDocx([]string{"첫 줄", "새로 낀 줄", "셋째 줄!"})
	pa, err := opc.OpenBytes(a)
	if err != nil {
		t.Fatalf("OpenBytes a: %v", err)
	}
	pb, err := opc.OpenBytes(b)
	if err != nil {
		t.Fatalf("OpenBytes b: %v", err)
	}
	_, sch, errs, err := tmpl.Extract([]*opc.Package{pa, pb}, []string{"a.docx", "b.docx"}, true)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("플래그를 줬는데 거절됐다: %+v", errs)
	}
	if len(sch.Unrepresented) != 1 {
		t.Fatalf("unrepresented 가 %d건 (기대 1 — b 에만 있는 문단 하나): %+v",
			len(sch.Unrepresented), sch.Unrepresented)
	}
	u := sch.Unrepresented[0]
	if u.Doc != "b.docx" {
		t.Errorf("doc=%q (기대 b.docx)", u.Doc)
	}
	if u.Part != "word/document.xml" {
		t.Errorf("part=%q", u.Part)
	}
	if u.Nodes == 0 {
		t.Error("nodes 가 0 이다 — 서브트리 무게를 말해야 한다")
	}
	// 공통부에서 키가 나와야 한다 — "셋째 줄" 이 위치 정렬로 "새로 낀 줄" 과 짝지어진
	// 자리(테스트 위 주 참조).
	if len(sch.Keys) != 1 {
		t.Fatalf("키가 %d개 (기대 1): %+v", len(sch.Keys), sch.Keys)
	}
	if sch.Keys[0].Samples[0] != "셋째 줄" || sch.Keys[0].Samples[1] != "새로 낀 줄" {
		t.Fatalf("샘플이 %v (기대 [셋째 줄 새로 낀 줄])", sch.Keys[0].Samples)
	}
}

// TestExtractReportsCappedAlignmentAsUnrepresented 는 정렬이 상한(align.MaxCells,
// 2000×2000)을 넘어 자식 정렬을 포기하고 위치로만 짝지은 경우, 그 밑에서 나온
// 값을 조용히 키로 신뢰하지 않는지 본다.
//
// 형제 2001개씩을 접두사가 겹치지 않게 만들어("A0".."A2000" 대 "B0".."B2000")
// 앞뒤 공통 잘라내기가 전혀 안 먹게 하면서도, 개수는 똑같이(2001=2001) 맞춰
// 위치 짝짓기 자체는 완전히 맞아떨어지게 한다 — OnlyA/OnlyB 는 하나도 안 나오게
// 해서 Capped 고유의 신호만 격리해서 본다.
func TestExtractReportsCappedAlignmentAsUnrepresented(t *testing.T) {
	const n = 2001 // 2001 × 2001 > align.MaxCells(4,000,000)
	// MinimalDocx 는 문단마다 다른 w14:paraId 를 한 글자('1'+i)로 채우는데, i 가
	// 커지면 그 한 바이트가 넘쳐(overflow) 임의 바이트(예: '<')를 속성값 안에
	// 그대로 흘려 XML 을 깨뜨린다 — 이 태스크와 무관한 testutil 의 기존 한계라
	// 건드리지 않고, paraId 가 없는 DocxWithBody 로 우회한다.
	mk := func(prefix string) []byte {
		var body strings.Builder
		for i := 0; i < n; i++ {
			body.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
			body.WriteString(prefix)
			body.WriteString(strconv.Itoa(i))
			body.WriteString(`</w:t></w:r></w:p>`)
		}
		return testutil.DocxWithBody(body.String())
	}
	pa, err := opc.OpenBytes(mk("A"))
	if err != nil {
		t.Fatalf("OpenBytes a: %v", err)
	}
	pb, err := opc.OpenBytes(mk("B"))
	if err != nil {
		t.Fatalf("OpenBytes b: %v", err)
	}

	_, _, errs, err := tmpl.Extract([]*opc.Package{pa, pb}, []string{"a.docx", "b.docx"}, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "unrepresented_structure" {
		t.Fatalf("상한 초과로 위치 짝짓기된 정렬이 거절되지 않았다: %+v", errs)
	}

	_, sch, errs, err := tmpl.Extract([]*opc.Package{pa, pb}, []string{"a.docx", "b.docx"}, true)
	if err != nil {
		t.Fatalf("Extract(allow): %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("플래그를 줬는데 거절됐다: %+v", errs)
	}
	if len(sch.Unrepresented) == 0 {
		t.Fatal("Capped 가 unrepresented 로 신고되지 않았다")
	}
}

// TestExtractMultiDocumentKeysMatchDocumentCount 는 문서 3벌에서 뽑은 키가
// 모두 문서 수만큼의 샘플을 갖고 빈 샘플이 없는지 본다 — 정확한 키 개수는
// 못 박지 않는다(정렬 결과를 실행해보지 않고는 알 수 없다). base 의 셋째
// 문단은 doc c 에서만 통째로 빠져 있어, base-vs-b 에서는 매칭되고 base-vs-c
// 에서는 매칭되지 않는 노드가 생긴다 — "모든 문서에서 매칭된 노드만 키
// 후보"(설계 §5 규칙 1) 를 3벌 이상에서도 지키는지가 이 테스트의 핵심이다.
func TestExtractMultiDocumentKeysMatchDocumentCount(t *testing.T) {
	ps, names := pkgs(t,
		[]string{"머리글", "가운데", "꼬리A"},
		[]string{"머리글", "가운데-B", "꼬리B"},
		[]string{"머리글", "꼬리C"}, // 가운데 문단이 통째로 빠졌다
	)
	_, sch, errs, err := tmpl.Extract(ps, names, true)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("플래그를 줬는데 거절됐다: %+v", errs)
	}
	for _, k := range sch.Keys {
		if len(k.Samples) != len(ps) {
			t.Fatalf("키 %s 의 샘플이 %d개 (기대 문서 수 %d개): %+v", k.Key, len(k.Samples), len(ps), k)
		}
		for i, s := range k.Samples {
			if s == "" {
				t.Fatalf("키 %s 의 샘플[%d] 이 비었다: %+v", k.Key, i, k)
			}
		}
	}
	if len(sch.Unrepresented) == 0 {
		t.Fatal("가운데 문단이 통째로 빠졌는데 unrepresented 가 하나도 없다")
	}
}

func mustBytes(t *testing.T, p *opc.Package) []byte {
	t.Helper()
	b, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return b
}

func textsOf(t *testing.T, p *opc.Package) []string {
	t.Helper()
	c, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	tr, err := xmlscan.Scan(c, "document")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var out []string
	for _, n := range tr.Nodes {
		if n.Type == "t" {
			out = append(out, n.Text)
		}
	}
	return out
}
