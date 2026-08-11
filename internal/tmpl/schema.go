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

	// Unrepresented 는 이 템플릿이 표현하지 못하는 서브트리들이다.
	// **비어 있으면 필드 자체가 나오지 않는다** — 구조가 같은 문서에서 뽑은
	// 스키마는 이 슬라이스 이전과 바이트 동일해야 한다(설계 T1).
	Unrepresented []Unrepresented `json:"unrepresented,omitempty"`
}

// Unrepresented 는 템플릿이 표현하지 못하는 서브트리 하나다.
//
// 템플릿은 base 의 바이트에 setText 를 얹은 것이고 patch 의 연산은 setText 와
// replaceRaw 둘뿐이라, "이 문단은 어떤 문서에는 있고 어떤 문서에는 없다" 를
// 담을 수단이 없다(설계 §2). 그래서 그런 서브트리는 키 후보에서 빼고 여기
// 남긴다 — **도구가 자기가 못 하는 것을 스스로 말한다.**
type Unrepresented struct {
	Doc  string `json:"doc"`  // 어느 문서와 비교하다 나왔나
	Part string `json:"part"` // 물리 파트 경로
	Path string `json:"path"` // 그 서브트리의 루트 경로

	// Side 는 어느 쪽에만 있는지다. base 에만 있으면 템플릿에 남아 그 문서를
	// 재현할 때 **지워지지 않고**, 다른 문서에만 있으면 템플릿에 자리가 **없다**.
	// 두 실패 모양이 다르므로 구분해서 싣는다.
	Side string `json:"side"`

	Nodes int `json:"nodes"` // 서브트리의 노드 수 — 버려지는 무게
}
