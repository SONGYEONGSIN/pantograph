package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
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
