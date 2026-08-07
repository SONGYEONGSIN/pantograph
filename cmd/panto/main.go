// panto 는 docx 를 경로 단위로 덤프·패치하는 CLI 다.
// 모든 출력은 stdout JSON, 모든 진단은 stderr 다.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
)

// 종료 코드 (spec §9)
const (
	exitOK       = 0 // 성공
	exitInput    = 1 // 입력 오류 — 경로 미해석 / hash 불일치 / 겹침 / 거절 / 구조 불일치
	exitInternal = 2 // 내부 오류 — 파일 손상 / I/O / 적용 후 재스캔 실패
)

func usage() {
	fmt.Fprint(os.Stderr, `panto — docx 재현 하네스

사용법:
  panto dump  <in.docx>
  panto apply <in.docx> -p <patch.json> -o <out.docx>
  panto tmpl extract <a.docx> <b.docx> [...] -o <tmpl.docx> --schema <schema.json>
  panto tmpl fill    <tmpl.docx> --schema <schema.json> -d <data.json> -o <out.docx>
`)
}

// die 는 진단을 stderr 에 쓰고 종료 코드를 돌려준다.
func die(code int, format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "panto: "+format+"\n", args...)
	return code
}

// emit 은 값을 stdout 에 JSON 으로 쓴다.
func emit(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(b, '\n'))
	return err
}

// fail 은 오류 하나를 종료 코드로 바꾼다.
//
// 컨테이너를 바이트 동일하게 재현할 수 없는 파일은 **입력 오류**(코드 1)로
// stdout JSON 에 보고한다. 내부 결함이 아니라 이 도구가 다룰 수 없는 입력의
// 성질이므로, 코드 2(내부 오류)로 보내면 에이전트가 "재시도해도 소용없다"가
// 아니라 "도구가 고장났다"로 잘못 읽는다 (spec §9).
//
// **열기 시점 게이트만의 이야기가 아니다.** opc 는 열린 뒤에도 UnsupportedError 를
// 낸다 — 미지원 압축 방식의 파트를 풀 때(Part), 재조립 결과가 32비트 오프셋을
// 넘을 때(Write). 그 경로들도 전부 여기를 지나야 계약이 한 곳에서 지켜진다.
//
// UnsupportedError 가 아니면 내부 오류(코드 2)로 stderr 에 보고한다.
func fail(path string, err error) int {
	var ue *opc.UnsupportedError
	if !errors.As(err, &ue) {
		return die(exitInternal, "%v", err)
	}
	if err := emit(patch.Result{OK: false, Errors: []patch.Error{{
		Path:   path,
		Reason: "unsupported_container",
		Detail: ue.Detail,
	}}}); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitInput
}

// openInput 은 docx 를 연다.
// 두 번째 반환값은 종료 코드다. exitOK 면 패키지가 유효하다.
func openInput(path string) (*opc.Package, int) {
	p, err := opc.Open(path)
	if err == nil {
		return p, exitOK
	}
	return nil, fail(path, err)
}

// writeAtomic 은 같은 디렉토리의 임시 파일에 쓴 뒤 rename 으로 교체한다.
// 쓰기가 도중에 실패해도 대상 경로에 잘린 파일이 남지 않는다.
func writeAtomic(path string, write func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".panto-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if err := write(tmp); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	// rename 은 동시 독자에 대해서만 원자적이다. 크래시에 대해서는 아니라서,
	// 데이터가 디스크에 닿기 전에 rename 이 먼저 반영되면 대상 경로에
	// 길이 0 인 파일이 살아남을 수 있다.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	// os.CreateTemp 은 0600 으로 만든다. 그대로 두면 산출물이 소유자 전용이 되어
	// os.Create(0666 & ^umask) 로 만들었을 때와 권한이 달라진다.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitInput)
	}
	switch os.Args[1] {
	case "dump":
		os.Exit(cmdDump(os.Args[2:]))
	case "apply":
		os.Exit(cmdApply(os.Args[2:]))
	case "tmpl":
		os.Exit(cmdTmpl(os.Args[2:]))
	default:
		usage()
		os.Exit(exitInput)
	}
}
