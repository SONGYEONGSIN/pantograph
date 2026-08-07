// Package wml 은 WordprocessingML 을 스캔해 노드마다 경로와 바이트 범위를 부여한다.
//
// 이 패키지에는 재직렬화 함수가 없다. 의도적이다.
// XML 트리를 바이트로 되돌리는 경로가 존재하면 무손실이 깨진다 (spec §2.1).
package wml

// Span 은 스캔 대상 바이트 슬라이스 내의 [Start, End) 구간이다.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Len 은 구간 길이다.
func (s Span) Len() int { return s.End - s.Start }

// Attr 은 요소의 속성 하나다. Name 은 로컬명, NS 는 네임스페이스 URI 다.
// 접두사(w:, w14:)는 문서마다 다를 수 있으므로 보존하지 않는다.
type Attr struct {
	Name  string `json:"name"`
	NS    string `json:"ns,omitempty"`
	Value string `json:"value"`
}

// XMLNS 는 xml: 접두사가 가리키는 네임스페이스 URI 다.
// encoding/xml 이 xml:space 같은 속성의 Space 를 이 값으로 번역한다.
const XMLNS = "http://www.w3.org/XML/1998/namespace"

// Node 는 요소 하나다.
//
//	Span  요소 전체 — 시작 태그의 '<' 부터 종료 태그의 '>' 다음까지
//	Inner 시작 태그와 종료 태그 사이. 자기닫힘 요소는 빈 구간
//	Attrs 원문 순서의 속성. **네임스페이스 선언(xmlns:w=…)도 여기 들어온다** —
//	      encoding/xml 이 그것을 일반 속성으로 돌려주기 때문이다. 루트 노드의
//	      경우 {Name:"w", NS:"xmlns"} 같은 항목이 보인다. 진짜 속성만 필요하면
//	      NS == "xmlns" 인 항목을 걸러야 한다
//	Text  이 요소가 **직접** 품은 문자 데이터. 자손의 텍스트는 포함하지 않는다
type Node struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Span  Span   `json:"span"`
	Inner Span   `json:"inner"`
	Attrs []Attr `json:"attrs,omitempty"`
	Text  string `json:"text,omitempty"`
}

// Attr 은 로컬명으로 속성을 찾는다.
// 네임스페이스를 가리지 않으므로, 접두사가 의미를 갖는 속성에는 AttrNS 를 쓸 것.
func (n Node) Attr(local string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name == local {
			return a.Value, true
		}
	}
	return "", false
}

// AttrNS 는 네임스페이스 URI + 로컬명으로 속성을 찾는다.
func (n Node) AttrNS(ns, local string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name == local && a.NS == ns {
			return a.Value, true
		}
	}
	return "", false
}

// Tree 는 스캔 결과다. Nodes 는 문서 순서(pre-order)다.
type Tree struct {
	Src   []byte `json:"-"`
	Nodes []Node `json:"nodes"`

	index map[string]int
}

// Lookup 은 경로로 노드를 찾는다.
func (t *Tree) Lookup(path string) (Node, bool) {
	i, ok := t.index[path]
	if !ok {
		return Node{}, false
	}
	return t.Nodes[i], true
}

// Raw 는 노드의 원문 바이트다.
func (t *Tree) Raw(n Node) []byte { return t.Src[n.Span.Start:n.Span.End] }

// InnerRaw 는 시작/종료 태그를 뺀 안쪽 원문 바이트다.
func (t *Tree) InnerRaw(n Node) []byte { return t.Src[n.Inner.Start:n.Inner.End] }
