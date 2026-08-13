package testutil

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"testing"
)

// docPart 는 만들어진 docx 에서 word/document.xml 바이트를 꺼낸다.
// 검증이 우리 코드에 기대지 않도록 opc 대신 표준 archive/zip 을 쓴다.
func docPart(t *testing.T, raw []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open word/document.xml: %v", err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read word/document.xml: %v", err)
		}
		return b
	}
	t.Fatal("word/document.xml 이 없다")
	return nil
}

// TestMinimalDocxStaysWellFormed 는 문단 수가 늘어도 픽스처가 well-formed XML
// 인지 본다. paraId 를 한 바이트('1'+i)로 채우던 때는 12번째 문단에서
// '1'+11 = 0x3C = '<' 가 속성값 안으로 흘러 XML 이 깨졌고, 그 결과가 테스트
// 실패가 아니라 손상된 문서를 받은 채로의 통과였다.
//
// 검증은 encoding/xml 로 한다 — 우리 xmlscan 으로 재면 같은 관대함을 공유해
// 자기검증이 되어버린다.
func TestMinimalDocxStaysWellFormed(t *testing.T) {
	for _, n := range []int{1, 9, 12, 20, 64} {
		t.Run(fmt.Sprintf("문단%d개", n), func(t *testing.T) {
			paras := make([]string, n)
			for i := range paras {
				paras[i] = fmt.Sprintf("문단 %d", i)
			}
			dec := xml.NewDecoder(bytes.NewReader(docPart(t, MinimalDocx(paras))))
			for {
				_, err := dec.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("문단 %d개에서 XML 이 깨졌다: %v", n, err)
				}
			}
		})
	}
}

// TestMinimalDocxParaIDsAreDistinctHex 는 paraId 가 문단마다 다르고 w14:paraId
// 형식(8자리 16진수)을 지키는지 본다. well-formed 만 보면 ':' 나 ';' 같은
// 비-16진수가 들어가도 통과하는데, 그건 실제 Word 를 흉내내는 픽스처의 목적을
// 무너뜨린다 — parts.VolatileAttrs 가 걸러낼 값이 형식부터 가짜가 된다.
func TestMinimalDocxParaIDsAreDistinctHex(t *testing.T) {
	const n = 20
	paras := make([]string, n)
	for i := range paras {
		paras[i] = fmt.Sprintf("문단 %d", i)
	}
	doc := string(docPart(t, MinimalDocx(paras)))

	seen := make(map[string]bool, n)
	rest := doc
	for {
		i := strings.Index(rest, `w14:paraId="`)
		if i < 0 {
			break
		}
		rest = rest[i+len(`w14:paraId="`):]
		j := strings.IndexByte(rest, '"')
		if j < 0 {
			t.Fatal("닫히지 않은 paraId 속성값")
		}
		id := rest[:j]
		rest = rest[j+1:]

		if len(id) != 8 {
			t.Errorf("paraId %q: 길이 %d, 8 이어야 한다", id, len(id))
		}
		for _, c := range id {
			if !strings.ContainsRune("0123456789ABCDEF", c) {
				t.Errorf("paraId %q: 16진수가 아닌 문자 %q", id, c)
				break
			}
		}
		if seen[id] {
			t.Errorf("paraId %q 가 중복됐다 — 문단마다 달라야 한다", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Errorf("paraId %d개, %d개여야 한다", len(seen), n)
	}
}

// TestMinimalDocxFirstParaIDIsStable 은 첫 문단의 paraId 가 "00000001" 인지
// 잠근다. patch·xmlscan·tmpl 의 테스트 세 곳이 이 문자열을 그대로 써서,
// 여기가 바뀌면 그쪽이 조용히 아무것도 안 지키게 된다.
func TestMinimalDocxFirstParaIDIsStable(t *testing.T) {
	doc := string(docPart(t, MinimalDocx([]string{"제목", "본문"})))
	if !strings.Contains(doc, `w14:paraId="00000001"`) {
		t.Errorf("첫 문단의 paraId 가 00000001 이 아니다:\n%s", doc)
	}
	if !strings.Contains(doc, `w14:paraId="00000002"`) {
		t.Errorf("둘째 문단의 paraId 가 00000002 가 아니다:\n%s", doc)
	}
}
