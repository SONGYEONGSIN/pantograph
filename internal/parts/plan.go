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

// ErrUnsupportedFormat 은 **Plan 의 모든 실패**가 감싸는 센티널이다 — 알려진 본문
// 파트가 없는 것부터 presentation.xml 이 안 읽히는 것, sldId 의 rId 가 rels 에
// 없는 것까지 한 부류다.
// CLI 는 이것을 stdout JSON + 종료 코드 1 로 낸다 — 입력 파일의 성질이지 도구의 고장이 아니다.
//
// 부류가 하나여야 세 명령(dump·apply·tmpl)이 같은 파일에 같은 코드를 낸다.
// 묶지 않으면 같은 오류가 dump 에서는 코드 1(errors.Is 로 걸러짐), apply·tmpl
// 에서는 코드 2 로 나가고, 종료 코드로 재시도를 가르는 에이전트가 "입력이 틀렸다"를
// "도구가 고장났다"로 읽는다 (spec §4·§8: unsupported_format 은 코드 1).
//
// 감쌀 때는 원인 오류의 체인을 %w 로 보존한다 — 파트를 푸는 도중 난
// *opc.UnsupportedError 는 CLI 가 여전히 unsupported_container 로 가려내야 한다.
var ErrUnsupportedFormat = errors.New("지원하지 않는 포맷")

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
		return "", nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
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

	return "", nil, fmt.Errorf("%w: 알려진 본문 파트를 찾지 못했다", ErrUnsupportedFormat)
}

// orderSlides 는 presentation.xml 의 sldIdLst 순서로 슬라이드 파트를 낸다.
// 파일명 순서를 쓰지 않는 이유: 파일명이 발표 순서와 일치한다는 보장이 없고,
// 어긋났을 때 알아낼 방법도 없다 (설계 §4).
func orderSlides(p *opc.Package, ct *contentTypes) ([]Part, error) {
	presRaw, err := p.Part("ppt/presentation.xml")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
	}
	relsRaw, err := p.Part("ppt/_rels/presentation.xml.rels")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
	}

	var pres struct {
		SldIDs []struct {
			RID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sldIdLst>sldId"`
	}
	if err := xml.Unmarshal(presRaw, &pres); err != nil {
		return nil, fmt.Errorf("%w: presentation.xml 파싱 실패: %w", ErrUnsupportedFormat, err)
	}

	var rels struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(relsRaw, &rels); err != nil {
		return nil, fmt.Errorf("%w: presentation.xml.rels 파싱 실패: %w", ErrUnsupportedFormat, err)
	}
	target := make(map[string]string, len(rels.Rels))
	for _, r := range rels.Rels {
		target[r.ID] = r.Target
	}

	// 여기가 신뢰 경계다. presentation.xml·rels·[Content_Types].xml 은 셋 다
	// 입력 파일이 정하는 것이라, "슬라이드 ContentType 으로 선언됐다"는 그 파트가
	// 실제로 있다는 근거도, 한 번만 나온다는 근거도 못 된다. 계획은 sldId 하나당
	// 항목 하나를 만들므로, 검사 없이 통과시키면 이 함수가 입력 XML 을 작업량
	// 배수로 바꾼다 — 같은 슬라이드를 N 번 가리키는 2KB presentation.xml 하나가
	// 수백 MB 의 덤프가 된다(dump.Build 가 같은 노드 슬라이스를 N 번 담고
	// Marshal 이 N 번 직렬화한다). 둘 다 거절이므로 폴백 금지 규칙과 맞는다.
	inZip := make(map[string]bool, len(p.Names()))
	for _, n := range p.Names() {
		inZip[n] = true
	}

	out := make([]Part, 0, len(pres.SldIDs))
	seen := make(map[string]bool, len(pres.SldIDs))
	for i, s := range pres.SldIDs {
		tgt, ok := target[s.RID]
		if !ok {
			return nil, fmt.Errorf("%w: sldId 의 관계 %s 를 rels 에서 못 찾았다", ErrUnsupportedFormat, s.RID)
		}
		// Target 은 ppt/ 기준 상대 경로다 ("slides/slide1.xml").
		name := path.Join("ppt", tgt)
		if !inZip[name] {
			return nil, fmt.Errorf("%w: sldId 가 가리키는 %s 가 컨테이너에 없다", ErrUnsupportedFormat, name)
		}
		if ct.of(name) != ctPptxSlide {
			return nil, fmt.Errorf("%w: %s 는 슬라이드 ContentType 이 아니다", ErrUnsupportedFormat, name)
		}
		// 이름이 겹치면 계획에 같은 파트가 두 번 들어간다. rId 를 두 번 쓰거나
		// 서로 다른 두 rId 가 같은 Target 을 가리키면 그렇게 된다.
		if seen[name] {
			return nil, fmt.Errorf("%w: %s 를 sldIdLst 가 두 번 이상 가리킨다", ErrUnsupportedFormat, name)
		}
		seen[name] = true
		out = append(out, Part{
			Name: name,
			Ref:  "pptx/slide[" + strconv.Itoa(i+1) + "]",
			Root: "sld",
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: sldIdLst 가 비어 슬라이드를 하나도 못 골랐다", ErrUnsupportedFormat)
	}
	return out, nil
}
