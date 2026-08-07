package main

import (
	"github.com/SONGYEONGSIN/pantograph/internal/dump"
)

func cmdDump(args []string) int {
	if len(args) != 1 {
		return die(exitInput, "사용법: panto dump <in.docx>")
	}
	p, code := openInput(args[0])
	if code != exitOK {
		return code
	}
	// Build 는 word/document.xml 을 푼다 — 미지원 압축 방식이면 여기서
	// UnsupportedError 가 난다. 열기 게이트를 통과한 뒤에도 나는 종류다.
	d, err := dump.Build(p)
	if err != nil {
		return fail(args[0], err)
	}
	if err := emit(d); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
