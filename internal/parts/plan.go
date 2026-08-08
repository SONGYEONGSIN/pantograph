// Package parts 는 문서의 파트 지도를 만든다. 포맷을 아는 유일한 곳이다.
//
// dump·patch·tmpl 은 이 계획만 받고 포맷을 모른다. xlsx 가 들어올 때
// 손댈 곳도 여기 하나다.
package parts

import (
	"encoding/xml"
	"errors"
	"fmt"
	"path"
	"strconv"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
)

// 본문 파트의 ContentType
const (
	ctDocxMain  = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	ctPptxSlide = "application/vnd.openxmlformats-officedocument.presentationml.slide+xml"
)

// ErrUnsupportedFormat 은 알려진 본문 파트를 하나도 못 찾았을 때다.
// CLI 는 이것을 stdout JSON + 종료 코드 1 로 낸다 — 입력 파일의 성질이지 도구의 고장이 아니다.
var ErrUnsupportedFormat = errors.New("알려진 본문 파트를 찾지 못했다")

// Part 는 스캔 대상 파트 하나다.
type Part struct {
	Name string // "ppt/slides/slide1.xml" — 물리 파트 경로
	Ref  string // "pptx/slide[1]" — 논리 참조. 없으면 ""
	Root string // 경로의 루트 별칭 ("document" / "sld")
}

// Plan 은 문서의 본문 파트를 순서대로 돌려준다.
// 출력 순서는 결정론적이다 (I3) — pptx 는 sldIdLst 순서, docx 는 하나뿐이다.
func Plan(p *opc.Package) (string, []Part, error) {
	ct, err := readContentTypes(p)
	if err != nil {
		return "", nil, err
	}

	// docx: 본문 파트가 하나다. 컨테이너 순서대로 첫 번째를 잡는다.
	for _, name := range p.Names() {
		if ct.of(name) == ctDocxMain {
			return "docx", []Part{{Name: name, Ref: "docx/document", Root: "document"}}, nil
		}
	}

	// pptx: 슬라이드가 있으면 presentation.xml 이 정한 순서로 낸다.
	hasSlide := false
	for _, name := range p.Names() {
		if ct.of(name) == ctPptxSlide {
			hasSlide = true
			break
		}
	}
	if hasSlide {
		ordered, err := orderSlides(p, ct)
		if err != nil {
			return "", nil, err
		}
		return "pptx", ordered, nil
	}

	return "", nil, ErrUnsupportedFormat
}

// orderSlides 는 presentation.xml 의 sldIdLst 순서로 슬라이드 파트를 낸다.
// 파일명 순서를 쓰지 않는 이유: 파일명이 발표 순서와 일치한다는 보장이 없고,
// 어긋났을 때 알아낼 방법도 없다 (설계 §4).
func orderSlides(p *opc.Package, ct *contentTypes) ([]Part, error) {
	presRaw, err := p.Part("ppt/presentation.xml")
	if err != nil {
		return nil, fmt.Errorf("ppt/presentation.xml 없음: %w", err)
	}
	relsRaw, err := p.Part("ppt/_rels/presentation.xml.rels")
	if err != nil {
		return nil, fmt.Errorf("ppt/_rels/presentation.xml.rels 없음: %w", err)
	}

	var pres struct {
		SldIDs []struct {
			RID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sldIdLst>sldId"`
	}
	if err := xml.Unmarshal(presRaw, &pres); err != nil {
		return nil, fmt.Errorf("presentation.xml 파싱 실패: %w", err)
	}

	var rels struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(relsRaw, &rels); err != nil {
		return nil, fmt.Errorf("presentation.xml.rels 파싱 실패: %w", err)
	}
	target := make(map[string]string, len(rels.Rels))
	for _, r := range rels.Rels {
		target[r.ID] = r.Target
	}

	out := make([]Part, 0, len(pres.SldIDs))
	for i, s := range pres.SldIDs {
		tgt, ok := target[s.RID]
		if !ok {
			return nil, fmt.Errorf("sldId 의 관계 %s 를 rels 에서 못 찾았다", s.RID)
		}
		// Target 은 ppt/ 기준 상대 경로다 ("slides/slide1.xml").
		name := path.Join("ppt", tgt)
		if ct.of(name) != ctPptxSlide {
			return nil, fmt.Errorf("%s 는 슬라이드 ContentType 이 아니다", name)
		}
		out = append(out, Part{
			Name: name,
			Ref:  "pptx/slide[" + strconv.Itoa(i+1) + "]",
			Root: "sld",
		})
	}
	if len(out) == 0 {
		return nil, ErrUnsupportedFormat
	}
	return out, nil
}
