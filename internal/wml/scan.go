package wml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
)

type frame struct {
	path       string
	start      int
	innerStart int
	counts     map[string]int
	nodeIdx    int
	attrs      []Attr
	text       bytes.Buffer
}

// Scan 은 XML 바이트를 훑어 노드마다 경로와 바이트 범위를 부여한다.
//
// 오프셋은 xml.Decoder.InputOffset 으로 얻는다. InputOffset 은 "가장 최근에
// 반환된 토큰의 끝이자 다음 토큰의 시작"을 가리키므로, 토큰을 읽기 직전의
// 오프셋이 곧 그 토큰의 시작이다. CharData 가 공백까지 토큰으로 반환하므로
// 연속된 오프셋이 입력을 빈틈없이 분할한다.
//
// 같은 입력이면 항상 같은 결과를 낸다 — 난수·시각·맵 순회가 개입하지 않는다.
func Scan(src []byte) (*Tree, error) {
	dec := xml.NewDecoder(bytes.NewReader(src))
	t := &Tree{Src: src, index: make(map[string]int)}

	var stack []*frame
	prev := 0

	for {
		start := prev
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("XML 파싱 실패 (offset %d): %w", start, err)
		}
		end := int(dec.InputOffset())
		prev = end

		switch tk := tok.(type) {
		case xml.StartElement:
			path := "word"
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.counts[tk.Name.Local]++
				path = parent.path + "/" + tk.Name.Local + "[" +
					strconv.Itoa(parent.counts[tk.Name.Local]) + "]"
			}
			if _, dup := t.index[path]; dup {
				return nil, fmt.Errorf("경로 충돌: %s", path)
			}

			f := &frame{
				path:       path,
				start:      start,
				innerStart: end,
				counts:     make(map[string]int),
				nodeIdx:    len(t.Nodes),
			}
			for _, a := range tk.Attr {
				f.attrs = append(f.attrs, Attr{Name: a.Name.Local, NS: a.Name.Space, Value: a.Value})
			}

			t.Nodes = append(t.Nodes, Node{}) // 자리 예약 — 문서 순서를 유지하기 위해
			t.index[path] = f.nodeIdx
			stack = append(stack, f)

		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("짝 없는 종료 태그 (offset %d)", start)
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			t.Nodes[f.nodeIdx] = Node{
				Path:  f.path,
				Type:  tk.Name.Local,
				Span:  Span{Start: f.start, End: end},
				Inner: Span{Start: f.innerStart, End: start},
				Attrs: f.attrs,
				Text:  f.text.String(),
			}

		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(tk)
			}
		}
	}

	if len(stack) > 0 {
		return nil, fmt.Errorf("닫히지 않은 요소: %s", stack[len(stack)-1].path)
	}
	return t, nil
}
