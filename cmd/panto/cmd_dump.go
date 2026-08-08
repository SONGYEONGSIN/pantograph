package main

import (
	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
)

func cmdDump(args []string) int {
	if len(args) != 1 {
		return die(exitInput, "사용법: panto dump <in.docx>")
	}
	p, code := openInput(args[0])
	if code != exitOK {
		return code
	}
	doc, err := parts.Open(p)
	if err != nil {
		return fail(args[0], err)
	}
	// Build 는 계획의 본문 파트를 전부 푼다 — 미지원 압축 방식이면 여기서
	// UnsupportedError 가 난다. 열기 게이트를 통과한 뒤에도 나는 종류다.
	// 선택자(--part)는 Task 6 에서 더한다 — 지금은 항상 전체를 스캔한다.
	d, err := dump.Build(doc, nil)
	if err != nil {
		return fail(args[0], err)
	}
	if err := emit(d); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
