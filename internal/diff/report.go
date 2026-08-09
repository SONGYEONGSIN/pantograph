// Package diff 는 두 문서의 차이를 경로 단위로 센다.
//
// tmpl 의 비교 코드(diffPartSet·diffStructure·diffMarkup)와 이름은 비슷하지만
// 하는 일이 반대다. 그쪽은 **게이트**다 — 첫 차이에서 멈추고 차이를 에러로
// 부른다. 여기는 **측정**이다 — 끝까지 세고 차이를 데이터로 낸다.
// 공유하는 것은 "무엇이 휘발성인가"(parts.StableAttrs) 뿐이다.
package diff

// Diff 는 차이 하나다.
//
// Expected·Actual 이 포인터인 이유: 한쪽에만 있는 속성을 null 로 실어야 한다.
// omitempty 를 붙이지 않는 이유: 모든 항목이 같은 모양이어야 소비자가 kind 별로
// 분기하지 않고 읽을 수 있다. 값이 없는 kind(structure·part_content·
// part_missing)에서는 null 이 나오며, 그것이 "값이 없다"는 정직한 표현이다.
type Diff struct {
	Kind     string  `json:"kind"`
	Scope    string  `json:"scope,omitempty"` // "body" | "other". 파트 전체 항목은 비운다
	Part     string  `json:"part"`
	Path     string  `json:"path,omitempty"`
	Attr     string  `json:"attr,omitempty"`
	Expected *string `json:"expected"`
	Actual   *string `json:"actual"`
	Detail   string  `json:"detail,omitempty"`
}

// Summary 는 kind 별 개수다. 벤치마크 임계는 이 위에 세워진다.
type Summary struct {
	Text        int `json:"text"`
	Attr        int `json:"attr"`
	Elem        int `json:"elem"`
	Structure   int `json:"structure"`
	PartContent int `json:"part_content"`
	PartMissing int `json:"part_missing"`
	Total       int `json:"total"`

	// VolatileOnly 는 바이트는 다른데 항목이 하나도 안 나온 계획 밖 파트의 수다.
	// 이 값이 없으면 사용자가 unzip+diff 로 본 것과 여기 답이 어긋나 보이고,
	// 그 침묵을 설명할 방법이 없다. 12 라고 말해주면 "알고 무시했다"가 된다.
	VolatileOnly int `json:"volatile_only"`
}

// Report 는 비교 하나의 결과다.
type Report struct {
	Expected string  `json:"expected"`
	Actual   string  `json:"actual"`
	Summary  Summary `json:"summary"`
	Diffs    []Diff  `json:"diffs"`
}

// add 는 항목을 담고 요약을 함께 올린다.
// 두 곳에서 세면 어긋나므로 세는 곳은 여기 하나다.
func (r *Report) add(d Diff) {
	r.Diffs = append(r.Diffs, d)
	switch d.Kind {
	case "text":
		r.Summary.Text++
	case "attr":
		r.Summary.Attr++
	case "elem":
		r.Summary.Elem++
	case "structure":
		r.Summary.Structure++
	case "part_content":
		r.Summary.PartContent++
	case "part_missing":
		r.Summary.PartMissing++
	}
	r.Summary.Total++
}

// ptr 은 문자열의 주소를 낸다. 루프 변수의 주소를 그대로 쓰지 않기 위한 것이다.
func ptr(s string) *string { return &s }
