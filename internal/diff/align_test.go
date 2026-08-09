package diff

import (
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// scanReal 은 실제 픽스처의 본문 파트 하나를 스캔해 돌려준다.
func scanReal(t *testing.T, name, part string) *xmlscan.Tree {
	t.Helper()
	p, err := opc.Open(filepath.Join("..", "..", "testdata", "real", name))
	if err != nil {
		t.Fatalf("Open %s: %v", name, err)
	}
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open %s: %v", name, err)
	}
	tr, err := d.Tree(part)
	if err != nil {
		t.Fatalf("Tree %s: %v", part, err)
	}
	return tr
}

// TestBuildTreeCoversEveryNodeExactlyOnce 는 트리 구성이 노드를 하나도
// 빠뜨리거나 겹치지 않는지 본다.
//
// Span 포함 관계로 부모·자식을 정하는데, 경계 조건(자기닫힘 요소, 빈 요소)이
// 틀리면 노드가 엉뚱한 부모에 붙거나 사라진다. size 합이 전체 노드 수와 같다는
// 것이 그것을 한 번에 잡는다.
func TestBuildTreeCoversEveryNodeExactlyOnce(t *testing.T) {
	for _, c := range []struct{ file, part string }{
		{"form-a.docx", "word/document.xml"},
		{"deck-a.pptx", "ppt/slides/slide1.xml"},
	} {
		t.Run(c.file, func(t *testing.T) {
			tr := scanReal(t, c.file, c.part)
			root := buildTree(tr)
			if root == nil {
				t.Fatal("루트가 nil 이다")
			}
			if root.size != len(tr.Nodes) {
				t.Fatalf("서브트리 노드 수 %d, 실제 %d — 빠뜨렸거나 겹쳤다", root.size, len(tr.Nodes))
			}
			if root.Path != tr.Nodes[0].Path {
				t.Fatalf("루트가 %q (기대 %q)", root.Path, tr.Nodes[0].Path)
			}
			// 자식의 Span 은 부모 안에 들어 있어야 한다.
			var check func(n *node)
			check = func(n *node) {
				for _, k := range n.kids {
					if k.Span.Start < n.Span.Start || k.Span.End > n.Span.End {
						t.Fatalf("%s 의 자식 %s 가 부모 Span 밖이다", n.Path, k.Path)
					}
					check(k)
				}
			}
			check(root)
		})
	}
}

// TestSignIgnoresVolatileAttrs 는 휘발성 속성이 서브트리 해시를 바꾸지 않는지
// 본다. **이 슬라이스의 급소다** — creationId 가 해시에 들어가면 pptx
// 레이아웃은 문서마다 값이 달라 영영 매칭되지 않고 정렬이 통째로 무너진다.
func TestSignIgnoresVolatileAttrs(t *testing.T) {
	mk := func(creationVal, realVal string) *node {
		tr := &xmlscan.Tree{Nodes: []xmlscan.Node{
			{Path: "sp", Type: "sp", Span: xmlscan.Span{Start: 0, End: 100}},
			{Path: "sp/creationId[1]", Type: "creationId", Span: xmlscan.Span{Start: 10, End: 40},
				Attrs: []xmlscan.Attr{{Name: "val", Value: creationVal}}},
			{Path: "sp/sz[1]", Type: "sz", Span: xmlscan.Span{Start: 40, End: 90},
				Attrs: []xmlscan.Attr{{Name: "val", Value: realVal}}},
		}}
		n := buildTree(tr)
		sign(n)
		return n
	}
	a := mk("111", "24")
	b := mk("999", "24") // creationId 만 다르다
	if a.sig != b.sig {
		t.Fatal("휘발성 속성이 해시를 바꿨다 — pptx 레이아웃이 영영 매칭되지 않는다")
	}
	c := mk("111", "36") // 진짜 속성이 다르다
	if a.sig == c.sig {
		t.Fatal("진짜 속성 차이가 해시에 안 들어갔다")
	}
}

// TestSignDistinguishesTextAndShape 는 해시가 텍스트와 모양을 모두 반영하는지
// 본다. 둘 중 하나라도 빠지면 서로 다른 서브트리가 같다고 매칭된다.
func TestSignDistinguishesTextAndShape(t *testing.T) {
	mk := func(text string, extraKid bool) *node {
		nodes := []xmlscan.Node{
			{Path: "p", Type: "p", Span: xmlscan.Span{Start: 0, End: 100}},
			{Path: "p/t[1]", Type: "t", Text: text, Span: xmlscan.Span{Start: 10, End: 50}},
		}
		if extraKid {
			nodes = append(nodes, xmlscan.Node{Path: "p/t[2]", Type: "t", Text: "덤",
				Span: xmlscan.Span{Start: 50, End: 90}})
		}
		n := buildTree(&xmlscan.Tree{Nodes: nodes})
		sign(n)
		return n
	}
	if mk("가", false).sig == mk("나", false).sig {
		t.Fatal("텍스트 차이가 해시에 안 들어갔다")
	}
	if mk("가", false).sig == mk("가", true).sig {
		t.Fatal("모양 차이가 해시에 안 들어갔다")
	}
	if mk("가", false).sig != mk("가", false).sig {
		t.Fatal("같은 입력이 다른 해시를 냈다 — 결정론적이지 않다")
	}
}
