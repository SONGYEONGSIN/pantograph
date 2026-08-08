// Package dump 는 문서를 에이전트가 읽을 JSON 으로 내보낸다.
package dump

import (
	"encoding/json"

	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// ScannedPart 는 이 태스크 이전의 dump 가 가정하던 유일한 본문 파트다.
// dump 자신은 더 이상 쓰지 않는다 — 파트별로 스캔한다.
// patch·tmpl 이 아직 참조하므로 Task 7 이 마지막 사용처를 걷어낸 뒤 지운다.
//
// NOTE: 브리프는 이 상수와 "파트 하나 + 그 노드들" 묶음 타입이 둘 다
// ScannedPart 라는 이름을 갖는다고 적었지만, Go 는 같은 패키지 안에서
// const 와 type 이 식별자를 공유하는 것을 허용하지 않는다("ScannedPart
// redeclared in this block"). 상수는 patch·tmpl 이 그대로 참조하므로
// 이름을 바꿀 수 없어, 묶음 타입 쪽을 Part 로 바꿔 충돌을 피했다.
const ScannedPart = "word/document.xml"

type Doc struct {
	Format  string   `json:"format"`
	Hash    string   `json:"hash"`
	Parts   []string `json:"parts"`   // 컨테이너의 전 엔트리
	Scanned []string `json:"scanned"` // 그중 파싱한 것
}

// Part 는 파싱한 파트 하나와 그 노드들이다.
type Part struct {
	Part  string         `json:"part"`
	Ref   string         `json:"ref,omitempty"`
	Root  string         `json:"root"`
	Nodes []xmlscan.Node `json:"nodes"`
}

type Dump struct {
	Doc          Doc    `json:"doc"`
	ScannedParts []Part `json:"scannedParts"`
}

// Build 는 문서를 덤프 구조로 바꾼다.
// sels 가 비면 계획의 본문 파트를 전부 스캔한다.
func Build(d *parts.Document, sels []string) (*Dump, error) {
	selected, err := d.Select(sels)
	if err != nil {
		return nil, err
	}

	out := &Dump{
		Doc: Doc{
			Format:  d.Format(),
			Hash:    d.Hash(),
			Parts:   d.Names(),
			Scanned: make([]string, 0, len(selected)),
		},
		ScannedParts: make([]Part, 0, len(selected)),
	}
	for _, pt := range selected {
		tree, err := d.Tree(pt.Name)
		if err != nil {
			return nil, err
		}
		out.Doc.Scanned = append(out.Doc.Scanned, pt.Name)
		out.ScannedParts = append(out.ScannedParts, Part{
			Part:  pt.Name,
			Ref:   pt.Ref,
			Root:  pt.Root,
			Nodes: tree.Nodes,
		})
	}
	return out, nil
}

// Marshal 은 덤프를 JSON 으로 직렬화한다.
// 모든 필드가 슬라이스·문자열이라 맵 순회 순서가 개입하지 않는다 (I3).
func Marshal(d *Dump) ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}
