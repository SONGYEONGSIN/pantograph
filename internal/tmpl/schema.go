// Package tmpl 은 같은 양식 문서 N벌에서 {{key}} 템플릿을 역추출하고 되채운다.
//
// 별도 엔진이 아니다 — Extract 는 setText 패치를 만드는 기계이고
// Fill 은 스키마와 데이터로 setText 패치를 만들어 patch.Apply 에 넘긴다.
// 그래서 I1~I3 가 템플릿 층까지 자동으로 덮는다.
package tmpl

// Key 는 템플릿의 가변 자리 하나다.
type Key struct {
	Key     string   `json:"key"`
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
