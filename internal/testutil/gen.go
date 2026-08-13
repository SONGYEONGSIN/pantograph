// Package testutil 은 테스트용 결정론적 docx 픽스처를 만든다.
package testutil

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"
)

// fixedTime 은 zip 헤더의 수정 시각을 고정한다.
// 시각이 흔들리면 같은 입력에서 다른 바이트가 나와 I3 가 무너진다.
var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`</Types>`

const packageRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`</Relationships>`

// escaper 는 patch 패키지의 이스케이프 규칙과 동일해야 한다.
// 텍스트 노드에서 의미를 갖는 세 글자만 다룬다.
var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// MinimalDocx 는 주어진 문단들로 최소 docx 를 만든다.
// 같은 입력이면 항상 같은 바이트를 낸다.
func MinimalDocx(paragraphs []string) []byte {
	var body strings.Builder
	for i, p := range paragraphs {
		// 문단마다 다른 휘발성 ID — 실제 Word 의 w14:paraId 처럼 8자리 16진수.
		// 한 바이트('1'+i)로 채우던 때는 열째 문단부터 16진수를 벗어났고
		// ('0000000:'), 열두째에서 '1'+11 = 0x3C = '<' 가 속성값 안으로 흘러
		// XML 자체를 깨뜨렸다. 첫 아홉 문단의 값은 그때와 같다.
		body.WriteString(`<w:p w14:paraId="`)
		fmt.Fprintf(&body, "%08X", i+1)
		body.WriteString(`"><w:r><w:t xml:space="preserve">`)
		body.WriteString(escaper.Replace(p))
		body.WriteString(`</w:t></w:r></w:p>`)
	}
	return DocxWithBody(body.String())
}

// DocxWithBody 는 <w:body> 안쪽 마크업을 직접 받아 docx 를 만든다.
// MinimalDocx 가 못 만드는 요소(w:instrText, 들여쓰기 등)를 다루는 테스트용이다.
//
// Package.Replace 로 내용을 갈아끼우는 방식과 다른 점: 그렇게 하면 원본 zip
// 바이트(Package.Source)는 옛 내용 그대로라, Source 를 다시 여는 코드
// (tmpl.Extract 의 템플릿 생성 단계)가 갈아끼운 내용을 보지 못한다.
func DocxWithBody(body string) []byte {
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document ` +
		`xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" ` +
		`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" ` +
		`mc:Ignorable="w14"><w:body>` + body + `</w:body></w:document>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, content string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", packageRels},
		{"word/document.xml", doc},
	} {
		fh := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: fixedTime}
		w, err := zw.CreateHeader(fh)
		if err != nil {
			panic(err) // 테스트 헬퍼 — 여기서 실패하면 테스트 자체가 성립하지 않는다
		}
		if _, err := w.Write([]byte(e.content)); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// ZipOf 는 이름→내용 맵으로 결정론적 zip 을 만든다. Plan 의 거절 경로 시험용이다.
// 맵 순회 순서가 새지 않도록 이름을 정렬해서 쓴다.
func ZipOf(entries map[string]string) []byte {
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		fh := &zip.FileHeader{Name: n, Method: zip.Deflate, Modified: fixedTime}
		w, err := zw.CreateHeader(fh)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write([]byte(entries[n])); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
