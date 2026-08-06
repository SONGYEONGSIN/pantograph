package main

import (
	"encoding/json"
	"os"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
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

	p, err := opc.Open(in)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	pb, err := os.ReadFile(patchPath)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	var pt patch.Patch
	if err := json.Unmarshal(pb, &pt); err != nil {
		return die(exitInput, "패치 JSON 파싱 실패: %v", err)
	}

	errs, err := patch.Apply(p, pt)
	if err != nil {
		return die(exitInternal, "%v", err)
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
	if err := writeAtomic(out, p.Write); err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := emit(patch.Result{OK: true}); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
