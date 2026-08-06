package patch_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
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
			Path: "word/body[1]/p[1]",
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
		Ops:  []patch.Op{{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p><w:r><w:t>X</w:t></w:r></w:p>`}},
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

// I2 국소성 — 실제 Word 문서. 픽스처가 없으면 FAIL (spec §10).
func TestLocalityReal(t *testing.T) {
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
		Ops:  []patch.Op{{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p/>`}},
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
}

func TestAtomicityNothingAppliedOnBadPath(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p><w:r><w:t>유효</w:t></w:r></w:p>`},
			{Op: "replaceRaw", Path: "word/body[1]/p[99]", XML: `<w:p/>`},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("에러 %d개, 기대 1개: %+v", len(errs), errs)
	}
	if errs[0].Path != "word/body[1]/p[99]" || errs[0].Reason != "path_not_found" {
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

func TestHashMismatchRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Ops:  []patch.Op{{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p/>`}},
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
			{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p/>`},
			{Op: "replaceRaw", Path: "word/body[1]/p[1]/r[1]", XML: `<w:r/>`}, // p[1] 안쪽
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
		Ops:  []patch.Op{{Op: "setProps", Path: "word/body[1]/p[1]"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "unknown_op" {
		t.Fatalf("알 수 없는 연산이 거절되지 않았다: %+v", errs)
	}
}

func TestBrokenXMLIsInternalError(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	_, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p><w:r>`}},
	})
	if err == nil {
		t.Fatal("깨진 XML 을 넣었는데 에러가 없다")
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
