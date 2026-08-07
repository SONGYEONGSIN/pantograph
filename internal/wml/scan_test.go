package wml_test

import (
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
	"github.com/SONGYEONGSIN/pantograph/internal/wml"
)

func docXML(t *testing.T, paragraphs []string) []byte {
	t.Helper()
	p, err := opc.OpenBytes(testutil.MinimalDocx(paragraphs))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	c, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	return c
}

func TestScanAssignsPaths(t *testing.T) {
	tree, err := wml.Scan(docXML(t, []string{"제목", "본문"}))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, want := range []string{
		"word",
		"word/body[1]",
		"word/body[1]/p[1]",
		"word/body[1]/p[1]/r[1]",
		"word/body[1]/p[1]/r[1]/t[1]",
		"word/body[1]/p[2]/r[1]/t[1]",
	} {
		if _, ok := tree.Lookup(want); !ok {
			t.Errorf("경로 없음: %s", want)
		}
	}
}

func TestScanSpanIsExactOriginalBytes(t *testing.T) {
	src := docXML(t, []string{"제목"})
	tree, err := wml.Scan(src)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	n, ok := tree.Lookup("word/body[1]/p[1]")
	if !ok {
		t.Fatal("word/body[1]/p[1] 없음")
	}
	got := string(tree.Raw(n))
	want := `<w:p w14:paraId="00000001"><w:r><w:t xml:space="preserve">제목</w:t></w:r></w:p>`
	if got != want {
		t.Fatalf("Raw:\n  got  %s\n  want %s", got, want)
	}
}

func TestScanInnerExcludesTags(t *testing.T) {
	src := docXML(t, []string{"제목"})
	tree, err := wml.Scan(src)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	n, ok := tree.Lookup("word/body[1]/p[1]/r[1]/t[1]")
	if !ok {
		t.Fatal("t[1] 없음")
	}
	if got := string(tree.InnerRaw(n)); got != "제목" {
		t.Fatalf("InnerRaw = %q, want %q", got, "제목")
	}
	if got := n.Text; got != "제목" {
		t.Fatalf("Text = %q, want %q", got, "제목")
	}
}

func TestScanSelfClosingElementHasEmptyInner(t *testing.T) {
	src := []byte(`<w:document xmlns:w="http://x"><w:body><w:p><w:pPr><w:b/></w:pPr></w:p></w:body></w:document>`)
	tree, err := wml.Scan(src)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	n, ok := tree.Lookup("word/body[1]/p[1]/pPr[1]/b[1]")
	if !ok {
		t.Fatal("b[1] 없음")
	}
	if got := string(tree.Raw(n)); got != "<w:b/>" {
		t.Fatalf("Raw = %q, want %q", got, "<w:b/>")
	}
	if len(tree.InnerRaw(n)) != 0 {
		t.Fatalf("자기닫힘 요소의 Inner 가 비어있지 않다: %q", tree.InnerRaw(n))
	}
}

func TestScanAttrsPreserveSourceOrder(t *testing.T) {
	src := []byte(`<w:document xmlns:w="http://x" xmlns:w14="http://y"><w:body><w:p w14:paraId="AA" w14:textId="BB"/></w:body></w:document>`)
	tree, err := wml.Scan(src)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	n, ok := tree.Lookup("word/body[1]/p[1]")
	if !ok {
		t.Fatal("p[1] 없음")
	}
	if len(n.Attrs) != 2 {
		t.Fatalf("속성 %d개, 기대 2개: %+v", len(n.Attrs), n.Attrs)
	}
	if n.Attrs[0].Name != "paraId" || n.Attrs[1].Name != "textId" {
		t.Fatalf("속성 순서가 원문과 다르다: %+v", n.Attrs)
	}
	if v, ok := n.Attr("paraId"); !ok || v != "AA" {
		t.Fatalf("Attr(paraId) = %q, %v", v, ok)
	}
}

func TestScanNodesArePreOrder(t *testing.T) {
	tree, err := wml.Scan(docXML(t, []string{"제목"}))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := []string{"word", "word/body[1]", "word/body[1]/p[1]", "word/body[1]/p[1]/r[1]", "word/body[1]/p[1]/r[1]/t[1]"}
	if len(tree.Nodes) != len(want) {
		t.Fatalf("노드 %d개, 기대 %d개", len(tree.Nodes), len(want))
	}
	for i, w := range want {
		if tree.Nodes[i].Path != w {
			t.Fatalf("노드 %d: %s, 기대 %s", i, tree.Nodes[i].Path, w)
		}
	}
}

// TestScanRejectsMalformedXML 은 well-formed 가 아닌 입력이 거절되는지 본다.
// 이 입력은 encoding/xml 디코더가 먼저 잡는다 (태그 짝이 어긋남) — Scan 의
// "닫히지 않은 요소" 분기까지 가지 않는다. 이름이 그 분기를 검증한다고
// 주장하지 않도록 일반적인 파싱 거절로 부른다.
func TestScanRejectsMalformedXML(t *testing.T) {
	_, err := wml.Scan([]byte(`<w:document xmlns:w="http://x"><w:body></w:document>`))
	if err == nil {
		t.Fatal("well-formed 가 아닌 XML 인데 에러가 없다")
	}
}
