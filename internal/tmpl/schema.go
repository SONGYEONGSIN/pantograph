// Package tmpl 은 같은 양식 문서 N벌에서 {{key}} 템플릿을 역추출하고 되채운다.
//
// 별도 엔진이 아니다 — Extract 는 setText 패치를 만드는 기계이고
// Fill 은 스키마와 데이터로 setText 패치를 만들어 patch.Apply 에 넘긴다.
// 그래서 I1~I3 가 템플릿 층까지 자동으로 덮는다.
package tmpl

// Key 는 템플릿의 가변 자리 하나다.
type Key struct {
	Key     string   `json:"key"`
	Part    string   `json:"part"`
	Path    string   `json:"path"`
	Samples []string `json:"samples"`
}

// Schema 는 역추출 결과다.
type Schema struct {
	Base string `json:"base"`

	// Hash 는 베이스 문서의 hash 다. **정보 항목이지 잠금이 아니다** —
	// 어느 문서에서 이 템플릿이 나왔는지 기록할 뿐, 이 값을 대조하는 코드 경로는
	// 없다. Fill 은 사용자가 템플릿 파일을 따로 손댈 수 있다고 보고 hash 대조를
	// 하지 않으며, 대신 경로마다 자리표시자가 그대로인지 확인해
	// template_drift 로 거절한다.
	Hash string `json:"hash"`

	Keys []Key `json:"keys"`
}

// VolatileAttrs 는 문서마다 달라도 "같은 양식"으로 보는 속성의 로컬명이다.
// Word 가 문단·저장마다 새로 붙이는 식별자들이라 내용과 무관하다.
var VolatileAttrs = map[string]bool{
	"paraId": true,
	"textId": true,
}

// isVolatile 은 속성이 비교에서 제외되는지 판정한다.
// w:rsid* 계열(rsidR, rsidRDefault, rsidP, rsidRPr, rsidTr, rsidDel, rsidSect)은
// 접두사로 잡는다 — Word 버전마다 종류가 늘어난다.
func isVolatile(local string) bool {
	if VolatileAttrs[local] {
		return true
	}
	return len(local) >= 4 && local[:4] == "rsid"
}

// VolatileElements 는 diffMarkup 이 **속성만** 통째로 비교에서 빼는 요소의
// 로컬명이다 — 요소 전체가 휘발성인 것은 아니다. Type·직접 텍스트·자손은
// VolatileAttrs 와 마찬가지로 그대로 비교 대상에 남는다. VolatileAttrs 와
// 달리 속성 이름이 아니라 요소 이름으로 판정한다 — 이 요소들은 "생성할
// 때마다 새로 찍는 식별자"만 속성으로 담는 게 존재 이유라 속성 이름으로는
// 저격할 수 없다.
//
// 로컬명만으로 키를 잡는 이유: xmlscan.Attr 에는 NS(네임스페이스)가 있지만
// xmlscan.Node 에는 없다 — 요소의 네임스페이스를 스캐너가 안 담는다는
// 구조적 한계다. 나중에 이 저격을 네임스페이스까지 좁혀야 한다면 거기서부터
// 손대야 한다.
//
// creationId: PowerPoint 가 도형을 새로 만들 때마다(a16:creationId, 속성
// 이름은 id) · 슬라이드를 새로 만들 때마다(p14:creationId, 속성 이름은
// val) 찍는 GUID·숫자 식별자. xmlscan.Attr.Name 은 로컬명만 담으므로
// 네임스페이스(a16: vs p14:)와 무관하게 둘 다 Type=="creationId" 로
// 잡힌다. "id"·"val" 은 OOXML 전역에서 진짜 내용(색상 값 등)을 나르는
// 흔한 이름이라 VolatileAttrs 로 넣으면 그 내용까지 다 숨겨버린다 —
// 그래서 요소 단위로 통째로 뺀다. diffStructure 가 이미 두 문서의
// 노드 경로 순열이 같다는 걸 보장하므로, 이 요소가 한쪽에만 있고
// 없는 경우는 여기 오기 전에 structure_mismatch 로 걸린다.
var VolatileElements = map[string]bool{
	"creationId": true,
}
