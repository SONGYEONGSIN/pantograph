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
	d, err := dump.Build(p)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := emit(d); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
