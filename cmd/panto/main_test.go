package main

import (
	"bytes"
	"encoding/binary"
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
