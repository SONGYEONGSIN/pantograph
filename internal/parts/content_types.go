package parts

import (
	"encoding/xml"
	"fmt"
	"path"
	"strings"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
)

// contentTypes 는 [Content_Types].xml 이 선언한 파트별 ContentType 이다.
// Override 가 Default 를 이긴다 (OPC 규약).
type contentTypes struct {
	byExt  map[string]string // 확장자(소문자, 점 없음) → ContentType
	byPart map[string]string // 파트 경로(선행 / 없음) → ContentType
}

func readContentTypes(p *opc.Package) (*contentTypes, error) {
	raw, err := p.Part("[Content_Types].xml")
	if err != nil {
		return nil, fmt.Errorf("[Content_Types].xml 없음: %w", err)
	}
	var doc struct {
		Defaults []struct {
			Extension   string `xml:"Extension,attr"`
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Default"`
		Overrides []struct {
			PartName    string `xml:"PartName,attr"`
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Override"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("[Content_Types].xml 파싱 실패: %w", err)
	}

	ct := &contentTypes{byExt: map[string]string{}, byPart: map[string]string{}}
	for _, d := range doc.Defaults {
		ct.byExt[strings.ToLower(d.Extension)] = d.ContentType
	}
	for _, o := range doc.Overrides {
		// PartName 은 "/word/document.xml" 처럼 선행 / 를 갖는다. opc 의 이름에는 없다.
		ct.byPart[strings.TrimPrefix(o.PartName, "/")] = o.ContentType
	}
	return ct, nil
}

func (ct *contentTypes) of(partName string) string {
	if t, ok := ct.byPart[partName]; ok {
		return t
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(partName), "."))
	return ct.byExt[ext]
}
