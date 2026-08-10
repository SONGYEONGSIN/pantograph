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
// 분기하지 않고 읽을 수 있다. 값이 없는 kind(structure·inserted·deleted·
// part_content·part_missing)에서는 null 이 나오며, 그것이 "값이 없다"는
// 정직한 표현이다.
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
	Text int `json:"text"`
	Attr int `json:"attr"`
	Elem int `json:"elem"`
	// Structure 는 **정렬을 포기하고 위치로 비교한** 경우의 수다. 세 조건
	// 중 하나에서 난다: (1) 형제가 상한(align.go 의 maxCells)을 넘을 때,
	// (2) 한쪽 파트가 노드 0개로 스캔될 때(compare.go — xmlscan.Scan 은
	// 시작 요소가 없는 XML 도 에러 없이 빈 트리로 받아준다), (3) 'r' 구간에
	// 매칭 앵커가 하나도 없어 위치로 짝지었는데 양쪽 길이까지 다를 때
	// (compare.go alignChildren 의 'r' 분기 — 삽입과 인접 편집이 겹치면
	// 생긴다). 실제 문서에서는 사실상 0 이다. 예전에는 "구조가 갈려 그
	// 파트를 포기했다"는 뜻이었으나 LCS 정렬이 들어오면서 형제 수준에서는
	// 포기하는 일이 없어졌다.
	Structure int `json:"structure"`
	// Inserted·Deleted 는 **서브트리당 1건**이다. 문단 하나는 노드 3개
	// (w:p·w:r·w:t)이고 실제 문서에서는 10~20개다 — 노드마다 세면 문단 하나에
	// 수십 건이 된다. 항목의 detail 이 노드 수를 알린다.
	Inserted    int `json:"inserted"`
	Deleted     int `json:"deleted"`
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
	case "inserted":
		r.Summary.Inserted++
	case "deleted":
		r.Summary.Deleted++
	case "part_content":
		r.Summary.PartContent++
	case "part_missing":
		r.Summary.PartMissing++
	}
	r.Summary.Total++
}

// ptr 은 문자열의 주소를 낸다. 루프 변수의 주소를 그대로 쓰지 않기 위한 것이다.
func ptr(s string) *string { return &s }
