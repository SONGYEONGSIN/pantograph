package main

import (
	"os"

	"github.com/SONGYEONGSIN/pantograph/internal/patch"
)

func cmdApply(args []string) int {
	var in, patchPath, out string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p":
			i++
			if i >= len(args) {
				return die(exitInput, "-p 뒤에 패치 파일 경로가 필요하다")
			}
			patchPath = args[i]
		case "-o":
			i++
			if i >= len(args) {
				return die(exitInput, "-o 뒤에 출력 경로가 필요하다")
			}
			out = args[i]
		default:
			if in != "" {
				return die(exitInput, "입력 파일이 둘 이상이다: %s, %s", in, args[i])
			}
			in = args[i]
		}
	}
	if in == "" || patchPath == "" || out == "" {
		return die(exitInput, "사용법: panto apply <in.docx> -p <patch.json> -o <out.docx>")
	}

	p, code := openInput(in)
	if code != exitOK {
		return code
	}
	pb, err := os.ReadFile(patchPath)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	var pt patch.Patch
	if err := decodeStrict(pb, &pt); err != nil {
		return die(exitInput, "패치 JSON 파싱 실패: %v", err)
	}

	// Apply 는 op 이 지목한 파트만 지연 스캔한다 (Task 6) — 빈 패치는 아무
	// 파트도 스캔하지 않는다. 스캔된 파트가 미지원 압축 방식이면 여기서
	// UnsupportedError 가 난다.
	errs, err := patch.Apply(p, pt)
	if err != nil {
		return fail(in, err)
	}
	if len(errs) > 0 {
		if err := emit(patch.Result{OK: false, Errors: errs}); err != nil {
			return die(exitInternal, "%v", err)
		}
		return exitInput
	}

	// 출력 파일은 적용이 성공했을 때만 만든다 — 원자성 (spec §9).
	// writeAtomic 이 임시 파일에 쓰고 rename 하므로, 쓰기 도중 실패해도
	// out 경로에 잘린 파일이 남지 않는다.
	// p.Write 도 UnsupportedError 를 낼 수 있다 (재압축 대상의 미지원 압축 방식,
	// 32비트 오프셋 초과). I/O 실패는 그대로 내부 오류로 남는다.
	if err := writeAtomic(out, p.Write); err != nil {
		return fail(in, err)
	}
	if err := emit(patch.Result{OK: true}); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
