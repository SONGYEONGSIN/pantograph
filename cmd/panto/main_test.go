package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

// TestWriteAtomicFailureLeavesDestinationUntouched 는 콜백이 도중에 실패해도
// 대상 파일이 훼손되지 않고, 임시 파일도 남지 않아야 함을 검증한다.
func TestWriteAtomicFailureLeavesDestinationUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.docx")
	sentinel := []byte("원본 파일 내용 — 절대 사라지면 안 된다")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatalf("사전 파일 생성 실패: %v", err)
	}

	wantErr := errors.New("디스크 꽉 참")
	err := writeAtomic(path, func(w io.Writer) error {
		if _, err := w.Write([]byte("일부만 쓰고 실패")); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("writeAtomic 이 콜백 에러를 전파하지 않았다: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("대상 파일 읽기 실패: %v", err)
	}
	if !bytes.Equal(got, sentinel) {
		t.Fatalf("실패한 쓰기가 대상 파일을 훼손했다: got=%q want=%q", got, sentinel)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("디렉토리 읽기 실패: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "out.docx" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("임시 파일이 남아있다: %v", names)
	}
}

// captureStdout 는 f 실행 중 os.Stdout 을 파이프로 바꿔 emit() 이 쓴 내용을 돌려준다.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	f()
	os.Stdout = orig
	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("파이프 읽기 실패: %v", err)
	}
	return buf.String()
}

// TestTmplFillRejectsZeroKeySchema 는 리뷰 Finding 1 — 스키마에 키가
// 하나도 없으면 (예: extract 를 동일 문서 2벌로 돌려 만든 스키마, 또는
// "keys" 필드가 없는 아무 JSON) missing_key/template_drift 검사를
// 전혀 거치지 않고 조용히 "ok": true 로 원본 그대로의 사본을 내보내는
// 문제를 재현한다. Fill 이 빈 ops 로 patch.Apply 를 호출하면
// (nil, nil) 이 돌아와 거절 없이 writeAtomic 까지 도달한다.
func TestTmplFillRejectsZeroKeySchema(t *testing.T) {
	dir := t.TempDir()

	tplPath := filepath.Join(dir, "tmpl.docx")
	if err := os.WriteFile(tplPath, testutil.MinimalDocx([]string{"청구서", "{{k1}}"}), 0o644); err != nil {
		t.Fatalf("템플릿 파일 쓰기 실패: %v", err)
	}
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{"base":"a.docx","hash":"sha256:x","keys":[]}`), 0o644); err != nil {
		t.Fatalf("스키마 파일 쓰기 실패: %v", err)
	}
	dataPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(dataPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("데이터 파일 쓰기 실패: %v", err)
	}
	outPath := filepath.Join(dir, "out.docx")

	var code int
	stdout := captureStdout(t, func() {
		code = cmdTmplFill([]string{tplPath, "--schema", schemaPath, "-d", dataPath, "-o", outPath})
	})

	if code != exitInput {
		extra := ""
		if out, err := os.ReadFile(outPath); err == nil {
			extra = fmt.Sprintf(" — 출력 파일이 만들어졌고 내용은: %s", out)
		}
		t.Fatalf("빈 keys 스키마인데 exit=%d (기대 %d), stdout=%s%s", code, exitInput, stdout, extra)
	}
	if _, err := os.Stat(outPath); err == nil {
		out, _ := os.ReadFile(outPath)
		t.Fatalf("빈 keys 스키마인데 출력 파일이 만들어졌다: %s", out)
	} else if !os.IsNotExist(err) {
		t.Fatalf("출력 파일 상태 확인 실패: %v", err)
	}
}

// TestWriteAtomicSuccessReplacesDestination 은 성공 경로에서 콜백이 쓴 바이트
// 그대로 대상 파일에 반영됨을 검증한다.
func TestWriteAtomicSuccessReplacesDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.docx")
	want := []byte("새로 쓴 내용")

	err := writeAtomic(path, func(w io.Writer) error {
		_, err := w.Write(want)
		return err
	})
	if err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("대상 파일 읽기 실패: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("대상 파일 내용이 다르다: got=%q want=%q", got, want)
	}

	// os.CreateTemp 은 0600 으로 만든다. 그대로 rename 하면 산출물이 소유자
	// 전용이 되어, os.Create 로 만들었을 때(보통 0644)와 권한이 달라진다.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o644 {
		t.Fatalf("산출물 권한 %04o, 기대 0644", perm)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("디렉토리 읽기 실패: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "out.docx" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("임시 파일이 남아있다: %v", names)
	}
}

// TestApplyReportsUnsupportedContainerAsInputError 는 재현할 수 없는 zip 이
// **입력 오류**로 보고되는지 본다 — stdout JSON + 종료 코드 1.
//
// 내부 오류(코드 2, stderr)로 내보내면, 종료 코드로 재시도 여부를 가르는
// 에이전트가 "이 파일은 이 도구로 다룰 수 없다"를 "도구가 고장났다"로 읽는다.
func TestApplyReportsUnsupportedContainerAsInputError(t *testing.T) {
	dir := t.TempDir()

	// 중앙 레코드의 압축 크기를 ZIP64 sentinel 로 만든다 — 실제 값이 32비트
	// 필드 밖(zip64 확장)에 있다는 뜻이라 이 writer 가 재조립할 수 없다.
	src := testutil.MinimalDocx([]string{"제목"})
	i := bytes.Index(src, []byte("PK\x01\x02"))
	if i < 0 {
		t.Fatal("중앙 디렉토리 레코드를 못 찾았다")
	}
	binary.LittleEndian.PutUint32(src[i+20:], 0xFFFFFFFF)

	inPath := filepath.Join(dir, "in.docx")
	if err := os.WriteFile(inPath, src, 0o644); err != nil {
		t.Fatalf("입력 파일 쓰기 실패: %v", err)
	}
	patchPath := filepath.Join(dir, "patch.json")
	if err := os.WriteFile(patchPath, []byte(`{"ops":[]}`), 0o644); err != nil {
		t.Fatalf("패치 파일 쓰기 실패: %v", err)
	}
	outPath := filepath.Join(dir, "out.docx")

	var code int
	stdout := captureStdout(t, func() {
		code = cmdApply([]string{inPath, "-p", patchPath, "-o", outPath})
	})

	if code != exitInput {
		t.Fatalf("재현 불가 컨테이너인데 exit=%d (기대 %d), stdout=%s", code, exitInput, stdout)
	}
	if !strings.Contains(stdout, "unsupported_container") {
		t.Fatalf("stdout 에 unsupported_container 가 없다: %s", stdout)
	}
	if !strings.Contains(stdout, inPath) {
		t.Fatalf("stdout 이 문제의 파일 경로를 달지 않았다: %s", stdout)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("거절됐는데 출력 파일이 만들어졌다: %v", err)
	}
}

// unsupportedMethodDocx 는 word/document.xml 의 압축 방식만 12(bzip2)로 바꾼
// docx 를 만들어 경로를 돌려준다.
//
// 컨테이너 자체는 바이트 그대로 재현되므로 **열기 시점 게이트는 통과한다**.
// 그 파트를 풀려는 순간에야 UnsupportedError 가 난다 — 즉 openInput 을 거치지
// 않고 나오는 UnsupportedError 다. 이 종류가 종료 코드 1 로 나오는지가 핵심이다.
func unsupportedMethodDocx(t *testing.T, path string) string {
	t.Helper()
	src := testutil.MinimalDocx([]string{"제목", "본문"})

	eocd := bytes.LastIndex(src, []byte("PK\x05\x06"))
	if eocd < 0 {
		t.Fatal("EOCD 를 못 찾았다")
	}
	off := int(binary.LittleEndian.Uint32(src[eocd+16:]))
	found := false
	for range int(binary.LittleEndian.Uint16(src[eocd+10:])) {
		nameLen := int(binary.LittleEndian.Uint16(src[off+28:]))
		if string(src[off+46:off+46+nameLen]) == "word/document.xml" {
			ls := int(binary.LittleEndian.Uint32(src[off+42:]))
			binary.LittleEndian.PutUint16(src[off+10:], 12) // 중앙 레코드
			binary.LittleEndian.PutUint16(src[ls+8:], 12)   // 로컬 헤더
			found = true
			break
		}
		off += 46 + nameLen + int(binary.LittleEndian.Uint16(src[off+30:])) +
			int(binary.LittleEndian.Uint16(src[off+32:]))
	}
	if !found {
		t.Fatal("word/document.xml 엔트리를 못 찾았다")
	}
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatalf("입력 파일 쓰기 실패: %v", err)
	}
	return path
}

// assertUnsupportedReported 는 명령이 입력 오류(코드 1) + stdout JSON 으로
// 보고했는지 본다. 코드 2(내부 오류)로 나가면 종료 코드로 재시도 여부를 가르는
// 에이전트가 "이 입력은 영원히 안 된다"를 "도구가 고장났다"로 읽는다 (spec §9).
func assertUnsupportedReported(t *testing.T, code int, stdout, wantPath string) {
	t.Helper()
	if code != exitInput {
		t.Fatalf("exit=%d (기대 %d) — UnsupportedError 가 내부 오류로 샜다. stdout=%s", code, exitInput, stdout)
	}
	if !strings.Contains(stdout, "unsupported_container") {
		t.Fatalf("stdout 에 unsupported_container 가 없다: %s", stdout)
	}
	if !strings.Contains(stdout, wantPath) {
		t.Fatalf("stdout 이 문제의 파일 경로를 달지 않았다: %s", stdout)
	}
}

// TestDumpReportsUnsupportedMethodAsInputError 는 열기 이후에 나는
// UnsupportedError(미지원 압축 방식)도 dump 에서 코드 1 로 나오는지 본다.
func TestDumpReportsUnsupportedMethodAsInputError(t *testing.T) {
	dir := t.TempDir()
	in := unsupportedMethodDocx(t, filepath.Join(dir, "in.docx"))

	var code int
	stdout := captureStdout(t, func() { code = cmdDump([]string{in}) })
	assertUnsupportedReported(t, code, stdout, in)
}

// TestDumpBadSelectorIsInputError 는 --part 선택자가 아무 파트도 못 고르면
// exit 1 + part_not_found 로 stdout JSON 보고되는지 본다 (리뷰 라운드 Finding 1).
// 이전엔 dump.Build 의 선택자 실패가 fail() 을 타 내부 오류(exit 2, stderr)로 샜다 —
// 사용자 오탈자가 "도구가 고장났다"로 잘못 읽힌다.
func TestDumpBadSelectorIsInputError(t *testing.T) {
	var code int
	stdout := captureStdout(t, func() {
		code = cmdDump([]string{
			filepath.Join("..", "..", "testdata", "real", "deck-a.pptx"),
			"--part", "ppt/nope/*",
		})
	})
	if code != exitInput {
		t.Fatalf("잘못된 선택자인데 exit=%d (기대 %d), stdout=%s", code, exitInput, stdout)
	}
	if !strings.Contains(stdout, "part_not_found") {
		t.Fatalf("stdout 에 part_not_found 가 없다: %s", stdout)
	}
}

// TestApplyReportsUnsupportedMethodAsInputError 는 같은 것을 apply 에서 본다.
// Apply 는 이제 op 이 지목한 파트만 지연 스캔한다 (Task 6) — 빈 패치는
// 아무 파트도 스캔하지 않으므로, op 이 word/document.xml 을 실제로
// 가리켜야 이 경로가 탄다.
func TestApplyReportsUnsupportedMethodAsInputError(t *testing.T) {
	dir := t.TempDir()
	in := unsupportedMethodDocx(t, filepath.Join(dir, "in.docx"))
	patchPath := filepath.Join(dir, "patch.json")
	patchJSON := `{"ops":[{"op":"setText","path":"document/body[1]/p[1]/r[1]/t[1]","text":"x"}]}`
	if err := os.WriteFile(patchPath, []byte(patchJSON), 0o644); err != nil {
		t.Fatalf("패치 파일 쓰기 실패: %v", err)
	}
	outPath := filepath.Join(dir, "out.docx")

	var code int
	stdout := captureStdout(t, func() {
		code = cmdApply([]string{in, "-p", patchPath, "-o", outPath})
	})
	assertUnsupportedReported(t, code, stdout, in)
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("거절됐는데 출력 파일이 만들어졌다: %v", err)
	}
}

// TestTmplExtractReportsUnsupportedMethodAsInputError 는 tmpl extract 에서 본다.
func TestTmplExtractReportsUnsupportedMethodAsInputError(t *testing.T) {
	dir := t.TempDir()
	a := unsupportedMethodDocx(t, filepath.Join(dir, "a.docx"))
	b := unsupportedMethodDocx(t, filepath.Join(dir, "b.docx"))

	var code int
	stdout := captureStdout(t, func() {
		code = cmdTmplExtract([]string{a, b, "-o", filepath.Join(dir, "t.docx"),
			"--schema", filepath.Join(dir, "schema.json")})
	})
	assertUnsupportedReported(t, code, stdout, a)
}

// TestTmplFillReportsUnsupportedMethodAsInputError 는 tmpl fill 에서 본다.
func TestTmplFillReportsUnsupportedMethodAsInputError(t *testing.T) {
	dir := t.TempDir()
	in := unsupportedMethodDocx(t, filepath.Join(dir, "t.docx"))
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath,
		[]byte(`{"base":"t.docx","keys":[{"key":"k1","path":"document/body[1]/p[1]/r[1]/t[1]"}]}`), 0o644); err != nil {
		t.Fatalf("스키마 파일 쓰기 실패: %v", err)
	}
	dataPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(dataPath, []byte(`{"k1":"값"}`), 0o644); err != nil {
		t.Fatalf("데이터 파일 쓰기 실패: %v", err)
	}

	var code int
	stdout := captureStdout(t, func() {
		code = cmdTmplFill([]string{in, "--schema", schemaPath, "-d", dataPath,
			"-o", filepath.Join(dir, "out.docx")})
	})
	assertUnsupportedReported(t, code, stdout, in)
}

// bodylessContainer 는 알려진 본문 파트가 하나도 없는 최소 OPC 컨테이너를 만든다.
// 컨테이너 게이트(opc.Open)는 통과하고 parts.Plan 에서만 걸린다 — xlsx 를 넣는
// 것이 이 부류의 대표 입력이다 (범위 밖 포맷, spec §1).
func bodylessContainer(t *testing.T, path string) string {
	t.Helper()
	src := testutil.ZipOf(map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="xml" ContentType="text/xml"/></Types>`,
		"junk.xml": `<a/>`,
	})
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatalf("입력 파일 쓰기 실패: %v", err)
	}
	return path
}

// TestTmplExtractOnBodylessContainerIsInputError 는 본문 파트가 없는 컨테이너를
// tmpl extract 에 주면 **입력 오류**(코드 1 + stdout JSON unsupported_format)로
// 나오는지 본다.
//
// 사용자가 이 기능에서 가장 먼저 저지를 실수가 `panto tmpl extract a.xlsx b.xlsx`
// 다. tmpl 은 parts.Open 의 오류를 맨 error 채널로 흘려보내고 fail() 이
// *opc.UnsupportedError 만 알아봤기 때문에 코드 2(내부 오류)로 샜다 — 같은 오류를
// dump 는 코드 1 로 낸다. 종료 코드로 재시도를 가르는 에이전트는 2 를 "도구가
// 고장났으니 에스컬레이션"으로, 1 을 "입력이 틀렸으니 재시도 말 것"으로 읽는다
// (spec §4·§8: unsupported_format 은 코드 1).
func TestTmplExtractOnBodylessContainerIsInputError(t *testing.T) {
	dir := t.TempDir()
	a := bodylessContainer(t, filepath.Join(dir, "a.xlsx"))
	b := bodylessContainer(t, filepath.Join(dir, "b.xlsx"))

	var code int
	stdout := captureStdout(t, func() {
		code = cmdTmplExtract([]string{a, b, "-o", filepath.Join(dir, "t.xlsx"),
			"--schema", filepath.Join(dir, "schema.json")})
	})
	if code != exitInput {
		t.Fatalf("본문 파트 없는 컨테이너인데 exit=%d (기대 %d), stdout=%s", code, exitInput, stdout)
	}
	if !strings.Contains(stdout, "unsupported_format") {
		t.Fatalf("stdout 에 unsupported_format 이 없다: %s", stdout)
	}
}

// TestApplyOnBrokenPresentationIsInputError 는 unsupported_format 이 "본문 파트를
// 못 찾았다" 한 경우만이 아님을 고정한다. Plan 의 **모든** 실패 —
// [Content_Types].xml 없음, presentation.xml 파싱 실패, rId 미해석, 슬라이드가
// 아닌 ContentType — 가 같은 부류이며 세 명령 모두 코드 1 로 낸다.
//
// 이 픽스처는 슬라이드 ContentType 은 선언하지만 presentation.xml 이 XML 로
// 읽히지 않는다. 부류로 묶지 않으면 이 경로만 apply·tmpl 에서 코드 2 로 샌다.
func TestApplyOnBrokenPresentationIsInputError(t *testing.T) {
	dir := t.TempDir()
	const slideCT = `application/vnd.openxmlformats-officedocument.presentationml.slide+xml`
	src := testutil.ZipOf(map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Override PartName="/ppt/slides/slide1.xml" ContentType="` + slideCT + `"/></Types>`,
		"ppt/presentation.xml":            `<p:presentation><이건 XML 이 아니다`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
		"ppt/slides/slide1.xml":           `<p:sld/>`,
	})
	inPath := filepath.Join(dir, "in.pptx")
	if err := os.WriteFile(inPath, src, 0o644); err != nil {
		t.Fatalf("입력 파일 쓰기 실패: %v", err)
	}
	patchPath := filepath.Join(dir, "patch.json")
	if err := os.WriteFile(patchPath, []byte(`{"ops":[]}`), 0o644); err != nil {
		t.Fatalf("패치 파일 쓰기 실패: %v", err)
	}

	var code int
	stdout := captureStdout(t, func() {
		code = cmdApply([]string{inPath, "-p", patchPath, "-o", filepath.Join(dir, "out.pptx")})
	})
	if code != exitInput {
		t.Fatalf("presentation.xml 이 깨진 컨테이너인데 exit=%d (기대 %d), stdout=%s", code, exitInput, stdout)
	}
	if !strings.Contains(stdout, "unsupported_format") {
		t.Fatalf("stdout 에 unsupported_format 이 없다: %s", stdout)
	}
}

// TestDumpSelectorReasonsMatchApply 는 --part 가 아무 파트도 못 고를 때의 사유가
// apply 의 op.part 해석과 같은지 본다.
//
// patch.resolvePart 는 part_not_found / ref_not_found / part_not_scannable 을
// 구분하는데 dump 의 Select 는 전부 하나로 뭉쳐 part_not_found 로 냈다 — 같은
// 질문에 두 답이 나오면 에이전트가 어느 쪽을 믿어야 할지 알 수 없다.
// 사유 판정은 한 곳(parts)에만 있어야 한다.
func TestDumpSelectorReasonsMatchApply(t *testing.T) {
	deck := filepath.Join("..", "..", "testdata", "real", "deck-a.pptx")
	cases := []struct{ sel, reason string }{
		{"ppt/theme/theme1.xml", "part_not_scannable"}, // 컨테이너엔 있으나 본문 파트가 아니다
		{"pptx/slide[99]", "ref_not_found"},            // 논리 참조 모양인데 안 풀린다
		{"ppt/nope/*", "part_not_found"},               // 아무것도 못 고르는 glob
		{"ppt/theme/*", "part_not_scannable"},          // 컨테이너 엔트리는 고르지만 전부 본문 파트가 아니다
	}
	for _, c := range cases {
		var code int
		stdout := captureStdout(t, func() { code = cmdDump([]string{deck, "--part", c.sel}) })
		if code != exitInput {
			t.Errorf("%s: exit=%d (기대 %d), stdout=%s", c.sel, code, exitInput, stdout)
			continue
		}
		if !strings.Contains(stdout, c.reason) {
			t.Errorf("%s: stdout 에 %s 가 없다: %s", c.sel, c.reason, stdout)
		}
	}
}

// TestApplyEmptyPatchIsByteIdentical 은 CLI 전 경로(파일 읽기 → Apply →
// writeAtomic)를 지나도 빈 패치가 바이트 동일 산출을 내는지 본다 (I1).
func TestApplyEmptyPatchIsByteIdentical(t *testing.T) {
	dir := t.TempDir()
	src := testutil.MinimalDocx([]string{"제목", "본문 한 줄"})

	inPath := filepath.Join(dir, "in.docx")
	if err := os.WriteFile(inPath, src, 0o644); err != nil {
		t.Fatalf("입력 파일 쓰기 실패: %v", err)
	}
	patchPath := filepath.Join(dir, "patch.json")
	if err := os.WriteFile(patchPath, []byte(`{"ops":[]}`), 0o644); err != nil {
		t.Fatalf("패치 파일 쓰기 실패: %v", err)
	}
	outPath := filepath.Join(dir, "out.docx")

	var code int
	stdout := captureStdout(t, func() {
		code = cmdApply([]string{inPath, "-p", patchPath, "-o", outPath})
	})
	if code != exitOK {
		t.Fatalf("빈 패치인데 exit=%d, stdout=%s", code, stdout)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("출력 파일 읽기 실패: %v", err)
	}
	if !bytes.Equal(src, got) {
		t.Fatalf("I1 위반 — 빈 패치인데 바이트가 달라졌다 (%d -> %d바이트)", len(src), len(got))
	}
}

// TestApplyRejectsUnknownPatchField 는 op 의 필드 이름을 잘못 쓴 패치를
// 거절하는지 본다.
//
// encoding/json 은 모르는 필드를 조용히 버린다. 그래서 "text" 를 "value" 로
// 잘못 쓴 setText 는 빈 문자열로 성공했다 — {"ok": true} 를 내면서 문서의
// 텍스트를 지웠다. 빈 텍스트 자체는 정당한 연산이라 결과만 봐서는 오타와
// 구별되지 않으므로, 막을 수 있는 지점은 디코드뿐이다.
func TestApplyRejectsUnknownPatchField(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.docx")
	src := testutil.MinimalDocx([]string{"지켜져야 할 텍스트"})
	if err := os.WriteFile(inPath, src, 0o644); err != nil {
		t.Fatalf("입력 파일 쓰기 실패: %v", err)
	}
	patchPath := filepath.Join(dir, "patch.json")
	bad := `{"ops":[{"op":"setText","path":"document/body[1]/p[1]/r[1]/t[1]","value":"오타"}]}`
	if err := os.WriteFile(patchPath, []byte(bad), 0o644); err != nil {
		t.Fatalf("패치 파일 쓰기 실패: %v", err)
	}
	outPath := filepath.Join(dir, "out.docx")

	code := cmdApply([]string{inPath, "-p", patchPath, "-o", outPath})
	if code != exitInput {
		t.Fatalf("모르는 필드가 있는 패치인데 exit=%d (기대 %d)", code, exitInput)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("거절된 패치가 출력 파일을 만들었다: %v", err)
	}
}

// TestTmplFillRejectsUnknownSchemaField 는 스키마 JSON 의 오타도 같은 이유로
// 거절하는지 본다. 스키마는 키마다 part·path 를 들고 있어, 필드 하나가 조용히
// 비면 채우기가 엉뚱한 곳을 가리키거나 아무 데도 안 간다.
func TestTmplFillRejectsUnknownSchemaField(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "t.docx")
	if err := os.WriteFile(tmplPath, testutil.MinimalDocx([]string{"자리"}), 0o644); err != nil {
		t.Fatalf("템플릿 쓰기 실패: %v", err)
	}
	schemaPath := filepath.Join(dir, "schema.json")
	bad := `{"base":"t.docx","keys":[{"key":"k1","paths":"document/body[1]/p[1]/r[1]/t[1]"}]}`
	if err := os.WriteFile(schemaPath, []byte(bad), 0o644); err != nil {
		t.Fatalf("스키마 쓰기 실패: %v", err)
	}
	dataPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(dataPath, []byte(`{"k1":"값"}`), 0o644); err != nil {
		t.Fatalf("데이터 쓰기 실패: %v", err)
	}
	outPath := filepath.Join(dir, "out.docx")

	var code int
	stdout := captureStdout(t, func() {
		code = cmdTmpl([]string{"fill", tmplPath, "--schema", schemaPath, "-d", dataPath, "-o", outPath})
	})
	if code != exitInput {
		t.Fatalf("모르는 필드가 있는 스키마인데 exit=%d (기대 %d)", code, exitInput)
	}
	// 종료 코드만으로는 부족하다 — 오타 난 스키마는 지금도 코드 1 로 끝나지만,
	// 그건 빈 path 를 들고 채우기까지 간 뒤 template_drift("템플릿에  경로가 없다")
	// 로 걸리기 때문이다. 원인이 오타라는 사실이 그 메시지 어디에도 없다.
	// 파싱 단계에서 끊기면 stdout 봉투 자체가 나오지 않는다.
	if stdout != "" {
		t.Fatalf("스키마 오타가 파싱을 통과해 채우기까지 갔다: %s", stdout)
	}
}

// TestRejectsTrailingJSON 은 첫 JSON 값 뒤에 내용이 더 있으면 거절하는지 본다.
//
// json.Unmarshal 은 뒤에 남은 내용을 오류로 봤지만 json.Decoder.Decode 는 첫
// 값만 읽고 멈춘다. DisallowUnknownFields 를 쓰려고 Decoder 로 바꾸면 이 검사가
// 조용히 사라져, 잘린 파일이나 두 번 이어붙인 파일이 **성공으로** 통과한다 —
// 모르는 필드를 막으려다 같은 부류의 구멍을 새로 여는 셈이다.
//
// `]` 를 따로 보는 이유: Decoder.More() 는 다음 바이트가 `]` 나 `}` 면 false 를
// 내므로 그것만으로는 이 경우를 못 잡는다. Token() 이 io.EOF 를 내는지로 본다.
func TestRejectsTrailingJSON(t *testing.T) {
	head := `{"ops":[{"op":"setText","path":"document/body[1]/p[1]/r[1]/t[1]","text":"값"}]}`
	for _, c := range []struct{ name, body string }{
		{"쓰레기", head + ` 이 뒤는 JSON 이 아니다`},
		{"닫는 괄호", head + `]`},
		{"값 두 개", head + head},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			inPath := filepath.Join(dir, "in.docx")
			if err := os.WriteFile(inPath, testutil.MinimalDocx([]string{"원래 텍스트"}), 0o644); err != nil {
				t.Fatalf("입력 파일 쓰기 실패: %v", err)
			}
			patchPath := filepath.Join(dir, "patch.json")
			if err := os.WriteFile(patchPath, []byte(c.body), 0o644); err != nil {
				t.Fatalf("패치 파일 쓰기 실패: %v", err)
			}
			outPath := filepath.Join(dir, "out.docx")

			var code int
			stdout := captureStdout(t, func() {
				code = cmdApply([]string{inPath, "-p", patchPath, "-o", outPath})
			})
			if code != exitInput {
				t.Fatalf("여분의 내용이 있는 패치인데 exit=%d (기대 %d), stdout=%s", code, exitInput, stdout)
			}
			if _, err := os.Stat(outPath); !os.IsNotExist(err) {
				t.Fatalf("거절된 패치가 출력 파일을 만들었다: %v", err)
			}
		})
	}
}

// TestDiffSelfIsEmptyAndExitsZero 는 차이가 없어도, 있어도 종료 코드가 0 인지 본다.
// 차이는 결과지 오류가 아니다 — 코드에 세 번째 뜻을 섞으면 에이전트의 재시도
// 판단이 흐려진다 (설계 §3).
func TestDiffSelfIsEmptyAndExitsZero(t *testing.T) {
	deck := filepath.Join("..", "..", "testdata", "real", "deck-a.pptx")
	var code int
	stdout := captureStdout(t, func() { code = cmdDiff([]string{deck, deck}) })
	if code != exitOK {
		t.Fatalf("exit=%d (기대 0), stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, `"total": 0`) {
		t.Fatalf("total 이 0 이 아니다: %s", stdout)
	}
}

func TestDiffWithDifferencesStillExitsZero(t *testing.T) {
	a := filepath.Join("..", "..", "testdata", "real", "deck-a.pptx")
	b := filepath.Join("..", "..", "testdata", "real", "deck-b.pptx")
	var code int
	stdout := captureStdout(t, func() { code = cmdDiff([]string{a, b}) })
	if code != exitOK {
		t.Fatalf("차이가 있는데 exit=%d (기대 0), stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, `"kind": "text"`) {
		t.Fatalf("text 항목이 없다: %s", stdout)
	}
}

// TestDiffFormatMismatchIsInputError 는 docx 와 pptx 비교가 코드 1 + stdout
// JSON 으로 보고되는지 본다.
func TestDiffFormatMismatchIsInputError(t *testing.T) {
	doc := filepath.Join("..", "..", "testdata", "real", "form-a.docx")
	deck := filepath.Join("..", "..", "testdata", "real", "deck-a.pptx")
	var code int
	stdout := captureStdout(t, func() { code = cmdDiff([]string{doc, deck}) })
	if code != exitInput {
		t.Fatalf("exit=%d (기대 %d), stdout=%s", code, exitInput, stdout)
	}
	if !strings.Contains(stdout, "format_mismatch") {
		t.Fatalf("stdout 에 format_mismatch 가 없다: %s", stdout)
	}
}

// TestDiffBadSelectorIsInputError 는 선택자 오타가 dump 와 같은 사유로
// 거절되는지 본다. 같은 질문에 두 명령이 다른 답을 내면 안 된다.
func TestDiffBadSelectorIsInputError(t *testing.T) {
	deck := filepath.Join("..", "..", "testdata", "real", "deck-a.pptx")
	var code int
	stdout := captureStdout(t, func() {
		code = cmdDiff([]string{deck, deck, "--part", "ppt/theme/*"})
	})
	if code != exitInput {
		t.Fatalf("exit=%d (기대 %d), stdout=%s", code, exitInput, stdout)
	}
	if !strings.Contains(stdout, "part_not_scannable") {
		t.Fatalf("stdout 에 part_not_scannable 이 없다: %s", stdout)
	}
}

// TestDiffNeedsTwoInputs 는 인자 수가 틀리면 사용법을 내는지 본다.
func TestDiffNeedsTwoInputs(t *testing.T) {
	deck := filepath.Join("..", "..", "testdata", "real", "deck-a.pptx")
	if code := cmdDiff([]string{deck}); code != exitInput {
		t.Fatalf("인자 하나인데 exit=%d (기대 %d)", code, exitInput)
	}
	if code := cmdDiff([]string{deck, deck, deck}); code != exitInput {
		t.Fatalf("인자 셋인데 exit=%d (기대 %d)", code, exitInput)
	}
}

// twoDocxWithInsertion 은 문단 하나가 삽입된 docx 쌍을 만들어 경로를 돌려준다.
//
// 삽입 구간 앞뒤에 완전히 같은 앵커("첫 줄"·"셋째 줄")가 있어 align.Siblings 가
// 'e'+'i' 만으로 깨끗이 푼다 — 문서 루트에 'r' 이 하나 나오긴 하지만 la=lb=1 이라
// 꼬리가 비어 자명하게 통과한다. align.Match 의 'r' 꼬리 처리(OnlyA·OnlyB 로
// 넘기는 두 루프) 는 이 쌍만으로는 **어느 쪽도** 돌지 않는다 — 그 두 방향은
// 아래 twoDocxWithUnanchoredMismatchBLonger(OnlyB)·
// twoDocxWithUnanchoredMismatchALonger(OnlyA)가 한 방향씩 나눠 잠근다.
func twoDocxWithInsertion(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.docx")
	b := filepath.Join(dir, "b.docx")
	if err := os.WriteFile(a, testutil.MinimalDocx([]string{"첫 줄", "셋째 줄"}), 0o644); err != nil {
		t.Fatalf("a 쓰기: %v", err)
	}
	if err := os.WriteFile(b, testutil.MinimalDocx([]string{"첫 줄", "새로 낀 줄", "셋째 줄"}), 0o644); err != nil {
		t.Fatalf("b 쓰기: %v", err)
	}
	return a, b
}

// twoDocxWithUnanchoredMismatchBLonger 는 앞뒤 앵커("첫 줄"·"끝 줄")는 같지만
// 그 사이가 b 쪽이 더 많은(a 1개, b 2개) docx 쌍을 만든다.
//
// align.Siblings 로 손으로 되짚으면: 앞 공통 p=1("첫 줄"), 뒤 공통 s=1("끝 줄").
// 가운데 a[1:2]=["중간A"], b[1:3]=["중간B1","중간B2"] 는 서로 sig 가 달라 LCS
// 매칭이 하나도 없고, alignMiddle 이 la=1·lb=2 인 단일 'r' 구간으로 낸다 —
// **꼬리가 있는 'r'** 이다. align.Match 에서 m=min(la,lb)=1 이라 "중간A"↔"중간B1"
// 은 위치로 짝지어지고, "중간B2" 는 BStart+m..BEnd 꼬리로 남는다 —
// **OnlyB 루프만** 돈다. la=1=m 이라 AStart+m..AEnd 는 비어 있어 OnlyA
// 루프는 반복 0회로 자명하게 통과할 뿐 실제로 검증하지 않는다 — 재리뷰가
// OnlyA 루프만 지우는 변이로 이 쌍 혼자서는 못 잡는다는 것을 실증했다. 그
// 거울상(OnlyA 를 도는 쪽)은 twoDocxWithUnanchoredMismatchALonger 가 잠근다.
func twoDocxWithUnanchoredMismatchBLonger(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.docx")
	b := filepath.Join(dir, "b.docx")
	if err := os.WriteFile(a, testutil.MinimalDocx([]string{"첫 줄", "중간A", "끝 줄"}), 0o644); err != nil {
		t.Fatalf("a 쓰기: %v", err)
	}
	if err := os.WriteFile(b, testutil.MinimalDocx([]string{"첫 줄", "중간B1", "중간B2", "끝 줄"}), 0o644); err != nil {
		t.Fatalf("b 쓰기: %v", err)
	}
	return a, b
}

// twoDocxWithUnanchoredMismatchALonger 는 twoDocxWithUnanchoredMismatchBLonger 의
// 거울상이다 — 앞뒤 앵커는 같지만 가운데는 a 쪽이 더 많다(a 2개, b 1개).
//
// align.Siblings 로 손으로 되짚으면: 앞 공통 p=1("첫 줄"), 뒤 공통 s=1("끝 줄").
// 가운데 a[1:3]=["중간A1","중간A2"], b[1:2]=["중간B1"] 는 서로 sig 가 달라 LCS
// 매칭이 하나도 없고, alignMiddle 이 la=2·lb=1 인 단일 'r' 구간으로 낸다.
// align.Match 에서 m=min(la,lb)=1 이라 "중간A1"↔"중간B1" 은 위치로 짝지어지고,
// "중간A2" 는 AStart+m..AEnd 꼬리로 남는다 — **OnlyA 루프만** 돈다. lb=1=m
// 이라 BStart+m..BEnd 는 비어 있어 OnlyB 루프는 반복 0회로 자명하게 통과할
// 뿐이다.
func twoDocxWithUnanchoredMismatchALonger(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.docx")
	b := filepath.Join(dir, "b.docx")
	if err := os.WriteFile(a, testutil.MinimalDocx([]string{"첫 줄", "중간A1", "중간A2", "끝 줄"}), 0o644); err != nil {
		t.Fatalf("a 쓰기: %v", err)
	}
	if err := os.WriteFile(b, testutil.MinimalDocx([]string{"첫 줄", "중간B1", "끝 줄"}), 0o644); err != nil {
		t.Fatalf("b 쓰기: %v", err)
	}
	return a, b
}

// TestTmplExtractRejectsUnrepresentedByDefault 는 플래그 없이 구조가 다른
// 문서를 주면 CLI 가 거절하고 출력 파일을 만들지 않는지 본다.
func TestTmplExtractRejectsUnrepresentedByDefault(t *testing.T) {
	a, b := twoDocxWithInsertion(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "t.docx")
	schema := filepath.Join(dir, "s.json")

	var code int
	stdout := captureStdout(t, func() {
		code = cmdTmpl([]string{"extract", a, b, "-o", out, "--schema", schema})
	})
	if code != exitInput {
		t.Fatalf("exit=%d (기대 %d), stdout=%s", code, exitInput, stdout)
	}
	if !strings.Contains(stdout, "unrepresented_structure") {
		t.Fatalf("stdout 에 unrepresented_structure 가 없다: %s", stdout)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("거절됐는데 템플릿 파일이 생겼다")
	}
	if _, err := os.Stat(schema); !os.IsNotExist(err) {
		t.Fatal("거절됐는데 스키마 파일이 생겼다")
	}
}

// TestTmplExtractAllowFlagWritesUnrepresented 는 플래그를 주면 진행하고
// 스키마에 unrepresented 가 실리는지 본다.
func TestTmplExtractAllowFlagWritesUnrepresented(t *testing.T) {
	a, b := twoDocxWithInsertion(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "t.docx")
	schema := filepath.Join(dir, "s.json")

	var code int
	stdout := captureStdout(t, func() {
		code = cmdTmpl([]string{"extract", a, b, "-o", out, "--schema", schema, "--allow-unrepresented"})
	})
	if code != exitOK {
		t.Fatalf("exit=%d (기대 0), stdout=%s", code, stdout)
	}
	raw, err := os.ReadFile(schema)
	if err != nil {
		t.Fatalf("스키마 읽기: %v", err)
	}
	if !strings.Contains(string(raw), "unrepresented") {
		t.Fatalf("스키마에 unrepresented 가 없다: %s", raw)
	}
}

// TestT5DiffAndTmplAgreeOnAlignment 는 같은 문서 쌍에서 diff 의
// inserted·deleted 경로 집합과 tmpl 의 unrepresented 경로 집합이 일치하는지 본다.
//
// 두 명령이 각자 재귀를 갖고 있어(diff 의 alignChildren, align.Match) 'r' 구간의
// 위치 짝짓기 규칙이 복제돼 있다. 갈라지는 날 같은 문서에 다른 답을 내는데,
// 그것이 바로 이 슬라이스가 없애려던 상태다(설계 T5).
//
// 세 케이스를 표로 돈다 — 각각이 align.Match 의 어느 분기를 실제로 지나는지가
// 다르다(가짜로 통과만 하는 반복 0회 루프는 검증이 아니다):
//
//   - twoDocxWithInsertion: 'r' 이 나오지만 la=lb=1 이라 OnlyA·OnlyB 어느
//     쪽도 안 돈다 — 앵커 있는 삽입만 잠근다.
//   - twoDocxWithUnanchoredMismatchBLonger: la=1 < lb=2, OnlyB 루프만 돈다.
//   - twoDocxWithUnanchoredMismatchALonger: la=2 > lb=1, OnlyA 루프만 돈다.
//
// 처음엔 두 번째 케이스만 있었는데, 재리뷰가 OnlyA 만 지우는 변이로 그
// 상태에서 T5 가 안 걸린다는 것을 실증했다(OnlyB 만 도니까) — 세 번째
// 케이스가 그 구멍을 메운다.
func TestT5DiffAndTmplAgreeOnAlignment(t *testing.T) {
	cases := []struct {
		name string
		pair func(t *testing.T) (string, string)
	}{
		{"앵커 있음_꼬리 없는 r", twoDocxWithInsertion},
		{"앵커 없음_꼬리 있는 r_B가 더 김_OnlyB", twoDocxWithUnanchoredMismatchBLonger},
		{"앵커 없음_꼬리 있는 r_A가 더 김_OnlyA", twoDocxWithUnanchoredMismatchALonger},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := c.pair(t)
			dir := t.TempDir()
			schema := filepath.Join(dir, "s.json")

			diffOut := captureStdout(t, func() {
				if code := cmdDiff([]string{a, b}); code != exitOK {
					t.Fatalf("diff exit=%d", code)
				}
			})
			if code := cmdTmpl([]string{"extract", a, b,
				"-o", filepath.Join(dir, "t.docx"), "--schema", schema, "--allow-unrepresented"}); code != exitOK {
				t.Fatalf("tmpl extract exit=%d", code)
			}

			var rep struct {
				Diffs []struct{ Kind, Part, Path string } `json:"diffs"`
			}
			if err := json.Unmarshal([]byte(diffOut), &rep); err != nil {
				t.Fatalf("diff 출력 파싱: %v", err)
			}
			fromDiff := map[string]bool{}
			for _, d := range rep.Diffs {
				if d.Kind == "inserted" || d.Kind == "deleted" {
					fromDiff[d.Part+"|"+d.Path] = true
				}
			}

			raw, err := os.ReadFile(schema)
			if err != nil {
				t.Fatalf("스키마 읽기: %v", err)
			}
			var sch struct {
				Unrepresented []struct{ Part, Path string } `json:"unrepresented"`
			}
			if err := json.Unmarshal(raw, &sch); err != nil {
				t.Fatalf("스키마 파싱: %v", err)
			}
			fromTmpl := map[string]bool{}
			for _, u := range sch.Unrepresented {
				fromTmpl[u.Part+"|"+u.Path] = true
			}

			if len(fromDiff) == 0 {
				t.Fatal("diff 가 inserted/deleted 를 하나도 안 냈다 — 이 쌍은 구조가 다르다")
			}
			if len(fromDiff) != len(fromTmpl) {
				t.Fatalf("경로 집합 크기가 다르다 — diff %d, tmpl %d\n  diff=%v\n  tmpl=%v",
					len(fromDiff), len(fromTmpl), fromDiff, fromTmpl)
			}
			for k := range fromDiff {
				if !fromTmpl[k] {
					t.Errorf("diff 에만 있는 경로: %s", k)
				}
			}
		})
	}
}
