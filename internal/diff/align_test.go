package diff

import (
	"path/filepath"
	"strconv"
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

// mkSigs 는 주어진 해시 문자열들을 가진 형제 목록을 만든다.
// 정렬은 sig 만 보므로 나머지 필드는 비워도 된다.
func mkSigs(sigs ...string) []*node {
	out := make([]*node, len(sigs))
	for i, s := range sigs {
		out[i] = &node{sig: s, size: 1}
	}
	return out
}

// opsString 은 구간 목록을 읽기 쉬운 한 줄로 만든다. 기대값과 대조하기 위한 것이다.
func opsString(ops []op) string {
	s := ""
	for _, o := range ops {
		if s != "" {
			s += " "
		}
		s += string(o.tag) + "a" + itoa(o.aStart) + ":" + itoa(o.aEnd) +
			"b" + itoa(o.bStart) + ":" + itoa(o.bEnd)
	}
	return s
}

func itoa(i int) string { return strconv.Itoa(i) }

// TestAlignSiblings 는 정렬이 네 가지 모양을 정확히 가르는지 본다.
func TestAlignSiblings(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want string
	}{
		{"완전히 같다", []string{"x", "y"}, []string{"x", "y"}, "ea0:2b0:2"},
		{"가운데 삽입", []string{"x", "z"}, []string{"x", "y", "z"},
			"ea0:1b0:1 ia1:1b1:2 ea1:2b2:3"},
		{"가운데 삭제", []string{"x", "y", "z"}, []string{"x", "z"},
			"ea0:1b0:1 da1:2b1:1 ea2:3b1:2"},
		{"양쪽에 남는다", []string{"x", "y", "z"}, []string{"x", "w", "z"},
			"ea0:1b0:1 ra1:2b1:2 ea2:3b2:3"},
		{"기대만 비었다", nil, []string{"x"}, "ia0:0b0:1"},
		{"실제만 비었다", []string{"x"}, nil, "da0:1b0:0"},
		{"둘 다 비었다", nil, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ops, capped := alignSiblings(mkSigs(c.a...), mkSigs(c.b...))
			if capped {
				t.Fatal("상한에 걸릴 입력이 아닌데 capped 다")
			}
			if got := opsString(ops); got != c.want {
				t.Fatalf("정렬이 다르다\n  실제 %s\n  기대 %s", got, c.want)
			}
		})
	}
}

// TestAlignSiblingsCommonPrefixSuffixIsFree 는 앞뒤 공통 구간을 잘라내는지 본다.
// 잘라내지 않으면 상한에 쉽게 걸린다 — 실제 문서는 대부분 앞뒤가 같다.
func TestAlignSiblingsCommonPrefixSuffixIsFree(t *testing.T) {
	// 앞 2500개와 뒤 2500개가 같고 가운데만 다르다. 잘라내지 않으면
	// 5001 × 5001 = 2500만 칸이라 상한(400만)을 넘는다.
	var a, b []string
	for i := 0; i < 2500; i++ {
		a = append(a, "pre"+itoa(i))
		b = append(b, "pre"+itoa(i))
	}
	a = append(a, "가운데-기대")
	b = append(b, "가운데-실제")
	for i := 0; i < 2500; i++ {
		a = append(a, "post"+itoa(i))
		b = append(b, "post"+itoa(i))
	}
	ops, capped := alignSiblings(mkSigs(a...), mkSigs(b...))
	if capped {
		t.Fatal("앞뒤 공통을 잘라냈으면 상한에 안 걸린다 — 잘라내지 않았다")
	}
	if got := opsString(ops); got != "ea0:2500b0:2500 ra2500:2501b2500:2501 ea2501:5001b2501:5001" {
		t.Fatalf("정렬이 다르다: %s", got)
	}
}

// TestAlignSiblingsMidListPureInsertion 은 alignMiddle 안에서 LCS 매칭 사이의
// 순수 삽입(gap() 의 case bj > pb: 분기)이 항목을 내는지 본다.
//
// 이전까지 이 분기는 커버리지 0이었다 — TestAlignSiblings 의 "가운데 삽입"
// 사례([x,z] vs [x,y,z])는 앞뒤 공통을 걷어내면 가운데가 len(a)==0 이 되어
// alignMiddle 의 조기 반환(case len(a) == 0)으로 빠진다. gap() 의 LCS 매칭
// 루프 안 순수 삽입 분기는 그 경로로는 한 번도 실행되지 않는다.
//
// 이 사례는 앞뒤 어디도 통째로 못 잘라내게 만든다: a=[P,Q] 대 b=[P,N,Q,R].
// P 가 앞 공통(p=1)으로 잘리고 나면 가운데는 a=[Q] 대 b=[N,Q,R] — 양쪽 다
// 비지 않은 채로 alignMiddle 의 LCS 갈래로 들어가, Q 매칭 앞뒤로 gap() 이
// 두 번(N 앞에서 한 번, R 뒤에서 한 번) 순수 삽입 분기를 탄다.
func TestAlignSiblingsMidListPureInsertion(t *testing.T) {
	ops, capped := alignSiblings(mkSigs("P", "Q"), mkSigs("P", "N", "Q", "R"))
	if capped {
		t.Fatal("상한에 걸릴 입력이 아닌데 capped 다")
	}
	want := "ea0:1b0:1 ia1:1b1:2 ea1:2b2:3 ia2:2b3:4"
	if got := opsString(ops); got != want {
		t.Fatalf("정렬이 다르다\n  실제 %s\n  기대 %s", got, want)
	}
}

// TestAlignSiblingsCapFallsBackAndSaysSo 는 상한을 넘으면 위치 정렬로
// 떨어지되 **그 사실을 알리는지** 본다. 조용히 떨어지면 거짓 성공이다.
func TestAlignSiblingsCapFallsBackAndSaysSo(t *testing.T) {
	const n = 2001 // 2001 × 2001 = 4,004,001 > 4,000,000
	var a, b []string
	for i := 0; i < n; i++ {
		a = append(a, "A"+itoa(i))
		b = append(b, "B"+itoa(i)) // 하나도 안 겹친다 — 앞뒤 잘라내기가 안 먹는다
	}
	ops, capped := alignSiblings(mkSigs(a...), mkSigs(b...))
	if !capped {
		t.Fatalf("%d × %d 는 상한(%d)을 넘는데 capped 가 아니다", n, n, maxCells)
	}
	if len(ops) != 1 || ops[0].tag != 'r' {
		t.Fatalf("상한 초과는 위치 짝짓기용 r 구간 하나여야 한다: %s", opsString(ops))
	}
}
