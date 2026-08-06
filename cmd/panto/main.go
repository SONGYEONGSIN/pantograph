// panto 는 docx 를 경로 단위로 덤프·패치하는 CLI 다.
// 모든 출력은 stdout JSON, 모든 진단은 stderr 다.
package main

import (
	"encoding/json"
	"fmt"
	"os"
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
  panto tmpl fill    <tmpl.docx> -d <data.json> -o <out.docx>
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

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitInput)
	}
	switch os.Args[1] {
	case "dump":
		os.Exit(cmdDump(os.Args[2:]))
	default:
		usage()
		os.Exit(exitInput)
	}
}
