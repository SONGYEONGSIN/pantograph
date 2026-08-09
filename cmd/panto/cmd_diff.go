package main

import (
	"path/filepath"

	"github.com/SONGYEONGSIN/pantograph/internal/diff"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
)

// cmdDiff 는 두 문서의 차이를 경로 단위로 센다.
//
// 인자 순서가 의미를 갖는다 — 첫째가 기대, 둘째가 실제다. 출력의 expected·
// actual 이 그 어휘를 그대로 쓴다.
func cmdDiff(args []string) int {
	var ins []string
	var sels []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--part":
			i++
			if i >= len(args) {
				return die(exitInput, "--part 뒤에 선택자가 필요하다")
			}
			sels = append(sels, args[i])
		default:
			ins = append(ins, args[i])
		}
	}
	if len(ins) != 2 {
		return die(exitInput, "사용법: panto diff <expected> <actual> [--part <선택자>]")
	}

	ep, code := openInput(ins[0])
	if ep == nil {
		return code
	}
	ap, code := openInput(ins[1])
	if ap == nil {
		return code
	}
	ed, err := parts.Open(ep)
	if err != nil {
		return fail(ins[0], err)
	}
	ad, err := parts.Open(ap)
	if err != nil {
		return fail(ins[1], err)
	}

	rep, err := diff.Compare(ed, ad, filepath.Base(ins[0]), filepath.Base(ins[1]), sels)
	if err != nil {
		return fail(ins[0], err)
	}
	if err := emit(rep); err != nil {
		return die(exitInternal, "%v", err)
	}
	// 차이가 있어도 0 이다. 차이는 결과지 오류가 아니다 (설계 §3).
	return exitOK
}
