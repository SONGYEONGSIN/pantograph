// Package dump 는 docx 를 에이전트가 읽을 JSON 으로 내보낸다.
package dump

import (
	"encoding/json"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/wml"
)

// ScannedPart 는 이번 슬라이스가 파싱하는 유일한 파트다.
// 나머지 파트는 raw 로만 다루므로 노드가 없다.
const ScannedPart = "word/document.xml"

type Doc struct {
	Format      string   `json:"format"`
	Hash        string   `json:"hash"`
	Parts       []string `json:"parts"`
	ScannedPart string   `json:"scannedPart"`
}

type Dump struct {
	Doc   Doc        `json:"doc"`
	Nodes []wml.Node `json:"nodes"`
}

// Build 는 패키지를 덤프 구조로 바꾼다.
func Build(p *opc.Package) (*Dump, error) {
	content, err := p.Part(ScannedPart)
	if err != nil {
		return nil, err
	}
	tree, err := wml.Scan(content)
	if err != nil {
		return nil, err
	}
	return &Dump{
		Doc: Doc{
			Format:      "docx",
			Hash:        p.Hash,
			Parts:       p.Names(),
			ScannedPart: ScannedPart,
		},
		Nodes: tree.Nodes,
	}, nil
}

// Marshal 은 덤프를 JSON 으로 직렬화한다.
// 모든 필드가 슬라이스·문자열이라 맵 순회 순서가 개입하지 않는다 (I3).
func Marshal(d *Dump) ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}
