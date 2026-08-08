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
		return fail(in, err)
	}
	// 선택자 실패(오탈자 등)도 입력 오류다 — fail() 의 분류기가 *parts.SelectError
	// 를 알아보고 그 Reason(part_not_found / ref_not_found / part_not_scannable)을
	// 그대로 낸다. dump.Build 안에서도 같은 검사를 하지만, 여기서 먼저 걸러야
	// Build 가 낸 다른 오류와 뒤섞이지 않는다.
	// Select 는 순수 조회라 다시 불러도 무엇을 스캔하지 않는다 (지연 스캔 유지).
	if _, err := doc.Select(sels); err != nil {
		return fail(in, err)
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
