package parts_test

import (
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// TestStableAttrsIsElementScoped 는 (요소, 속성) 짝 휘발성이 그 요소에서만
// 듣는지 본다.
//
// 같은 속성 이름이 어떤 요소에서는 휘발성이고 어떤 요소에서는 내용이다.
// a:fld 의 id 는 문서마다 다른 식별자지만 p:cNvPr 의 id 는 도형 정체성이고,
// w:rsid 의 val 은 개정 저장 ID 지만 w:sz 의 val 은 글자 크기다.
// 속성 이름만으로 저격하면 둘 중 하나는 반드시 틀린다.
func TestStableAttrsIsElementScoped(t *testing.T) {
	cases := []struct {
		name string
		node xmlscan.Node
		want []string // 남아야 할 속성 이름, 원문 순서
	}{
		{
			"a:fld 는 id 만 빠지고 type 은 남는다",
			xmlscan.Node{Type: "fld", Attrs: []xmlscan.Attr{
				{Name: "id", Value: "{2A621118-2E33-1746-9985-973F71BF27EF}"},
				{Name: "type", Value: "datetimeFigureOut"},
			}},
			[]string{"type"},
		},
		{
			"p:cNvPr 의 id 는 도형 정체성이라 남는다",
			xmlscan.Node{Type: "cNvPr", Attrs: []xmlscan.Attr{
				{Name: "id", Value: "5"}, {Name: "name", Value: "제목 1"},
			}},
			[]string{"id", "name"},
		},
		{
			"w:rsid 의 val 은 빠진다",
			xmlscan.Node{Type: "rsid", Attrs: []xmlscan.Attr{
				{Name: "val", Value: "005A4B1B"},
			}},
			nil,
		},
		{
			"w:rsidRoot 의 val 도 빠진다",
			xmlscan.Node{Type: "rsidRoot", Attrs: []xmlscan.Attr{
				{Name: "val", Value: "005A4B1B"},
			}},
			nil,
		},
		{
			"w:sz 의 val 은 글자 크기라 남는다",
			xmlscan.Node{Type: "sz", Attrs: []xmlscan.Attr{
				{Name: "val", Value: "24"},
			}},
			[]string{"val"},
		},
		{
			"creationId 은 요소째 속성이 전부 빠진다 (기존 규칙)",
			xmlscan.Node{Type: "creationId", Attrs: []xmlscan.Attr{
				{Name: "val", Value: "2265694434"},
			}},
			nil,
		},
		{
			"paraId 는 어느 요소에서든 빠진다 (기존 규칙)",
			xmlscan.Node{Type: "p", Attrs: []xmlscan.Attr{
				{Name: "paraId", Value: "00000001"}, {Name: "rsidR", Value: "005A4B1B"},
			}},
			nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parts.StableAttrs(c.node)
			if len(got) != len(c.want) {
				t.Fatalf("속성 %d개 (기대 %d개): %+v", len(got), len(c.want), got)
			}
			for i, w := range c.want {
				if got[i].Name != w {
					t.Fatalf("%d번째가 %q (기대 %q): %+v", i, got[i].Name, w, got)
				}
			}
		})
	}
}
