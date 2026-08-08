// Package dump 는 문서를 에이전트가 읽을 JSON 으로 내보낸다.
package dump

import (
	"encoding/json"

	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

type Doc struct {
	Format  string   `json:"format"`
	Hash    string   `json:"hash"`
	Parts   []string `json:"parts"`   // 컨테이너의 전 엔트리
	Scanned []string `json:"scanned"` // 그중 파싱한 것
}

// ScannedPart 는 파싱한 파트 하나와 그 노드들이다.
// tmpl 이 파트 인식으로 바뀌면서(Task 7) 이 패키지의 마지막 단일 본문 파트
// 가정("word/document.xml" 고정)이 없어져, 그 이름을 쓰던 상수가 비고
// 이 타입이 그 이름을 이어받았다 — JSON 필드 scannedParts 와 짝을 맞추고,
// 계획 항목(파트 하나, 노드 없음)인 parts.Part 와 구분하기 위해서다.
type ScannedPart struct {
	Part  string         `json:"part"`
	Ref   string         `json:"ref,omitempty"`
	Root  string         `json:"root"`
	Nodes []xmlscan.Node `json:"nodes"`
}

type Dump struct {
	Doc          Doc           `json:"doc"`
	ScannedParts []ScannedPart `json:"scannedParts"`
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
		ScannedParts: make([]ScannedPart, 0, len(selected)),
	}
	for _, pt := range selected {
		tree, err := d.Tree(pt.Name)
		if err != nil {
			return nil, err
		}
		out.Doc.Scanned = append(out.Doc.Scanned, pt.Name)
		out.ScannedParts = append(out.ScannedParts, ScannedPart{
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
