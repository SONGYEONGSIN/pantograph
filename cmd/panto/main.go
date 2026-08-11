// panto 는 docx·pptx 를 경로 단위로 덤프·패치하는 CLI 다.
// 모든 출력은 stdout JSON, 모든 진단은 stderr 다.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/SONGYEONGSIN/pantograph/internal/diff"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
)

// 종료 코드 (spec §9)
//
// **손상된 문서는 코드 2 가 아니라 코드 1 이다.** 컨테이너가 재현되지 않는 것도
// (unsupported_container), 본문 파트가 읽히지 않는 것도(unsupported_format)
// 그 파일의 성질이지 이 도구의 고장이 아니다 — 같은 파일로 다시 실행해도 같은
// 결과가 나온다. 코드 2 는 "이 도구가 제 일을 못 했다"는 신호이므로, 손상 파일을
// 여기로 보내면 에이전트가 고칠 수 없는 것을 재시도하거나 사람을 잘못 부른다.
const (
	exitOK       = 0 // 성공
	exitInput    = 1 // 입력 오류 — 경로 미해석 / hash 불일치 / 겹침 / 거절 / 구조 불일치
	exitInternal = 2 // 내부 오류 — 파일 I/O / 분류되지 않은 오류 / 적용 후 재스캔 실패
)

// usage 는 하위 명령의 사용법 문구와 같은 표면을 말해야 한다 —
// 여기만 docx 뿐이라고 하면 pptx 를 받는 바이너리가 자기 자신을 잘못 소개한다.
func usage() {
	fmt.Fprint(os.Stderr, `panto — docx·pptx 재현 하네스

사용법:
  panto dump  <in.docx|in.pptx> [--part <선택자>]
  panto diff  <expected> <actual> [--part <선택자>]
  panto apply <in.docx|in.pptx> -p <patch.json> -o <out>
  panto tmpl extract <a> <b> [...] -o <tmpl> --schema <schema.json> [--allow-unrepresented]
  panto tmpl fill    <tmpl> --schema <schema.json> -d <data.json> -o <out>

선택자(--part):
  ppt/slides/*        물리 경로 glob
  pptx/slide[3]       논리 참조 — 정확 일치
  여러 번 줄 수 있고 합집합이다. 생략하면 본문 파트를 전부 스캔한다.
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

// decodeStrict 는 JSON 파일을 읽되 **모르는 필드와 뒤에 남은 내용을 모두 거절한다.**
//
// encoding/json 의 두 기본 동작이 각각 조용히 위험하다.
//
//	Unmarshal — 모르는 필드를 버린다. "text" 를 "value" 로 잘못 쓴 setText 가
//	            빈 문자열이 되어 {"ok": true} 와 함께 그 자리의 텍스트를 지웠다.
//	Decoder   — 첫 값만 읽고 뒤를 안 본다. 잘리거나 두 번 이어붙은 파일이 통과한다.
//
// 둘 다 성공을 보고하면서 사용자가 쓴 것과 다른 일을 한다. 하나를 고치려고
// 다른 하나로 갈아타면 구멍이 옮겨갈 뿐이라, 두 검사를 함께 여기 한 곳에 둔다.
//
// 뒤에 남은 내용은 Token() 이 io.EOF 를 내는지로 본다 — Decoder.More() 는 다음
// 바이트가 `]` 나 `}` 면 false 를 내므로 `{...}]` 같은 꼬리를 놓친다.
func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("JSON 값 뒤에 내용이 더 있다")
	}
	return nil
}

// classify 는 오류가 stdout JSON 으로 보고할 **입력 부류**인지 가르고 그 reason 을 낸다.
//
//	*opc.UnsupportedError       → unsupported_container  (컨테이너를 바이트 동일하게 재현할 수 없다)
//	diff.ErrFormatMismatch      → format_mismatch        (docx 와 pptx 를 비교할 수 없다)
//	parts.ErrUnsupportedFormat  → unsupported_format     (Plan 의 모든 실패가 이것을 감싼다)
//	*parts.SelectError          → 그 Reason              (part_not_found | ref_not_found | part_not_scannable)
//	그 외                        → 보고 불가 → 내부 오류(코드 2)
//
// **분류는 여기 한 번만 한다.** dump·apply·tmpl 이 이 표 하나를 공유하지 않으면
// 같은 파일이 명령마다 다른 코드로 나간다 — 실제로 `tmpl extract a.xlsx b.xlsx`
// 는 코드 2, 같은 파일의 `dump` 는 코드 1 이었다. 종료 코드로 재시도 여부를
// 가르는 에이전트는 2 를 "도구가 고장났으니 에스컬레이션", 1 을 "입력이 틀렸으니
// 재시도 말 것"으로 읽는다 (spec §4·§8·§9).
//
// UnsupportedError 를 먼저 본다 — Plan 이 파트를 푸는 도중 만난 미지원 압축
// 방식은 두 부류에 다 걸리는데, 그때 정확한 진단은 컨테이너 쪽이다.
//
// **열기 시점 게이트만의 이야기가 아니다.** opc 는 열린 뒤에도 UnsupportedError 를
// 낸다 — 미지원 압축 방식의 파트를 풀 때(Part), 재조립 결과가 32비트 오프셋을
// 넘을 때(Write). 그 경로들도 전부 여기를 지나야 계약이 한 곳에서 지켜진다.
func classify(err error) (reason, detail string, ok bool) {
	var ue *opc.UnsupportedError
	if errors.As(err, &ue) {
		return "unsupported_container", ue.Detail, true
	}
	var se *parts.SelectError
	if errors.As(err, &se) {
		return se.Reason, se.Detail, true
	}
	if errors.Is(err, diff.ErrFormatMismatch) {
		return "format_mismatch", err.Error(), true
	}
	if errors.Is(err, parts.ErrUnsupportedFormat) {
		return "unsupported_format", err.Error(), true
	}
	return "", "", false
}

// fail 은 오류 하나를 종료 코드로 바꾼다.
//
// 입력 부류(classify 가 알아보는 것)는 **입력 오류**(코드 1)로 stdout JSON 에
// 보고한다. 내부 결함이 아니라 이 도구가 다룰 수 없는 입력의 성질이므로, 코드
// 2(내부 오류)로 보내면 에이전트가 "재시도해도 소용없다"가 아니라 "도구가
// 고장났다"로 잘못 읽는다 (spec §9).
//
// 그 밖의 오류는 내부 오류(코드 2)로 stderr 에 보고한다.
func fail(path string, err error) int {
	reason, detail, ok := classify(err)
	if !ok {
		return die(exitInternal, "%v", err)
	}
	if e := emit(patch.Result{OK: false, Errors: []patch.Error{
		{Path: path, Reason: reason, Detail: detail},
	}}); e != nil {
		return die(exitInternal, "%v", e)
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
	case "diff":
		os.Exit(cmdDiff(os.Args[2:]))
	case "apply":
		os.Exit(cmdApply(os.Args[2:]))
	case "tmpl":
		os.Exit(cmdTmpl(os.Args[2:]))
	default:
		usage()
		os.Exit(exitInput)
	}
}
