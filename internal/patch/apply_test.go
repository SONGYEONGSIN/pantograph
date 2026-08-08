package patch_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

func open(t *testing.T, src []byte) *opc.Package {
	t.Helper()
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	return p
}

// rawEntries 는 엔트리별 압축 데이터를 이름으로 모은다.
func rawEntries(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.OpenRaw()
		if err != nil {
			t.Fatalf("OpenRaw %s: %v", f.Name, err)
		}
		raw, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll %s: %v", f.Name, err)
		}
		out[f.Name] = raw
	}
	return out
}

func TestReplaceRawSubstitutesNode(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{{
			Op:   "replaceRaw",
			Part: "word/document.xml",
			Path: "document/body[1]/p[1]",
			XML:  `<w:p><w:r><w:t>바뀐 제목</w:t></w:r></w:p>`,
		}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("에러가 없어야 하는데: %+v", errs)
	}

	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("바뀐 제목")) {
		t.Fatalf("교체가 반영되지 않았다: %s", content)
	}
	if bytes.Contains(content, []byte(`w14:paraId="00000001"`)) {
		t.Fatalf("p[1] 이 통째로 교체되지 않았다: %s", content)
	}
	if !bytes.Contains(content, []byte("본문")) {
		t.Fatalf("p[2] 가 사라졌다: %s", content)
	}
}

// I2 국소성 — 건드리지 않은 엔트리는 압축 데이터까지 동일해야 한다.
func TestLocalityUntouchedEntriesAreByteIdentical(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[1]", XML: `<w:p><w:r><w:t>X</w:t></w:r></w:p>`}},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("Apply: err=%v errs=%+v", err, errs)
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	before, after := rawEntries(t, src), rawEntries(t, got)
	for name, wantRaw := range before {
		gotRaw, ok := after[name]
		if !ok {
			t.Fatalf("엔트리 사라짐: %s", name)
		}
		if name == "word/document.xml" {
			if bytes.Equal(wantRaw, gotRaw) {
				t.Fatal("수정한 파트인데 압축 데이터가 그대로다")
			}
			continue
		}
		if !bytes.Equal(wantRaw, gotRaw) {
			t.Fatalf("안 건드린 엔트리의 압축 데이터가 달라졌다: %s", name)
		}
	}
}

// I2 국소성 — 실제 Word·PowerPoint 문서. 픽스처가 없으면 FAIL (spec §10).
//
// pptx 는 docx 보다 국소성이 더 강한 주장이 된다(설계 §7) — 슬라이드마다
// 별도 파트이므로, 슬라이드 하나를 고쳤을 때 나머지 슬라이드 파트의 압축
// 데이터까지 원본과 완전히 같아야 한다. 텍스트 비교가 아니라 zip 원시
// (압축된) 바이트로 본다 — 압축 알고리즘이 같은 내용도 다른 바이트로
// 낼 수 있으므로, 압축 데이터가 같다는 것 자체가 "재압축하지 않고
// 건드리지 않았다"는 더 강한 증거다.
func TestLocalityReal(t *testing.T) {
	t.Run("docx", func(t *testing.T) {
		paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "real", "*.docx"))
		if err != nil {
			t.Fatalf("Glob: %v", err)
		}
		if len(paths) == 0 {
			t.Fatal("testdata/real/*.docx 없음 (spec §10)")
		}
		src, err := os.ReadFile(paths[0])
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		p := open(t, src)

		// 첫 문단을 빈 문단으로 교체 — document.xml 만 dirty 가 된다.
		errs, err := patch.Apply(p, patch.Patch{
			Hash: p.Hash,
			Ops:  []patch.Op{{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[1]", XML: `<w:p/>`}},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("에러: %+v", errs)
		}
		got, err := p.Bytes()
		if err != nil {
			t.Fatalf("Bytes: %v", err)
		}
		before, after := rawEntries(t, src), rawEntries(t, got)
		for name, wantRaw := range before {
			if name == "word/document.xml" {
				continue
			}
			if !bytes.Equal(wantRaw, after[name]) {
				t.Fatalf("안 건드린 엔트리의 압축 데이터가 달라졌다: %s", name)
			}
		}
	})

	t.Run("pptx", func(t *testing.T) {
		paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "real", "*.pptx"))
		if err != nil {
			t.Fatalf("Glob: %v", err)
		}
		if len(paths) == 0 {
			t.Fatal("testdata/real/*.pptx 없음 (spec §10)")
		}
		src, err := os.ReadFile(paths[0])
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		p := open(t, src)
		d, err := parts.Open(p)
		if err != nil {
			t.Fatalf("parts.Open: %v", err)
		}
		slides := d.Parts()
		if len(slides) < 2 {
			t.Fatalf("슬라이드 %d장 — 국소성 시험엔 2장 이상 필요하다", len(slides))
		}
		target := slides[0].Name
		tr, err := d.Tree(target)
		if err != nil {
			t.Fatalf("Tree: %v", err)
		}
		var path string
		for _, n := range tr.Nodes {
			if n.Type == "t" && n.Text != "" {
				path = n.Path
				break
			}
		}
		if path == "" {
			t.Fatalf("%s 에 텍스트 노드가 없다", target)
		}

		// 첫 슬라이드만 고친다 — 나머지 슬라이드 파트는 손대지 않는다.
		errs, err := patch.Apply(p, patch.Patch{
			Hash: p.Hash,
			Ops:  []patch.Op{{Op: "setText", Part: target, Path: path, Text: "국소성 시험"}},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("에러: %+v", errs)
		}
		got, err := p.Bytes()
		if err != nil {
			t.Fatalf("Bytes: %v", err)
		}
		before, after := rawEntries(t, src), rawEntries(t, got)
		for name, wantRaw := range before {
			if name == target {
				continue
			}
			if !bytes.Equal(wantRaw, after[name]) {
				t.Fatalf("안 건드린 엔트리의 압축 데이터가 달라졌다: %s", name)
			}
		}

		// 위 루프가 이미 나머지 슬라이드를 포함해 모든 엔트리를 훑지만, 여기서
		// 슬라이드 파트만 따로 짚어 "나머지 슬라이드가 그대로인가"를 직접 확인한다.
		untouchedSlides := 0
		for _, pt := range slides {
			if pt.Name == target {
				continue
			}
			if !bytes.Equal(before[pt.Name], after[pt.Name]) {
				t.Fatalf("건드리지 않은 슬라이드의 압축 데이터가 달라졌다: %s", pt.Name)
			}
			untouchedSlides++
		}
		if untouchedSlides == 0 {
			t.Fatal("확인할 다른 슬라이드가 없다 — 국소성 시험이 무의미하다")
		}
	})
}

func TestAtomicityNothingAppliedOnBadPath(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[1]", XML: `<w:p><w:r><w:t>유효</w:t></w:r></w:p>`},
			{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[99]", XML: `<w:p/>`},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("에러 %d개, 기대 1개: %+v", len(errs), errs)
	}
	if errs[0].Path != "document/body[1]/p[99]" || errs[0].Reason != "path_not_found" {
		t.Fatalf("에러가 부정확하다: %+v", errs[0])
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(src, got) {
		t.Fatal("실패한 패치인데 문서가 바뀌었다 — 원자성 위반")
	}
}

// TestPathNotFoundHintOnBareRootDoesNotNameRootAlias 는 "/" 가 없는 경로에
// path_not_found 가 나면 힌트(Detail)가 특정 루트 별칭을 이름으로 박아넣지
// 않는지 본다. 루트 별칭은 파트마다 다르다 — docx 는 "document", pptx 는
// "sld" 다. 힌트 문구가 특정 값을 하드코딩하면 다른 파트를 스캔할 때마다
// 거짓이 된다. 정확한 문구가 아니라 "특정 별칭을 이름으로 대지 않는다"는
// 실질만 고정한다 — 문구가 또 바뀌어도 이 테스트는 다시 쓸 필요가 없다.
func TestPathNotFoundHintOnBareRootDoesNotNameRootAlias(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "replaceRaw", Part: "word/document.xml", Path: "root", XML: `<w:p/>`}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "path_not_found" {
		t.Fatalf("에러가 부정확하다: %+v", errs)
	}
	detail := errs[0].Detail
	if detail == "" {
		t.Fatal("Detail 이 비어있다 — 힌트를 못 주는 이유를 말해야 한다")
	}
	for _, alias := range []string{`"word"`, `"document"`} {
		if strings.Contains(detail, alias) {
			t.Fatalf("Detail 이 특정 루트 별칭을 이름으로 박아넣었다: %q (별칭 %s 포함)", detail, alias)
		}
	}
}

// TestSetTextRejectionDetailsNameNoFormatElement 는 위 테스트와 같은 부류를
// setText 의 세 거절 사유에 대해 고정한다.
//
// Detail 은 stdout JSON 으로 나가는 사용자 대면 문구다. `w:t` 는 Word 의 접두사이며
// pptx 에서 같은 요소는 `a:t` 다 — 문구가 특정 포맷의 요소 이름을 박아넣으면
// 슬라이드를 패치할 때마다 거짓을 말한다. setText 의 규칙은 "Word 의 w:t 여야
// 한다"가 아니라 "지목된 노드가 텍스트 요소여야 한다"다.
//
// 정확한 문구가 아니라 "포맷 특정 요소 이름을 대지 않는다"는 실질만 고정한다.
func TestSetTextRejectionDetailsNameNoFormatElement(t *testing.T) {
	// t[1] 은 self-closing, t[2] 는 xml:space 없는 w:t, p[1] 은 텍스트 요소가 아니다
	src := testutil.DocxWithBody(
		`<w:p><w:r><w:t/></w:r><w:r><w:t>제목</w:t></w:r></w:p>`)

	cases := []struct {
		name, path, text, reason string
	}{
		{"type_mismatch", "document/body[1]/p[1]", "X", "type_mismatch"},
		{"self_closing", "document/body[1]/p[1]/r[1]/t[1]", "X", "self_closing_target"},
		{"whitespace", "document/body[1]/p[1]/r[2]/t[1]", " 공백 ", "whitespace_needs_preserve"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := open(t, src)
			errs, err := patch.Apply(p, patch.Patch{
				Hash: p.Hash,
				Ops:  []patch.Op{{Op: "setText", Part: "word/document.xml", Path: c.path, Text: c.text}},
			})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if len(errs) != 1 || errs[0].Reason != c.reason {
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
		})
	}
}

func TestHashMismatchRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Ops:  []patch.Op{{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[1]", XML: `<w:p/>`}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "hash_mismatch" {
		t.Fatalf("hash 불일치가 거절되지 않았다: %+v", errs)
	}
}

func TestOverlapRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[1]", XML: `<w:p/>`},
			{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]", XML: `<w:r/>`}, // p[1] 안쪽
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "overlap" {
		t.Fatalf("겹침이 거절되지 않았다: %+v", errs)
	}
}

func TestUnknownOpRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setProps", Part: "word/document.xml", Path: "document/body[1]/p[1]"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "unknown_op" {
		t.Fatalf("알 수 없는 연산이 거절되지 않았다: %+v", errs)
	}
}

// TestBrokenXMLIsInputError 는 재스캔 실패가 **입력 오류**로 보고되는지 본다.
//
// 결함은 전적으로 호출자가 준 XML 에 있다. 이것을 내부 오류(코드 2, stderr, 경로
// 없음)로 내보내면 spec §9 를 세 군데 어기고, 종료 코드로 재시도 여부를 가르는
// 에이전트를 "포기" 분기로 잘못 보낸다.
func TestBrokenXMLIsInputError(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[1]", XML: `<w:p><w:r>`}},
	})
	if err != nil {
		t.Fatalf("깨진 XML 이 내부 오류로 보고됐다: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "invalid_xml" {
		t.Fatalf("invalid_xml 로 거절되지 않았다: %+v", errs)
	}
	if errs[0].Path != "document/body[1]/p[1]" {
		t.Fatalf("문제를 일으킨 op 의 경로를 달지 않았다: %+v", errs[0])
	}
	// 거절이면 문서는 손대지 않은 상태여야 한다.
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(src, got) {
		t.Fatal("거절된 패치인데 문서가 바뀌었다")
	}
}

// TestBrokenXMLBlamesTheOffendingOp 는 유효한 op 여럿 사이에서 깨뜨린 op 을
// 정확히 지목하는지 본다.
func TestBrokenXMLBlamesTheOffendingOp(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목", "본문", "꼬리"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[1]", XML: `<w:p><w:r><w:t>정상</w:t></w:r></w:p>`},
			{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[2]", XML: `<w:p><w:r>`}, // 여기가 장본인
			{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[3]", XML: `<w:p/>`},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "invalid_xml" {
		t.Fatalf("invalid_xml 로 거절되지 않았다: %+v", errs)
	}
	if errs[0].Path != "document/body[1]/p[2]" {
		t.Fatalf("장본인이 아닌 경로를 지목했다: %+v", errs[0])
	}
}

// TestDuplicatePathRejectedOnEmptyTarget 는 F-I1 회귀 테스트다.
//
// 빈 <w:t></w:t> 의 안쪽 구간은 폭 0([p,p))이라 겹침 검사
// (splices[i].start < splices[i-1].end)가 p < p 로 거짓이 된다. 두 op 이 모두
// 게이트를 통과해 같은 지점에 스플라이스되면 텍스트가 조용히 이어붙는다.
func TestDuplicatePathRejectedOnEmptyTarget(t *testing.T) {
	p := open(t, testutil.DocxWithBody(`<w:p><w:r><w:t></w:t></w:r></w:p>`))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]", Text: "AAA"},
			{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]", Text: "BBB"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "duplicate_path" {
		t.Fatalf("같은 경로 중복이 거절되지 않았다: %+v", errs)
	}
	if errs[0].Path != "document/body[1]/p[1]/r[1]/t[1]" {
		t.Fatalf("에러가 중복된 경로를 지목하지 않는다: %+v", errs[0])
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if bytes.Contains(content, []byte("AAABBB")) {
		t.Fatalf("두 op 이 같은 지점에 이어붙었다: %s", content)
	}
}

// TestDuplicatePathRejectedOnNonEmptyTarget 는 폭이 있는 대상에서도 겹침이 아니라
// duplicate_path 로 거절되는지 본다 — 사유가 원인을 정확히 말해야 한다.
func TestDuplicatePathRejectedOnNonEmptyTarget(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]", Text: "AAA"},
			{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]", Text: "BBB"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "duplicate_path" {
		t.Fatalf("같은 경로 중복이 거절되지 않았다: %+v", errs)
	}
}

// TestSetTextRejectsSpaceAttrInOtherNamespace 는 xml:space 를 로컬명만으로
// 판정하지 않는지 본다. 다른 네임스페이스의 space 속성은 xml:space 가 아니므로
// 앞뒤 공백을 허용하는 근거가 될 수 없다.
func TestSetTextRejectsSpaceAttrInOtherNamespace(t *testing.T) {
	p := open(t, testutil.DocxWithBody(`<w:p><w:r><w:t w:space="preserve">제목</w:t></w:r></w:p>`))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]", Text: " 앞뒤 공백 "}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "whitespace_needs_preserve" {
		t.Fatalf("xml: 이 아닌 네임스페이스의 space 가 preserve 로 인정됐다: %+v", errs)
	}
}

func TestEmptyPatchIsIdentity(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)
	errs, err := patch.Apply(p, patch.Patch{Hash: p.Hash})
	if err != nil || len(errs) != 0 {
		t.Fatalf("Apply: err=%v errs=%+v", err, errs)
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(src, got) {
		t.Fatal("빈 패치인데 바이트가 달라졌다 (I1)")
	}
}

func TestSetTextReplacesOnlyInnerText(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]", Text: "새 제목"}},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("Apply: err=%v errs=%+v", err, errs)
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte(`<w:t xml:space="preserve">새 제목</w:t>`)) {
		t.Fatalf("시작 태그가 보존되지 않았거나 텍스트가 안 바뀌었다: %s", content)
	}
	if !bytes.Contains(content, []byte(`w14:paraId="00000001"`)) {
		t.Fatalf("문단의 휘발성 속성이 사라졌다: %s", content)
	}
}

func TestSetTextEscapesMinimally(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]", Text: "a&b<c>d\ne"}},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("Apply: err=%v errs=%+v", err, errs)
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("a&amp;b&lt;c&gt;d\ne")) {
		t.Fatalf("이스케이프가 최소가 아니다 (개행이 문자 참조로 바뀌면 안 된다): %s", content)
	}
}

func TestSetTextRejectsNonTextNode(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]", Text: "X"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "type_mismatch" {
		t.Fatalf("w:t 가 아닌 노드가 거절되지 않았다: %+v", errs)
	}
}

func TestSetTextRejectsWhitespaceWithoutPreserve(t *testing.T) {
	// xml:space 속성이 없는 w:t
	src := []byte(`<w:document xmlns:w="http://x"><w:body><w:p><w:r><w:t>제목</w:t></w:r></w:p></w:body></w:document>`)
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	if err := p.Replace("word/document.xml", src); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	errs, err := patch.Apply(p, patch.Patch{
		Ops: []patch.Op{{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]", Text: " 앞뒤 공백 "}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "whitespace_needs_preserve" {
		t.Fatalf("공백이 거절되지 않았다: %+v", errs)
	}
}

func TestSetTextRejectsSelfClosingTarget(t *testing.T) {
	// self-closing <w:t/> 는 시작/종료 태그가 하나로 합쳐져 있어 '안쪽'이 없다.
	// xmlscan.Scan 의 Inner 는 이 경우 요소 바로 뒤의 폭 0 위치를 가리킨다.
	src := []byte(`<w:document xmlns:w="http://x"><w:body><w:p><w:r><w:t/></w:r></w:p></w:body></w:document>`)
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	if err := p.Replace("word/document.xml", src); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	before, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	beforeCopy := append([]byte(nil), before...)

	errs, err := patch.Apply(p, patch.Patch{
		Ops: []patch.Op{{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]", Text: "새 텍스트"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "self_closing_target" {
		t.Fatalf("self-closing 대상이 거절되지 않았다: %+v", errs)
	}
	after, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Equal(beforeCopy, after) {
		t.Fatalf("거절됐는데 문서가 바뀌었다:\nbefore=%s\nafter=%s", beforeCopy, after)
	}
}

func TestSetTextAllowsWhitespaceWithPreserve(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"})) // 생성기가 preserve 를 붙인다
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]", Text: " 앞뒤 공백 "}},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("preserve 가 있는데 거절됐다: err=%v errs=%+v", err, errs)
	}
}

func realPkg(t *testing.T, name string) *opc.Package {
	t.Helper()
	p, err := opc.Open(filepath.Join("..", "..", "testdata", "real", name))
	if err != nil {
		t.Fatalf("Open %s: %v", name, err)
	}
	return p
}

func slideText(t *testing.T, p *opc.Package, part string) string {
	t.Helper()
	b, err := p.Part(part)
	if err != nil {
		t.Fatalf("Part %s: %v", part, err)
	}
	return string(b)
}

func TestApplyAcrossParts(t *testing.T) {
	p := realPkg(t, "deck-a.pptx")
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	s1, s2 := d.Parts()[0].Name, d.Parts()[1].Name

	// 각 슬라이드의 첫 a:t 노드를 찾는다
	find := func(part string) string {
		tr, err := d.Tree(part)
		if err != nil {
			t.Fatalf("Tree: %v", err)
		}
		for _, n := range tr.Nodes {
			if n.Type == "t" && n.Text != "" {
				return n.Path
			}
		}
		t.Fatalf("%s 에 텍스트 노드가 없다", part)
		return ""
	}
	p1, p2 := find(s1), find(s2)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "setText", Part: s1, Path: p1, Text: "첫째 바뀜"},
			{Op: "setText", Part: "pptx/slide[2]", Path: p2, Text: "둘째 바뀜"},
		},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("Apply: err=%v errs=%+v", err, errs)
	}
	if !strings.Contains(slideText(t, p, s1), "첫째 바뀜") {
		t.Error("슬라이드 1 이 안 바뀌었다")
	}
	if !strings.Contains(slideText(t, p, s2), "둘째 바뀜") {
		t.Error("슬라이드 2 가 논리 참조로 안 바뀌었다")
	}
}

func TestApplyAtomicAcrossParts(t *testing.T) {
	p := realPkg(t, "deck-a.pptx")
	before, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	s1 := d.Parts()[0].Name
	tr, _ := d.Tree(s1)
	var valid string
	for _, n := range tr.Nodes {
		if n.Type == "t" && n.Text != "" {
			valid = n.Path
			break
		}
	}

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "setText", Part: s1, Path: valid, Text: "유효"},
			{Op: "setText", Part: "pptx/slide[2]", Path: "sld/없는[99]", Text: "무효"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "path_not_found" {
		t.Fatalf("에러가 부정확하다: %+v", errs)
	}
	after, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("한 파트가 무효인데 다른 파트가 바뀌었다 — 원자성 위반")
	}
}

func TestPartResolutionErrors(t *testing.T) {
	p := realPkg(t, "deck-a.pptx")
	cases := []struct{ part, reason string }{
		{"ppt/slides/slide99.xml", "part_not_found"},
		{"pptx/slide[99]", "ref_not_found"},
		{"ppt/theme/theme1.xml", "part_not_scannable"},
	}
	for _, c := range cases {
		errs, err := patch.Apply(p, patch.Patch{
			Hash: p.Hash,
			Ops:  []patch.Op{{Op: "setText", Part: c.part, Path: "sld", Text: "x"}},
		})
		if err != nil {
			t.Fatalf("%s: Apply: %v", c.part, err)
		}
		if len(errs) != 1 || errs[0].Reason != c.reason {
			t.Errorf("%s → %+v, want reason %s", c.part, errs, c.reason)
		}
	}
}

func TestOverlapIsPerPart(t *testing.T) {
	p := realPkg(t, "deck-a.pptx")
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	// 두 슬라이드에서 같은 경로(구조가 같으므로 오프셋도 겹칠 수 있다)를 동시에 고친다
	s1, s2 := d.Parts()[0].Name, d.Parts()[1].Name
	find := func(part string) string {
		tr, _ := d.Tree(part)
		for _, n := range tr.Nodes {
			if n.Type == "t" && n.Text != "" {
				return n.Path
			}
		}
		return ""
	}
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "setText", Part: s1, Path: find(s1), Text: "A"},
			{Op: "setText", Part: s2, Path: find(s2), Text: "B"},
		},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("다른 파트인데 겹침으로 거절됐다: err=%v errs=%+v", err, errs)
	}
}

func TestLazyPartLoading(t *testing.T) {
	p := realPkg(t, "deck-a.pptx")
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	target := d.Parts()[1].Name
	tr, _ := d.Tree(target)
	var path string
	for _, n := range tr.Nodes {
		if n.Type == "t" && n.Text != "" {
			path = n.Path
			break
		}
	}

	// Apply 는 자기 Document 를 새로 연다. 그 안에서 몇 개를 스캔하는지 본다.
	loaded := patch.PartsLoadedBy(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Part: target, Path: path, Text: "X"}},
	})
	if len(loaded) != 1 || loaded[0] != target {
		t.Fatalf("스캔된 파트 %v — 1개(%s)만 스캔해야 한다", loaded, target)
	}
}
