package tmpl_test

import (
	"bytes"
	"path/filepath"
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

	tp, sch, errs, err := tmpl.Extract(ps, names)
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
	_, sch, errs, err := tmpl.Extract(ps, names)
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

func TestExtractRejectsStructureMismatch(t *testing.T) {
	ps, names := pkgs(t,
		[]string{"A", "B", "C"},
		[]string{"A", "B"}, // 문단 수가 다르다
	)
	_, _, errs, err := tmpl.Extract(ps, names)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "structure_mismatch" {
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
	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "deck.pptx"})
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

	_, sch, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"})
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

	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"})
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

	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"})
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

	_, sch, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"})
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

	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"})
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

	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"})
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
	_, _, errs, err := tmpl.Extract(ps, names)
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

	tp, sch, errs, err := tmpl.Extract(ps, names)
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

	tp, sch, errs, err := tmpl.Extract(ps, names)
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

	tp, sch, errs, err := tmpl.Extract(ps, names)
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

	tp, sch, errs, err := tmpl.Extract(ps, names)
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
	tp, sch, errs, err := tmpl.Extract(ps, names)
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
	tp, sch, errs, err := tmpl.Extract(ps, names)
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
