// Package patch 는 경로 지정 패치를 바이트 스플라이스로 바꿔 적용한다.
//
// 계약: 전부 적용되거나 아무것도 적용되지 않는다. 부분 적용은 에이전트가
// 문서 상태를 잃는 최악의 실패다 (spec §9).
package patch

// Op 는 패치 연산 하나다. 연산은 setText·replaceRaw·delete 셋이다.
// Part 는 물리 파트 경로("ppt/slides/slide1.xml") 또는 논리 참조("pptx/slide[1]")다.
// 비어 있으면 본문 파트가 하나인 문서에 한해 그것으로 간주한다 — docx 하위호환.
//
// Text 와 XML 이 포인터인 이유: **"필드가 없다"(nil)와 "빈 값을 준다"("")를
// 구분해야 한다.** 값 타입이면 둘이 같은 제로값이 되어, text 를 빠뜨린 setText
// 가 조용히 텍스트를 지우고 ok:true 를 낸다 (설계 §1). 빈 문자열 자체는
// 정당한 값이므로 "빈 값 금지"로는 풀 수 없고, 존재 여부를 표현해야 한다.
type Op struct {
	Op   string  `json:"op"`
	Part string  `json:"part,omitempty"`
	Path string  `json:"path"`
	Text *string `json:"text,omitempty"` // setText 전용
	XML  *string `json:"xml,omitempty"`  // replaceRaw 전용
}

// Str 은 문자열 포인터를 만든다. Go 는 문자열 리터럴의 주소를 못 얻으므로
// Op 를 코드에서 조립할 때 필요하다.
func Str(v string) *string { return &v }

// Patch 는 배치 하나다. Hash 는 낙관적 잠금이며 비어있으면 대조를 건너뛴다.
type Patch struct {
	Hash string `json:"hash,omitempty"`
	Ops  []Op   `json:"ops"`
}

// Error 는 거절 사유다. 항상 경로를 단다 (spec §9).
type Error struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// Result 는 CLI 출력 봉투다.
type Result struct {
	OK     bool    `json:"ok"`
	Errors []Error `json:"errors,omitempty"`
}
