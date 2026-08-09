package parts

import "github.com/SONGYEONGSIN/pantograph/internal/xmlscan"

// 무엇이 휘발성인가 — 즉 문서마다 달라도 "내용이 같다"로 볼 것인가.
//
// **이 파일이 유일한 정의처다.** 같은 판정을 두 곳에 두면 갈라지는 날이 오고,
// 그날 템플릿 가역성(I4a)과 diff 가 같은 문서에 서로 다른 답을 낸다.
//
// 형식 지식이라 parts 에 있다 — paraId·rsid* 는 Word, creationId·fld 는
// PowerPoint 다. multipart 설계가 "parts 가 형식 지식의 유일한 보관처"라고
// 선언했다.
//
// 저격 방식이 셋인 이유는 실제 마크업이 세 모양이기 때문이다:
//
//	VolatileAttrs    속성 이름만 보면 되는 것 (w14:paraId 는 어디 있든 휘발성)
//	VolatileElements 요소가 식별자만 담아 속성 이름으로는 저격 못 하는 것
//	VolatilePairs    같은 속성 이름이 요소에 따라 휘발성이기도 내용이기도 한 것

// VolatileAttrs 는 어느 요소에 붙든 휘발성인 속성의 로컬명이다.
// Word 가 문단·저장마다 새로 붙이는 식별자들이라 내용과 무관하다.
var VolatileAttrs = map[string]bool{
	"paraId": true,
	"textId": true,
}

// VolatileElements 는 **속성을 통째로** 비교에서 빼는 요소의 로컬명이다.
// Type·직접 텍스트·자손은 그대로 비교 대상에 남는다.
//
// creationId: PowerPoint 가 도형을 만들 때마다(a16:creationId, 속성 이름 id)·
// 슬라이드를 만들 때마다(p14:creationId, 속성 이름 val) 찍는 식별자. 이 요소는
// 그 식별자만 담는 게 존재 이유라 요소째 빼는 게 맞다. xmlscan.Attr.Name 은
// 로컬명만 담으므로 네임스페이스(a16: vs p14:)와 무관하게 둘 다 잡힌다.
var VolatileElements = map[string]bool{
	"creationId": true,
}

// VolatilePairs 는 (요소 로컬명, 속성 로컬명) 짝으로 저격하는 휘발성이다.
//
// 같은 속성 이름이 요소에 따라 휘발성이기도 하고 내용이기도 하다:
//
//	a:fld  의 id  → 날짜·슬라이드 번호 필드의 식별자 (휘발성)
//	p:cNvPr 의 id → 도형 정체성 (내용)
//	w:rsid  의 val → Word 개정 저장 ID (휘발성)
//	w:sz    의 val → 글자 크기 (내용)
//
// 그래서 VolatileAttrs 로도 VolatileElements 로도 표현할 수 없다. a:fld 를
// 요소째 빼면 type("datetimeFigureOut" vs "slidenum")이 함께 사라져 날짜
// 필드와 슬라이드 번호 필드를 구별하지 못한다.
//
// **이 목록은 관찰된 것이지 완전한 목록이 아니다.** 새 생산자·새 버전이 우리가
// 모르는 식별자를 쓰면 그것은 진짜 차이로 보고된다. 새 픽스처가 들어올 때마다
// 이 목록이 자란다.
var VolatilePairs = map[[2]string]bool{
	{"fld", "id"}:       true,
	{"rsid", "val"}:     true,
	{"rsidRoot", "val"}: true,
}

// IsVolatileAttr 은 요소 elem 에 붙은 속성 attr 이 비교에서 빠지는지 본다.
// 둘 다 로컬명이다.
//
// w:rsid* 계열(rsidR, rsidRDefault, rsidP, rsidRPr, rsidTr, rsidDel, rsidSect)은
// 접두사로 잡는다 — Word 버전마다 종류가 늘어난다.
func IsVolatileAttr(elem, attr string) bool {
	if VolatileElements[elem] {
		return true
	}
	if VolatileAttrs[attr] {
		return true
	}
	if VolatilePairs[[2]string{elem, attr}] {
		return true
	}
	return len(attr) >= 4 && attr[:4] == "rsid"
}

// StableAttrs 는 휘발성을 뺀 속성 목록이다. 원문 순서를 유지한다.
//
// 네임스페이스 선언(xmlns:w=…)은 **빼지 않는다** — xmlscan 이 그것을 일반
// 속성으로 담고, tmpl 의 게이트가 지금 그렇게 비교하고 있다. 그것까지 빼면
// 이 슬라이스 밖의 동작이 변한다. diff 는 자기 쪽에서 따로 거른다 (설계 §6).
func StableAttrs(n xmlscan.Node) []xmlscan.Attr {
	out := make([]xmlscan.Attr, 0, len(n.Attrs))
	for _, a := range n.Attrs {
		if IsVolatileAttr(n.Type, a.Name) {
			continue
		}
		out = append(out, a)
	}
	return out
}
