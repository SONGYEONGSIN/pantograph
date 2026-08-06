package tmpl_test

import (
	"bytes"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
	"github.com/SONGYEONGSIN/pantograph/internal/tmpl"
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
	if k.Path != "word/body[1]/p[2]/r[1]/t[1]" {
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
	if sch.Keys[0].Key != "k1" || sch.Keys[0].Path != "word/body[1]/p[1]/r[1]/t[1]" {
		t.Fatalf("k1 이 문서 순서의 첫 가변부가 아니다: %+v", sch.Keys[0])
	}
	if sch.Keys[1].Key != "k2" || sch.Keys[1].Path != "word/body[1]/p[3]/r[1]/t[1]" {
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
