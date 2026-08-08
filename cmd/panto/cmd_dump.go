package main

import (
	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
)

func cmdDump(args []string) int {
	var in string
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
			if in != "" {
				return die(exitInput, "입력 파일이 둘 이상이다: %s, %s", in, args[i])
			}
			in = args[i]
		}
	}
	if in == "" {
		return die(exitInput, "사용법: panto dump <in.docx|in.pptx> [--part <선택자>]")
	}

	p, code := openInput(in)
	if p == nil {
		return code
	}
	doc, err := parts.Open(p)
	if err != nil {
		return failFormat(in, err)
	}
	// Build 는 선택된 본문 파트를 푼다 — 미지원 압축 방식이면 여기서
	// UnsupportedError 가 난다. 열기 게이트를 통과한 뒤에도 나는 종류다.
	d, err := dump.Build(doc, sels)
	if err != nil {
		return fail(in, err)
	}
	if err := emit(d); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
