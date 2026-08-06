# docx 왕복 무손실 코어 + 자동 템플릿 역추출 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** docx를 바이트 단위 무손실로 덤프·패치하고, 같은 양식 문서 N벌에서 `{{key}}` 템플릿을 자동 역추출하는 Go CLI `panto`를 만든다.

**Architecture:** zip 엔트리는 전부 raw 바이트로 보관하고 `word/document.xml` 하나만 파싱한다. 파싱은 **주소(바이트 범위)를 만들기 위해서만** 하고 XML 트리를 다시 직렬화하지 않는다. 패치는 그 바이트 구간을 갈아끼우는 스플라이싱이다. 템플릿 역추출은 별도 엔진이 아니라 setText 패치를 만드는 기계다.

**Tech Stack:** Go 1.26.5, 표준 라이브러리만 (`archive/zip`, `encoding/xml`, `encoding/json`, `crypto/sha256`)

**설계 문서:** [`docs/superpowers/specs/2026-08-06-docx-roundtrip-design.md`](../specs/2026-08-06-docx-roundtrip-design.md)

## Global Constraints

- **Go 1.26.5** — 설치 확인됨 (`/opt/homebrew/bin/go`)
- **외부 의존 0.** `go.mod`에 `require` 블록이 생기면 안 된다. 표준 라이브러리로만 만든다
- **`internal/wml`에 재직렬화 함수를 두지 않는다.** XML 트리 → 바이트로 되돌리는 함수가 존재하면 안 된다. 있으면 언젠가 누가 쓰고, 그 순간 무손실이 깨진다
- **폴백 금지.** 처리할 수 없는 입력은 조용히 근사하지 말고 거절한다. 에러는 항상 `path`를 단다
- **부분 적용 금지.** 패치는 전부 적용되거나 아무것도 적용되지 않는다
- **경로 문법**: 인덱스는 *같은 부모 아래 같은 로컬명* 기준 1-base로 **항상** 붙인다 — `word/body[1]/p[3]/r[2]/t[1]`. 루트 요소(`w:document`)만 `word`로 인덱스가 없다
- **결정성**: `dump`는 순수 함수여야 한다. 난수·시각·Go 맵 순회 순서가 출력에 새어들면 안 된다 (속성은 정렬된 슬라이스로 낸다)
- **모듈 경로**: `github.com/SONGYEONGSIN/pantograph`
- **커밋 메시지**: Conventional Commit 접두사(영어) + 한국어 본문, 제목 50자 이내

## 선행 조건 — 실제 docx 픽스처

Task 1이 여기 걸린다. 사용자가 아래를 배치해야 한다:

```
testdata/real/base.docx        Word가 저장한 아무 docx 1개
testdata/real/form-a.docx      같은 양식 2벌 (Task 6~7)
testdata/real/form-b.docx
```

없으면 `TestIdentityReal`·`TestLocalityReal`·`TestTemplateReversal`이 **skip이 아니라 FAIL**한다 (spec §10). 이는 의도된 동작이므로 "테스트가 실패하니 skip으로 바꾸자"는 판단을 하지 말 것.

## 파일 구조

| 파일 | 책임 |
|---|---|
| `go.mod` | 모듈 선언. 외부 의존 없음 |
| `internal/opc/package.go` | OPC 컨테이너. zip 엔트리 raw 보관, 파트 압축 해제/교체, 재작성 |
| `internal/opc/package_test.go` | I1 항등 |
| `internal/wml/node.go` | `Span`·`Attr`·`Node`·`Tree` 타입과 조회 |
| `internal/wml/scan.go` | `Scan` — 경로 부여 + 바이트 범위 기록 |
| `internal/wml/scan_test.go` | 경로·Span·결정성 |
| `internal/dump/dump.go` | `Tree` + `Package` → 덤프 JSON |
| `internal/dump/dump_test.go` | I3 결정성 |
| `internal/patch/patch.go` | 패치·에러 타입, JSON 계약 |
| `internal/patch/apply.go` | 경로 해석 → 검증 → 스플라이스 적용 (트랜잭션) |
| `internal/patch/apply_test.go` | I2 국소성, 원자성, 거절 3종 |
| `internal/tmpl/schema.go` | `Key`·`Schema` 타입 |
| `internal/tmpl/extract.go` | N벌 정렬 → 가변부 판별 → 템플릿·스키마 |
| `internal/tmpl/fill.go` | 스키마 + 데이터 → setText 패치 → apply |
| `internal/tmpl/tmpl_test.go` | I4a·I4b, 구조 불일치 |
| `internal/testutil/gen.go` | 결정론적 최소 docx 생성기 (테스트 전용) |
| `cmd/panto/main.go` | 서브커맨드 디스패치, stdout JSON, 종료 코드 |
| `cmd/panto/cmd_dump.go` | `panto dump` |
| `cmd/panto/cmd_apply.go` | `panto apply` |
| `cmd/panto/cmd_tmpl.go` | `panto tmpl extract` / `panto tmpl fill` |

---

## Task 1: OPC 컨테이너 + I1 항등

**Files:**
- Create: `go.mod`
- Create: `internal/opc/package.go`
- Create: `internal/testutil/gen.go`
- Test: `internal/opc/package_test.go`

**Interfaces:**
- Consumes: 없음 (첫 태스크)
- Produces:
  - `opc.OpenBytes(b []byte) (*Package, error)` / `opc.Open(path string) (*Package, error)`
  - `(*Package).Hash string` — 필드. `"sha256:<hex>"`
  - `(*Package).Source() []byte` — 원본 파일 바이트
  - `(*Package).Names() []string` — zip 원본 순서
  - `(*Package).Part(name string) ([]byte, error)` — 압축 해제 내용 (캐시)
  - `(*Package).Replace(name string, content []byte) error`
  - `(*Package).Bytes() ([]byte, error)` — 재작성 결과
  - `testutil.MinimalDocx(paragraphs []string) []byte`

**왜 이걸 맨 앞에 두는가:** 실제 Word docx에서 `CreateRaw` 왕복이 깨지면(ZIP64·data descriptor·extra field) 접근법 전체의 토대가 무너진다. 코드를 더 쌓기 전에 알아야 한다.

- [ ] **Step 1: `go.mod` 생성**

```bash
cd /Users/yss/개발/build/pantograph
/opt/homebrew/bin/go mod init github.com/SONGYEONGSIN/pantograph
```

`go.mod`가 아래 두 줄(+ 빈 줄)로만 이뤄져야 한다. **`require` 블록이 생기면 제약 위반이다.**

```
module github.com/SONGYEONGSIN/pantograph

go 1.26.5
```

`go` 줄의 버전 표기는 툴체인이 정하므로 `1.26`·`1.26.5` 중 무엇이 와도 된다. 확인할 것은 `require`가 없다는 사실뿐이다.

- [ ] **Step 2: 결정론적 최소 docx 생성기 작성**

`internal/testutil/gen.go`:

```go
// Package testutil 은 테스트용 결정론적 docx 픽스처를 만든다.
package testutil

import (
	"archive/zip"
	"bytes"
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
		body.WriteString(`<w:p w14:paraId="0000000`)
		body.WriteByte(byte('1' + i)) // 문단마다 다른 휘발성 ID — 실제 Word 를 흉내낸다
		body.WriteString(`"><w:r><w:t xml:space="preserve">`)
		body.WriteString(escaper.Replace(p))
		body.WriteString(`</w:t></w:r></w:p>`)
	}

	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document ` +
		`xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" ` +
		`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" ` +
		`mc:Ignorable="w14"><w:body>` + body.String() + `</w:body></w:document>`

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
```

- [ ] **Step 3: 실패하는 테스트 작성**

`internal/opc/package_test.go`:

```go
package opc_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

// I1 항등 — 생성 docx
func TestIdentityGenerated(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문 한 줄"})

	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(src, got) {
		t.Fatalf("바이트 불일치: 원본 %d바이트, 재작성 %d바이트", len(src), len(got))
	}
}

// I1 항등 — 실제 Word docx.
// 픽스처가 없으면 FAIL 이다. skip 으로 바꾸지 말 것 (spec §10).
func TestIdentityReal(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "real", "*.docx"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("testdata/real/*.docx 없음 — I1 은 실제 Word 문서로만 의미가 있다 (spec §10)")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			p, err := opc.OpenBytes(src)
			if err != nil {
				t.Fatalf("OpenBytes: %v", err)
			}
			got, err := p.Bytes()
			if err != nil {
				t.Fatalf("Bytes: %v", err)
			}
			if !bytes.Equal(src, got) {
				t.Fatalf("바이트 불일치: 원본 %d바이트, 재작성 %d바이트", len(src), len(got))
			}
		})
	}
}

func TestPartDecompresses(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목"})
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("<w:body>")) {
		t.Fatalf("document.xml 에 <w:body> 없음: %s", content)
	}
}

func TestNamesPreservesZipOrder(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목"})
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	want := []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"}
	got := p.Names()
	if len(got) != len(want) {
		t.Fatalf("엔트리 수 %d, 기대 %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("엔트리 %d: %q, 기대 %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 4: 테스트 실패 확인**

Run: `/opt/homebrew/bin/go test ./internal/opc/ -v`
Expected: 컴파일 실패 — `undefined: opc.OpenBytes`

- [ ] **Step 5: `opc` 패키지 구현**

`internal/opc/package.go`:

```go
// Package opc 는 docx(OPC 컨테이너)를 zip 엔트리 단위로 다룬다.
//
// 핵심 계약: 수정되지 않은 엔트리는 압축을 풀지도, 다시 압축하지도 않는다.
// zip.File.OpenRaw 로 읽은 압축 데이터를 zip.Writer.CreateRaw 로 그대로 흘려보낸다.
// 그래서 "안 건드린 파트는 바이트 동일"이 증명이 아니라 구조로 보장된다.
package opc

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

type part struct {
	file    *zip.File
	header  zip.FileHeader
	raw     []byte // 압축된 원본 바이트
	content []byte // 압축 해제 캐시. nil 이면 아직 안 풀었다
	dirty   bool
}

// Package 는 열린 docx 하나를 나타낸다.
type Package struct {
	// Hash 는 입력 파일 전체 바이트의 sha256 이다 ("sha256:<hex>").
	// 패치의 낙관적 잠금이 이 값을 대조한다.
	Hash string

	src   []byte
	order []string // zip 원본 엔트리 순서
	parts map[string]*part
}

func Open(path string) (*Package, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return OpenBytes(b)
}

func OpenBytes(b []byte) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("zip 열기 실패: %w", err)
	}
	sum := sha256.Sum256(b)
	p := &Package{
		Hash:  "sha256:" + hex.EncodeToString(sum[:]),
		src:   b,
		parts: make(map[string]*part, len(zr.File)),
	}
	for _, f := range zr.File {
		rc, err := f.OpenRaw()
		if err != nil {
			return nil, fmt.Errorf("%s: raw 열기 실패: %w", f.Name, err)
		}
		raw, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("%s: raw 읽기 실패: %w", f.Name, err)
		}
		p.order = append(p.order, f.Name)
		p.parts[f.Name] = &part{file: f, header: f.FileHeader, raw: raw}
	}
	return p, nil
}

// Source 는 원본 파일 바이트를 돌려준다. I1 검증에 쓴다.
func (p *Package) Source() []byte { return p.src }

// Names 는 zip 원본 순서의 엔트리 이름을 돌려준다.
func (p *Package) Names() []string {
	out := make([]string, len(p.order))
	copy(out, p.order)
	return out
}

// Part 는 엔트리의 압축 해제 내용을 돌려준다. 결과는 캐시된다.
func (p *Package) Part(name string) ([]byte, error) {
	pt, ok := p.parts[name]
	if !ok {
		return nil, fmt.Errorf("파트 없음: %s", name)
	}
	if pt.content != nil {
		return pt.content, nil
	}
	rc, err := pt.file.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: 열기 실패: %w", name, err)
	}
	defer rc.Close()
	c, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("%s: 읽기 실패: %w", name, err)
	}
	pt.content = c
	return c, nil
}

// Replace 는 엔트리 내용을 갈아끼운다. 해당 엔트리만 dirty 가 된다.
func (p *Package) Replace(name string, content []byte) error {
	pt, ok := p.parts[name]
	if !ok {
		return fmt.Errorf("파트 없음: %s", name)
	}
	pt.content = content
	pt.dirty = true
	return nil
}

// Write 는 컨테이너를 재작성한다.
// dirty 가 아닌 엔트리는 압축 데이터를 그대로 통과시킨다.
func (p *Package) Write(w io.Writer) error {
	zw := zip.NewWriter(w)
	for _, name := range p.order {
		pt := p.parts[name]
		fh := pt.header // 값 복사 — 원본 헤더를 건드리지 않는다

		if !pt.dirty {
			dst, err := zw.CreateRaw(&fh)
			if err != nil {
				return fmt.Errorf("%s: CreateRaw 실패: %w", name, err)
			}
			if _, err := dst.Write(pt.raw); err != nil {
				return fmt.Errorf("%s: raw 쓰기 실패: %w", name, err)
			}
			continue
		}

		// 재압축 대상이므로 원본의 CRC·크기를 지운다. Writer 가 다시 계산한다.
		fh.CRC32 = 0
		fh.CompressedSize, fh.CompressedSize64 = 0, 0
		fh.UncompressedSize, fh.UncompressedSize64 = 0, 0
		dst, err := zw.CreateHeader(&fh)
		if err != nil {
			return fmt.Errorf("%s: CreateHeader 실패: %w", name, err)
		}
		if _, err := dst.Write(pt.content); err != nil {
			return fmt.Errorf("%s: 쓰기 실패: %w", name, err)
		}
	}
	return zw.Close()
}

// Bytes 는 Write 결과를 메모리로 돌려준다.
func (p *Package) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 6: 테스트 통과 확인**

Run: `/opt/homebrew/bin/go test ./internal/opc/ -v`
Expected:
- `TestIdentityGenerated` PASS
- `TestPartDecompresses` PASS
- `TestNamesPreservesZipOrder` PASS
- `TestIdentityReal` — 픽스처가 있으면 PASS, 없으면 FAIL(의도된 동작)

**`TestIdentityReal`이 픽스처가 있는데도 실패하면 여기서 멈추고 보고할 것.** 실제 Word 파일에서 raw 통과가 깨졌다는 뜻이고, 그건 설계 전체를 다시 봐야 하는 사건이다. 우회하지 말 것.

- [ ] **Step 7: 커밋**

```bash
git add go.mod internal/opc internal/testutil
git commit -m "feat: OPC 컨테이너 raw 통과 왕복 (I1)"
```

---

## Task 2: WML 스캐너 — 경로와 바이트 범위

**Files:**
- Create: `internal/wml/node.go`
- Create: `internal/wml/scan.go`
- Test: `internal/wml/scan_test.go`

**Interfaces:**
- Consumes: `testutil.MinimalDocx`, `opc.Package.Part`
- Produces:
  - `wml.Span{Start, End int}` — `[Start, End)` 바이트 범위
  - `wml.Attr{Name, NS, Value string}` — `Name`은 로컬명, `NS`는 네임스페이스 URI
  - `wml.Node{Path, Type string; Span, Inner Span; Attrs []Attr; Text string}`
  - `wml.Tree{Src []byte; Nodes []Node}`
  - `wml.Scan(src []byte) (*Tree, error)`
  - `(*Tree).Lookup(path string) (Node, bool)`
  - `(*Tree).Raw(n Node) []byte` / `(*Tree).InnerRaw(n Node) []byte`
  - `(Node).Attr(local string) (string, bool)`

**`Inner`가 왜 필요한가:** `setText`는 `<w:t ...>`와 `</w:t>` **사이만** 갈아끼워야 한다. 시작 태그의 속성(`xml:space="preserve"` 등)을 건드리면 원본에 없던 바이트가 생겨 I4a가 깨진다.

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/wml/scan_test.go`:

```go
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

func TestScanRejectsUnclosedElement(t *testing.T) {
	_, err := wml.Scan([]byte(`<w:document xmlns:w="http://x"><w:body></w:document>`))
	if err == nil {
		t.Fatal("닫히지 않은 요소인데 에러가 없다")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `/opt/homebrew/bin/go test ./internal/wml/ -v`
Expected: 컴파일 실패 — `undefined: wml.Scan`

- [ ] **Step 3: 타입 정의**

`internal/wml/node.go`:

```go
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

// Node 는 요소 하나다.
//
//	Span  요소 전체 — 시작 태그의 '<' 부터 종료 태그의 '>' 다음까지
//	Inner 시작 태그와 종료 태그 사이. 자기닫힘 요소는 빈 구간
type Node struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Span  Span   `json:"span"`
	Inner Span   `json:"inner"`
	Attrs []Attr `json:"attrs,omitempty"`
	Text  string `json:"text,omitempty"`
}

// Attr 은 로컬명으로 속성을 찾는다.
func (n Node) Attr(local string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name == local {
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
```

- [ ] **Step 4: 스캐너 구현**

`internal/wml/scan.go`:

```go
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
```

- [ ] **Step 5: 테스트 통과 확인**

Run: `/opt/homebrew/bin/go test ./internal/wml/ -v`
Expected: 7개 테스트 전부 PASS

`TestScanSelfClosingElementHasEmptyInner`가 실패하면 Go 디코더가 자기닫힘 요소의 EndElement에서 오프셋을 어떻게 다루는지 실제 값을 출력해 확인할 것. 추측으로 코드를 고치지 말 것.

- [ ] **Step 6: 커밋**

```bash
git add internal/wml
git commit -m "feat: WML 스캐너 — 경로 부여와 바이트 범위 기록"
```

---

## Task 3: 덤프 + `panto dump` CLI + I3 결정성

**Files:**
- Create: `internal/dump/dump.go`
- Create: `cmd/panto/main.go`
- Create: `cmd/panto/cmd_dump.go`
- Test: `internal/dump/dump_test.go`

**Interfaces:**
- Consumes: `opc.Package`, `wml.Scan`, `wml.Tree`
- Produces:
  - `dump.ScannedPart = "word/document.xml"` — 상수
  - `dump.Doc{Format, Hash string; Parts []string; ScannedPart string}`
  - `dump.Dump{Doc Doc; Nodes []wml.Node}`
  - `dump.Build(p *opc.Package) (*Dump, error)`
  - `dump.Marshal(d *Dump) ([]byte, error)` — 들여쓰기 2칸 JSON
  - `main.fail(code int, errs []patch.Error)` 는 Task 4에서 도입. 이 태스크의 CLI는 성공/실패를 직접 처리한다

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/dump/dump_test.go`:

```go
package dump_test

import (
	"bytes"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

// I3 결정성 — 같은 입력을 두 번 덤프하면 바이트가 같아야 한다.
func TestDumpIsDeterministic(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문", "꼬리말"})

	run := func() []byte {
		p, err := opc.OpenBytes(src)
		if err != nil {
			t.Fatalf("OpenBytes: %v", err)
		}
		d, err := dump.Build(p)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		b, err := dump.Marshal(d)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		return b
	}

	for i := 0; i < 20; i++ {
		if a, b := run(), run(); !bytes.Equal(a, b) {
			t.Fatalf("반복 %d 에서 덤프가 달라졌다\n--- A ---\n%s\n--- B ---\n%s", i, a, b)
		}
	}
}

func TestDumpCarriesHashAndParts(t *testing.T) {
	p, err := opc.OpenBytes(testutil.MinimalDocx([]string{"제목"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	d, err := dump.Build(p)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if d.Doc.Hash != p.Hash {
		t.Fatalf("Hash = %q, want %q", d.Doc.Hash, p.Hash)
	}
	if d.Doc.Format != "docx" {
		t.Fatalf("Format = %q, want %q", d.Doc.Format, "docx")
	}
	if d.Doc.ScannedPart != dump.ScannedPart {
		t.Fatalf("ScannedPart = %q, want %q", d.Doc.ScannedPart, dump.ScannedPart)
	}
	if len(d.Doc.Parts) != 3 {
		t.Fatalf("Parts %d개, want 3: %v", len(d.Doc.Parts), d.Doc.Parts)
	}
	if len(d.Nodes) == 0 {
		t.Fatal("노드가 비었다")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `/opt/homebrew/bin/go test ./internal/dump/ -v`
Expected: 컴파일 실패 — `undefined: dump.Build`

- [ ] **Step 3: `dump` 패키지 구현**

`internal/dump/dump.go`:

```go
// Package dump 는 docx 를 에이전트가 읽을 JSON 으로 내보낸다.
package dump

import (
	"encoding/json"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/wml"
)

// ScannedPart 는 이번 슬라이스가 파싱하는 유일한 파트다.
// 나머지 파트는 raw 로만 다루므로 노드가 없다.
const ScannedPart = "word/document.xml"

type Doc struct {
	Format      string   `json:"format"`
	Hash        string   `json:"hash"`
	Parts       []string `json:"parts"`
	ScannedPart string   `json:"scannedPart"`
}

type Dump struct {
	Doc   Doc        `json:"doc"`
	Nodes []wml.Node `json:"nodes"`
}

// Build 는 패키지를 덤프 구조로 바꾼다.
func Build(p *opc.Package) (*Dump, error) {
	content, err := p.Part(ScannedPart)
	if err != nil {
		return nil, err
	}
	tree, err := wml.Scan(content)
	if err != nil {
		return nil, err
	}
	return &Dump{
		Doc: Doc{
			Format:      "docx",
			Hash:        p.Hash,
			Parts:       p.Names(),
			ScannedPart: ScannedPart,
		},
		Nodes: tree.Nodes,
	}, nil
}

// Marshal 은 덤프를 JSON 으로 직렬화한다.
// 모든 필드가 슬라이스·문자열이라 맵 순회 순서가 개입하지 않는다 (I3).
func Marshal(d *Dump) ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `/opt/homebrew/bin/go test ./internal/dump/ -v`
Expected: 2개 PASS

- [ ] **Step 5: CLI 뼈대와 `dump` 서브커맨드**

`cmd/panto/main.go`:

```go
// panto 는 docx 를 경로 단위로 덤프·패치하는 CLI 다.
// 모든 출력은 stdout JSON, 모든 진단은 stderr 다.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// 종료 코드 (spec §9)
const (
	exitOK       = 0 // 성공
	exitInput    = 1 // 입력 오류 — 경로 미해석 / hash 불일치 / 겹침 / 거절 / 구조 불일치
	exitInternal = 2 // 내부 오류 — 파일 손상 / I/O / 적용 후 재스캔 실패
)

func usage() {
	fmt.Fprint(os.Stderr, `panto — docx 재현 하네스

사용법:
  panto dump  <in.docx>
  panto apply <in.docx> -p <patch.json> -o <out.docx>
  panto tmpl extract <a.docx> <b.docx> [...] -o <tmpl.docx> --schema <schema.json>
  panto tmpl fill    <tmpl.docx> -d <data.json> -o <out.docx>
`)
}

// die 는 진단을 stderr 에 쓰고 종료 코드를 돌려준다.
func die(code int, format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "panto: "+format+"\n", args...)
	return code
}

// emit 은 값을 stdout 에 JSON 으로 쓴다.
func emit(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(b, '\n'))
	return err
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitInput)
	}
	switch os.Args[1] {
	case "dump":
		os.Exit(cmdDump(os.Args[2:]))
	default:
		usage()
		os.Exit(exitInput)
	}
}
```

`cmd/panto/cmd_dump.go`:

```go
package main

import (
	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
)

func cmdDump(args []string) int {
	if len(args) != 1 {
		return die(exitInput, "사용법: panto dump <in.docx>")
	}
	p, err := opc.Open(args[0])
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	d, err := dump.Build(p)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := emit(d); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
```

- [ ] **Step 6: 빌드하고 실제 파일로 손 검증**

```bash
/opt/homebrew/bin/go build -o /tmp/panto ./cmd/panto
/tmp/panto dump testdata/real/base.docx | head -40
/tmp/panto dump testdata/real/base.docx > /tmp/d1.json
/tmp/panto dump testdata/real/base.docx > /tmp/d2.json
cmp /tmp/d1.json /tmp/d2.json && echo "I3 CLI 수준 통과"
```

Expected: `doc`/`nodes`가 보이고, `cmp`가 침묵하며 "I3 CLI 수준 통과" 출력

- [ ] **Step 7: 커밋**

```bash
git add internal/dump cmd/panto
git commit -m "feat: 덤프 JSON 과 panto dump (I3)"
```

---

## Task 4: 패치 엔진 — `replaceRaw`, 원자성, I2

**Files:**
- Create: `internal/patch/patch.go`
- Create: `internal/patch/apply.go`
- Create: `cmd/panto/cmd_apply.go`
- Modify: `cmd/panto/main.go` — `apply` 케이스 추가
- Test: `internal/patch/apply_test.go`

**Interfaces:**
- Consumes: `opc.Package`, `wml.Scan`, `dump.ScannedPart`
- Produces:
  - `patch.Op{Op, Path, Text, XML string}`
  - `patch.Patch{Hash string; Ops []Op}`
  - `patch.Error{Path, Reason, Detail string}` — 이 태스크의 `Reason` ∈ `hash_mismatch`, `path_not_found`, `unknown_op`, `overlap` (Task 5가 `type_mismatch`·`whitespace_needs_preserve`를 더한다)
  - `patch.Apply(p *opc.Package, pt Patch) ([]Error, error)` — 반환된 `[]Error`가 비어있지 않으면 **패키지는 손대지 않은 상태**다. `error`는 내부 오류(종료 코드 2)
  - `patch.Result{OK bool; Errors []Error}` — CLI 출력 봉투

**적용 순서가 왜 내림차순인가:** 스플라이스를 앞에서부터 적용하면 길이가 바뀌면서 뒤쪽 Span의 오프셋이 밀린다. 내림차순으로 적용하면 아직 적용하지 않은 구간의 오프셋이 보존된다.

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/patch/apply_test.go`:

```go
package patch_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

func open(t *testing.T, src []byte) *opc.Package {
	t.Helper()
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	return p
}

// rawEntries 는 엔트리별 압축 데이터를 이름으로 모은다.
func rawEntries(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.OpenRaw()
		if err != nil {
			t.Fatalf("OpenRaw %s: %v", f.Name, err)
		}
		raw, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll %s: %v", f.Name, err)
		}
		out[f.Name] = raw
	}
	return out
}

func TestReplaceRawSubstitutesNode(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{{
			Op:   "replaceRaw",
			Path: "word/body[1]/p[1]",
			XML:  `<w:p><w:r><w:t>바뀐 제목</w:t></w:r></w:p>`,
		}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("에러가 없어야 하는데: %+v", errs)
	}

	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("바뀐 제목")) {
		t.Fatalf("교체가 반영되지 않았다: %s", content)
	}
	if bytes.Contains(content, []byte(`w14:paraId="00000001"`)) {
		t.Fatalf("p[1] 이 통째로 교체되지 않았다: %s", content)
	}
	if !bytes.Contains(content, []byte("본문")) {
		t.Fatalf("p[2] 가 사라졌다: %s", content)
	}
}

// I2 국소성 — 건드리지 않은 엔트리는 압축 데이터까지 동일해야 한다.
func TestLocalityUntouchedEntriesAreByteIdentical(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p><w:r><w:t>X</w:t></w:r></w:p>`}},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("Apply: err=%v errs=%+v", err, errs)
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	before, after := rawEntries(t, src), rawEntries(t, got)
	for name, wantRaw := range before {
		gotRaw, ok := after[name]
		if !ok {
			t.Fatalf("엔트리 사라짐: %s", name)
		}
		if name == "word/document.xml" {
			if bytes.Equal(wantRaw, gotRaw) {
				t.Fatal("수정한 파트인데 압축 데이터가 그대로다")
			}
			continue
		}
		if !bytes.Equal(wantRaw, gotRaw) {
			t.Fatalf("안 건드린 엔트리의 압축 데이터가 달라졌다: %s", name)
		}
	}
}

// I2 국소성 — 실제 Word 문서. 픽스처가 없으면 FAIL (spec §10).
func TestLocalityReal(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "real", "*.docx"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("testdata/real/*.docx 없음 (spec §10)")
	}
	src, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	p := open(t, src)

	// 첫 문단을 빈 문단으로 교체 — document.xml 만 dirty 가 된다.
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p/>`}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("에러: %+v", errs)
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	before, after := rawEntries(t, src), rawEntries(t, got)
	for name, wantRaw := range before {
		if name == "word/document.xml" {
			continue
		}
		if !bytes.Equal(wantRaw, after[name]) {
			t.Fatalf("안 건드린 엔트리의 압축 데이터가 달라졌다: %s", name)
		}
	}
}

func TestAtomicityNothingAppliedOnBadPath(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p><w:r><w:t>유효</w:t></w:r></w:p>`},
			{Op: "replaceRaw", Path: "word/body[1]/p[99]", XML: `<w:p/>`},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("에러 %d개, 기대 1개: %+v", len(errs), errs)
	}
	if errs[0].Path != "word/body[1]/p[99]" || errs[0].Reason != "path_not_found" {
		t.Fatalf("에러가 부정확하다: %+v", errs[0])
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(src, got) {
		t.Fatal("실패한 패치인데 문서가 바뀌었다 — 원자성 위반")
	}
}

func TestHashMismatchRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Ops:  []patch.Op{{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p/>`}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "hash_mismatch" {
		t.Fatalf("hash 불일치가 거절되지 않았다: %+v", errs)
	}
}

func TestOverlapRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p/>`},
			{Op: "replaceRaw", Path: "word/body[1]/p[1]/r[1]", XML: `<w:r/>`}, // p[1] 안쪽
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "overlap" {
		t.Fatalf("겹침이 거절되지 않았다: %+v", errs)
	}
}

func TestUnknownOpRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setProps", Path: "word/body[1]/p[1]"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "unknown_op" {
		t.Fatalf("알 수 없는 연산이 거절되지 않았다: %+v", errs)
	}
}

func TestBrokenXMLIsInternalError(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	_, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "replaceRaw", Path: "word/body[1]/p[1]", XML: `<w:p><w:r>`}},
	})
	if err == nil {
		t.Fatal("깨진 XML 을 넣었는데 에러가 없다")
	}
}

func TestEmptyPatchIsIdentity(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)
	errs, err := patch.Apply(p, patch.Patch{Hash: p.Hash})
	if err != nil || len(errs) != 0 {
		t.Fatalf("Apply: err=%v errs=%+v", err, errs)
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(src, got) {
		t.Fatal("빈 패치인데 바이트가 달라졌다 (I1)")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `/opt/homebrew/bin/go test ./internal/patch/ -v`
Expected: 컴파일 실패 — `undefined: patch.Apply`

- [ ] **Step 3: 패치 타입 정의**

`internal/patch/patch.go`:

```go
// Package patch 는 경로 지정 패치를 바이트 스플라이스로 바꿔 적용한다.
//
// 계약: 전부 적용되거나 아무것도 적용되지 않는다. 부분 적용은 에이전트가
// 문서 상태를 잃는 최악의 실패다 (spec §9).
package patch

// Op 는 패치 연산 하나다. 이번 슬라이스의 연산은 setText 와 replaceRaw 둘뿐이다.
type Op struct {
	Op   string `json:"op"`
	Path string `json:"path"`
	Text string `json:"text,omitempty"` // setText
	XML  string `json:"xml,omitempty"`  // replaceRaw
}

// Patch 는 배치 하나다. Hash 는 낙관적 잠금이며 비어있으면 대조를 건너뛴다.
type Patch struct {
	Hash string `json:"hash,omitempty"`
	Ops  []Op   `json:"ops"`
}

// Error 는 거절 사유다. 항상 경로를 단다 (spec §9).
type Error struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// Result 는 CLI 출력 봉투다.
type Result struct {
	OK     bool    `json:"ok"`
	Errors []Error `json:"errors,omitempty"`
}
```

- [ ] **Step 4: 적용 엔진 구현**

`internal/patch/apply.go`:

```go
package patch

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/wml"
)

type splice struct {
	span wml.Span
	repl []byte
	path string
}

// Apply 는 패치를 적용한다.
//
// 반환된 []Error 가 비어있지 않으면 패키지는 **손대지 않은 상태**다.
// error 는 내부 오류(종료 코드 2)이며, 이때도 패키지는 수정되지 않는다.
func Apply(p *opc.Package, pt Patch) ([]Error, error) {
	if pt.Hash != "" && pt.Hash != p.Hash {
		return []Error{{
			Path:   dump.ScannedPart,
			Reason: "hash_mismatch",
			Detail: fmt.Sprintf("패치 hash=%s, 문서 hash=%s", pt.Hash, p.Hash),
		}}, nil
	}

	content, err := p.Part(dump.ScannedPart)
	if err != nil {
		return nil, err
	}
	tree, err := wml.Scan(content)
	if err != nil {
		return nil, err
	}

	// 1) 모든 op 을 검증한다. 하나라도 실패하면 아무것도 적용하지 않는다.
	var errs []Error
	splices := make([]splice, 0, len(pt.Ops))
	for _, op := range pt.Ops {
		n, ok := tree.Lookup(op.Path)
		if !ok {
			errs = append(errs, Error{
				Path:   op.Path,
				Reason: "path_not_found",
				Detail: nearbyHint(tree, op.Path),
			})
			continue
		}
		switch op.Op {
		case "replaceRaw":
			splices = append(splices, splice{span: n.Span, repl: []byte(op.XML), path: op.Path})
		default:
			errs = append(errs, Error{
				Path:   op.Path,
				Reason: "unknown_op",
				Detail: fmt.Sprintf("알 수 없는 연산: %s (replaceRaw)", op.Op),
			})
		}
	}
	if len(errs) > 0 {
		return errs, nil
	}

	// 2) 겹침 검사
	sort.Slice(splices, func(i, j int) bool { return splices[i].span.Start < splices[j].span.Start })
	for i := 1; i < len(splices); i++ {
		if splices[i].span.Start < splices[i-1].span.End {
			return []Error{{
				Path:   splices[i].path,
				Reason: "overlap",
				Detail: fmt.Sprintf("%s 의 구간과 겹친다", splices[i-1].path),
			}}, nil
		}
	}

	// 3) 내림차순 적용 — 앞에서부터 하면 뒤 구간의 오프셋이 밀린다
	out := make([]byte, len(content))
	copy(out, content)
	for i := len(splices) - 1; i >= 0; i-- {
		s := splices[i]
		var buf bytes.Buffer
		buf.Grow(len(out) - s.span.Len() + len(s.repl))
		buf.Write(out[:s.span.Start])
		buf.Write(s.repl)
		buf.Write(out[s.span.End:])
		out = buf.Bytes()
	}

	// 4) 결과 재스캔 — 바이트를 잘라 붙였으므로 파서가 막아주지 않는다
	if _, err := wml.Scan(out); err != nil {
		return nil, fmt.Errorf("적용 결과가 유효한 XML 이 아니다 (롤백함): %w", err)
	}

	return nil, p.Replace(dump.ScannedPart, out)
}

// nearbyHint 는 경로를 못 찾았을 때 형제 개수를 알려준다.
func nearbyHint(tree *wml.Tree, path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return ""
	}
	parent, leaf := path[:i], path[i+1:]
	j := strings.Index(leaf, "[")
	if j < 0 {
		return ""
	}
	name := leaf[:j]
	count := 0
	for _, n := range tree.Nodes {
		if strings.HasPrefix(n.Path, parent+"/"+name+"[") &&
			!strings.Contains(n.Path[len(parent)+1:], "/") {
			count++
		}
	}
	return fmt.Sprintf("%s 아래 %s 는 %d개", parent, name, count)
}
```

- [ ] **Step 5: 테스트 통과 확인**

Run: `/opt/homebrew/bin/go test ./internal/patch/ -v`
Expected: 8개 전부 PASS (`TestLocalityReal`은 픽스처 필요)

- [ ] **Step 6: `panto apply` 추가**

`cmd/panto/cmd_apply.go`:

```go
package main

import (
	"encoding/json"
	"os"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
)

func cmdApply(args []string) int {
	var in, patchPath, out string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p":
			i++
			if i >= len(args) {
				return die(exitInput, "-p 뒤에 패치 파일 경로가 필요하다")
			}
			patchPath = args[i]
		case "-o":
			i++
			if i >= len(args) {
				return die(exitInput, "-o 뒤에 출력 경로가 필요하다")
			}
			out = args[i]
		default:
			if in != "" {
				return die(exitInput, "입력 파일이 둘 이상이다: %s, %s", in, args[i])
			}
			in = args[i]
		}
	}
	if in == "" || patchPath == "" || out == "" {
		return die(exitInput, "사용법: panto apply <in.docx> -p <patch.json> -o <out.docx>")
	}

	p, err := opc.Open(in)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	pb, err := os.ReadFile(patchPath)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	var pt patch.Patch
	if err := json.Unmarshal(pb, &pt); err != nil {
		return die(exitInput, "패치 JSON 파싱 실패: %v", err)
	}

	errs, err := patch.Apply(p, pt)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	if len(errs) > 0 {
		if err := emit(patch.Result{OK: false, Errors: errs}); err != nil {
			return die(exitInternal, "%v", err)
		}
		return exitInput
	}

	// 출력 파일은 적용이 성공했을 때만 만든다 — 원자성 (spec §9)
	f, err := os.Create(out)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	defer f.Close()
	if err := p.Write(f); err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := emit(patch.Result{OK: true}); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
```

`cmd/panto/main.go`의 switch에 케이스를 추가한다:

```go
	case "apply":
		os.Exit(cmdApply(os.Args[2:]))
```

- [ ] **Step 7: CLI 손 검증 — 원자성**

```bash
/opt/homebrew/bin/go build -o /tmp/panto ./cmd/panto
cat > /tmp/bad.json <<'EOF'
{"ops":[{"op":"replaceRaw","path":"word/body[1]/p[99]","xml":"<w:p/>"}]}
EOF
rm -f /tmp/out.docx
/tmp/panto apply testdata/real/base.docx -p /tmp/bad.json -o /tmp/out.docx
echo "종료 코드: $?"
test ! -f /tmp/out.docx && echo "출력 파일 미생성 — 원자성 OK"
```

Expected: `{"ok": false, "errors": [{"path":"word/body[1]/p[99]","reason":"path_not_found",…}]}`, 종료 코드 1, "출력 파일 미생성 — 원자성 OK"

- [ ] **Step 8: 커밋**

```bash
git add internal/patch cmd/panto
git commit -m "feat: 패치 엔진 — 스플라이싱 적용과 원자성 (I2)"
```

---

## Task 5: `setText` 연산

**Files:**
- Modify: `internal/patch/apply.go` — `setText` 분기 추가
- Test: `internal/patch/apply_test.go` — setText 테스트 추가

**Interfaces:**
- Consumes: Task 4의 `patch.Apply`, `patch.Error`, `wml.Node.Attr`, `wml.Node.Inner`
- Produces: `patch.Apply`가 `setText` 연산을 처리한다. `Error.Reason`에 `type_mismatch`·`whitespace_needs_preserve` 추가

**왜 별도 태스크인가:** Task 4는 `replaceRaw`로 스플라이스 기계를 세운다. setText는 그 위의 얇은 계층이지만 **거절 규칙**(타입·공백)이 독립적으로 검토·거부될 수 있는 계약이다.

**Task 4 시점의 상태:** `setText`는 아직 없다 — `default` 분기로 떨어져 `unknown_op`이 된다. 이 태스크의 RED가 바로 그것이다.

- [ ] **Step 1: 실패하는 테스트 추가**

`internal/patch/apply_test.go` 끝에 붙인다:

```go
func TestSetTextReplacesOnlyInnerText(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Path: "word/body[1]/p[1]/r[1]/t[1]", Text: "새 제목"}},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("Apply: err=%v errs=%+v", err, errs)
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte(`<w:t xml:space="preserve">새 제목</w:t>`)) {
		t.Fatalf("시작 태그가 보존되지 않았거나 텍스트가 안 바뀌었다: %s", content)
	}
	if !bytes.Contains(content, []byte(`w14:paraId="00000001"`)) {
		t.Fatalf("문단의 휘발성 속성이 사라졌다: %s", content)
	}
}

func TestSetTextEscapesMinimally(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Path: "word/body[1]/p[1]/r[1]/t[1]", Text: "a&b<c>d\ne"}},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("Apply: err=%v errs=%+v", err, errs)
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("a&amp;b&lt;c&gt;d\ne")) {
		t.Fatalf("이스케이프가 최소가 아니다 (개행이 문자 참조로 바뀌면 안 된다): %s", content)
	}
}

func TestSetTextRejectsNonTextNode(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Path: "word/body[1]/p[1]", Text: "X"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "type_mismatch" {
		t.Fatalf("w:t 가 아닌 노드가 거절되지 않았다: %+v", errs)
	}
}

func TestSetTextRejectsWhitespaceWithoutPreserve(t *testing.T) {
	// xml:space 속성이 없는 w:t
	src := []byte(`<w:document xmlns:w="http://x"><w:body><w:p><w:r><w:t>제목</w:t></w:r></w:p></w:body></w:document>`)
	p := open(t, testutil.MinimalDocx([]string{"제목"}))
	if err := p.Replace("word/document.xml", src); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	errs, err := patch.Apply(p, patch.Patch{
		Ops: []patch.Op{{Op: "setText", Path: "word/body[1]/p[1]/r[1]/t[1]", Text: " 앞뒤 공백 "}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "whitespace_needs_preserve" {
		t.Fatalf("공백이 거절되지 않았다: %+v", errs)
	}
}

func TestSetTextAllowsWhitespaceWithPreserve(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"})) // 생성기가 preserve 를 붙인다
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Path: "word/body[1]/p[1]/r[1]/t[1]", Text: " 앞뒤 공백 "}},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("preserve 가 있는데 거절됐다: err=%v errs=%+v", err, errs)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인 (RED)**

Run: `/opt/homebrew/bin/go test ./internal/patch/ -run 'TestSetText' -v`
Expected: 5개 전부 FAIL. `setText`가 `default` 분기로 떨어져 `unknown_op` 에러가 나므로:
- `TestSetTextReplacesOnlyInnerText` — `errs` 가 비어있지 않아 실패
- `TestSetTextRejectsNonTextNode` — `Reason`이 `unknown_op`(기대: `type_mismatch`)
- `TestSetTextRejectsWhitespaceWithoutPreserve` — `Reason`이 `unknown_op`(기대: `whitespace_needs_preserve`)

- [ ] **Step 3: `setText` 분기 구현**

`internal/patch/apply.go` 파일 상단, `type splice struct` 바로 앞에 이스케이퍼를 추가한다:

```go
// xmlEscaper 는 텍스트 노드에서 의미를 갖는 세 글자만 이스케이프한다.
// xml.EscapeText 는 개행·탭까지 문자 참조로 바꿔 원본에 없던 바이트를 만든다.
var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
```

`Apply` 의 op switch 에서 `case "replaceRaw":` 와 `default:` 사이에 분기를 넣는다:

```go
		case "setText":
			if n.Type != "t" {
				errs = append(errs, Error{
					Path:   op.Path,
					Reason: "type_mismatch",
					Detail: fmt.Sprintf("setText 는 w:t 에만 쓸 수 있다 (대상 타입: %s)", n.Type),
				})
				continue
			}
			// 앞뒤 공백은 xml:space="preserve" 가 있어야만 허용한다.
			// 없는데 속성을 붙여주면 원본에 없던 바이트가 생겨 I4a 가 깨진다.
			if strings.TrimSpace(op.Text) != op.Text {
				if v, ok := n.Attr("space"); !ok || v != "preserve" {
					errs = append(errs, Error{
						Path:   op.Path,
						Reason: "whitespace_needs_preserve",
						Detail: `대상 w:t 에 xml:space="preserve" 가 없어 앞뒤 공백을 넣을 수 없다. replaceRaw 를 쓸 것`,
					})
					continue
				}
			}
			// Inner 만 교체한다 — 시작 태그의 속성을 건드리면 I4a 가 깨진다.
			splices = append(splices, splice{
				span: n.Inner,
				repl: []byte(xmlEscaper.Replace(op.Text)),
				path: op.Path,
			})
```

`default:` 분기의 안내 문구도 갱신한다:

```go
				Detail: fmt.Sprintf("알 수 없는 연산: %s (setText | replaceRaw)", op.Op),
```

- [ ] **Step 4: 테스트 통과 확인 (GREEN)**

Run: `/opt/homebrew/bin/go test ./internal/patch/ -v`
Expected: Task 4의 테스트를 포함해 전부 PASS (`TestLocalityReal`은 픽스처 필요)

- [ ] **Step 5: 커밋**

```bash
git add internal/patch
git commit -m "feat: setText 연산 — Inner 교체와 타입·공백 거절"
```

---

## Task 6: 템플릿 역추출

**Files:**
- Create: `internal/tmpl/schema.go`
- Create: `internal/tmpl/extract.go`
- Test: `internal/tmpl/tmpl_test.go`

**Interfaces:**
- Consumes: `opc.Package`, `wml.Scan`, `patch.Apply`, `patch.Error`, `dump.ScannedPart`
- Produces:
  - `tmpl.Key{Key, Path string; Samples []string}`
  - `tmpl.Schema{Base, Hash string; Keys []Key}`
  - `tmpl.Extract(pkgs []*opc.Package, names []string) (*opc.Package, *Schema, []patch.Error, error)`
    - `pkgs[0]`이 베이스. `names[i]`는 표시용 파일명
    - 반환 `*opc.Package`는 `{{kN}}`이 박힌 템플릿
    - `Schema.Hash`는 **템플릿의 hash가 아니라 베이스 문서의 hash**다 (`fill`은 템플릿 파일을 따로 열므로 템플릿 hash가 필요 없다)
  - `tmpl.VolatileAttrs` — 비교에서 제외할 속성 로컬명 집합

**비교 방식:** 스펙 §8은 "그 외 원문 바이트 비교"라고 썼는데, 비단말 노드의 원문 바이트는 자손의 가변 텍스트를 포함하므로 그대로 비교하면 항상 다르다. 실제로 비교하는 것은 **요소 자신의 마크업** — `Type` + 휘발성 제외 `Attrs`다. 효과는 같고 오탐이 없다.

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/tmpl/tmpl_test.go`:

```go
package tmpl_test

import (
	"bytes"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
	"github.com/SONGYEONGSIN/pantograph/internal/tmpl"
)

func pkgs(t *testing.T, forms ...[]string) ([]*opc.Package, []string) {
	t.Helper()
	var out []*opc.Package
	var names []string
	for i, f := range forms {
		p, err := opc.OpenBytes(testutil.MinimalDocx(f))
		if err != nil {
			t.Fatalf("OpenBytes[%d]: %v", i, err)
		}
		out = append(out, p)
		names = append(names, string(rune('a'+i))+".docx")
	}
	return out, names
}

func TestExtractFindsVariableParts(t *testing.T) {
	ps, names := pkgs(t,
		[]string{"청구서", "홍길동", "합계"},
		[]string{"청구서", "김철수", "합계"},
	)

	tp, sch, errs, err := tmpl.Extract(ps, names)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("에러: %+v", errs)
	}
	if len(sch.Keys) != 1 {
		t.Fatalf("키 %d개, 기대 1개: %+v", len(sch.Keys), sch.Keys)
	}
	k := sch.Keys[0]
	if k.Key != "k1" {
		t.Fatalf("키 이름 %q, 기대 %q", k.Key, "k1")
	}
	if k.Path != "word/body[1]/p[2]/r[1]/t[1]" {
		t.Fatalf("키 경로 %q", k.Path)
	}
	if len(k.Samples) != 2 || k.Samples[0] != "홍길동" || k.Samples[1] != "김철수" {
		t.Fatalf("샘플이 부정확하다: %+v", k.Samples)
	}
	if sch.Base != "a.docx" {
		t.Fatalf("Base = %q", sch.Base)
	}
	if sch.Hash != ps[0].Hash {
		t.Fatalf("Hash 가 베이스 문서의 것이 아니다")
	}

	content, err := tp.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("{{k1}}")) {
		t.Fatalf("템플릿에 {{k1}} 이 없다: %s", content)
	}
	if !bytes.Contains(content, []byte("청구서")) || !bytes.Contains(content, []byte("합계")) {
		t.Fatalf("고정부가 사라졌다: %s", content)
	}
	if bytes.Contains(content, []byte("홍길동")) {
		t.Fatalf("가변부가 남아있다: %s", content)
	}
}

func TestExtractAssignsKeysInDocumentOrder(t *testing.T) {
	ps, names := pkgs(t,
		[]string{"A1", "고정", "B1"},
		[]string{"A2", "고정", "B2"},
	)
	_, sch, errs, err := tmpl.Extract(ps, names)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}
	if len(sch.Keys) != 2 {
		t.Fatalf("키 %d개, 기대 2개", len(sch.Keys))
	}
	if sch.Keys[0].Key != "k1" || sch.Keys[0].Path != "word/body[1]/p[1]/r[1]/t[1]" {
		t.Fatalf("k1 이 문서 순서의 첫 가변부가 아니다: %+v", sch.Keys[0])
	}
	if sch.Keys[1].Key != "k2" || sch.Keys[1].Path != "word/body[1]/p[3]/r[1]/t[1]" {
		t.Fatalf("k2 가 부정확하다: %+v", sch.Keys[1])
	}
}

func TestExtractRejectsStructureMismatch(t *testing.T) {
	ps, names := pkgs(t,
		[]string{"A", "B", "C"},
		[]string{"A", "B"}, // 문단 수가 다르다
	)
	_, _, errs, err := tmpl.Extract(ps, names)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "structure_mismatch" {
		t.Fatalf("구조 불일치가 거절되지 않았다: %+v", errs)
	}
	if errs[0].Path == "" {
		t.Fatal("갈라진 경로를 지목하지 않았다")
	}
}

func TestExtractIgnoresVolatileAttrs(t *testing.T) {
	// MinimalDocx 는 문단마다 다른 w14:paraId 를 넣지만
	// 두 문서의 같은 위치 문단은 같은 paraId 를 갖는다.
	// 여기서는 paraId 가 달라도 통과하는지 직접 확인한다.
	a, err := opc.OpenBytes(testutil.MinimalDocx([]string{"고정", "가변A"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	b, err := opc.OpenBytes(testutil.MinimalDocx([]string{"고정", "가변B"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	bc, err := b.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	bc = bytes.ReplaceAll(bc, []byte(`w14:paraId="00000001"`), []byte(`w14:paraId="DEADBEEF"`))
	if err := b.Replace("word/document.xml", bc); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	_, sch, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("휘발성 속성 차이로 거절됐다: %+v", errs)
	}
	if len(sch.Keys) != 1 {
		t.Fatalf("키 %d개, 기대 1개", len(sch.Keys))
	}
}

func TestExtractRejectsNonTextDiff(t *testing.T) {
	a, err := opc.OpenBytes(testutil.MinimalDocx([]string{"고정"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	b, err := opc.OpenBytes(testutil.MinimalDocx([]string{"고정"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	bc, err := b.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	// 휘발성이 아닌 속성을 추가한다
	bc = bytes.ReplaceAll(bc, []byte(`<w:t xml:space="preserve">`), []byte(`<w:t xml:space="default">`))
	if err := b.Replace("word/document.xml", bc); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "b.docx"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "nontext_diff" {
		t.Fatalf("텍스트 외 차이가 거절되지 않았다: %+v", errs)
	}
}

func TestExtractRequiresTwoDocuments(t *testing.T) {
	ps, names := pkgs(t, []string{"A"})
	_, _, errs, err := tmpl.Extract(ps, names)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "too_few_documents" {
		t.Fatalf("문서 1벌인데 거절되지 않았다: %+v", errs)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `/opt/homebrew/bin/go test ./internal/tmpl/ -v`
Expected: 컴파일 실패 — `undefined: tmpl.Extract`

- [ ] **Step 3: 스키마 타입 정의**

`internal/tmpl/schema.go`:

```go
// Package tmpl 은 같은 양식 문서 N벌에서 {{key}} 템플릿을 역추출하고 되채운다.
//
// 별도 엔진이 아니다 — Extract 는 setText 패치를 만드는 기계이고
// Fill 은 스키마와 데이터로 setText 패치를 만들어 patch.Apply 에 넘긴다.
// 그래서 I1~I3 가 템플릿 층까지 자동으로 덮는다.
package tmpl

// Key 는 템플릿의 가변 자리 하나다.
type Key struct {
	Key     string   `json:"key"`
	Path    string   `json:"path"`
	Samples []string `json:"samples"`
}

// Schema 는 역추출 결과다. Hash 는 베이스 문서의 hash 다.
type Schema struct {
	Base string `json:"base"`
	Hash string `json:"hash"`
	Keys []Key  `json:"keys"`
}

// VolatileAttrs 는 문서마다 달라도 "같은 양식"으로 보는 속성의 로컬명이다.
// Word 가 문단·저장마다 새로 붙이는 식별자들이라 내용과 무관하다.
var VolatileAttrs = map[string]bool{
	"paraId": true,
	"textId": true,
}

// isVolatile 은 속성이 비교에서 제외되는지 판정한다.
// w:rsid* 계열(rsidR, rsidRDefault, rsidP, rsidRPr, rsidTr, rsidDel, rsidSect)은
// 접두사로 잡는다 — Word 버전마다 종류가 늘어난다.
func isVolatile(local string) bool {
	if VolatileAttrs[local] {
		return true
	}
	return len(local) >= 4 && local[:4] == "rsid"
}
```

- [ ] **Step 4: 역추출 구현**

`internal/tmpl/extract.go`:

```go
package tmpl

import (
	"fmt"
	"strconv"

	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/wml"
)

// Extract 는 같은 양식 문서 N벌에서 템플릿과 스키마를 뽑는다.
// pkgs[0] 이 베이스이며 템플릿은 베이스를 기반으로 만들어진다.
//
// 반환된 []patch.Error 가 비어있지 않으면 템플릿·스키마는 nil 이다.
func Extract(pkgs []*opc.Package, names []string) (*opc.Package, *Schema, []patch.Error, error) {
	if len(pkgs) < 2 {
		return nil, nil, []patch.Error{{
			Path:   dump.ScannedPart,
			Reason: "too_few_documents",
			Detail: fmt.Sprintf("문서 %d벌 — 가변부를 판별하려면 2벌 이상이 필요하다", len(pkgs)),
		}}, nil
	}
	if len(names) != len(pkgs) {
		return nil, nil, nil, fmt.Errorf("문서 %d개에 이름 %d개", len(pkgs), len(names))
	}

	trees := make([]*wml.Tree, len(pkgs))
	for i, p := range pkgs {
		content, err := p.Part(dump.ScannedPart)
		if err != nil {
			return nil, nil, nil, err
		}
		tr, err := wml.Scan(content)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", names[i], err)
		}
		trees[i] = tr
	}

	base := trees[0]

	// 1) 구조 정렬 — 경로 집합이 완전히 일치해야 한다
	for i := 1; i < len(trees); i++ {
		if e := diffStructure(base, trees[i], names[0], names[i]); e != nil {
			return nil, nil, []patch.Error{*e}, nil
		}
	}

	// 2) 노드별 비교 — 경로가 같으므로 인덱스가 정렬돼 있다
	var keys []Key
	var ops []patch.Op
	for idx, bn := range base.Nodes {
		if e := diffMarkup(bn, trees, idx, names); e != nil {
			return nil, nil, []patch.Error{*e}, nil
		}
		if bn.Type != "t" {
			continue
		}
		varies := false
		for i := 1; i < len(trees); i++ {
			if trees[i].Nodes[idx].Text != bn.Text {
				varies = true
				break
			}
		}
		if !varies {
			continue
		}
		key := "k" + strconv.Itoa(len(keys)+1)
		samples := make([]string, len(trees))
		for i := range trees {
			samples[i] = trees[i].Nodes[idx].Text
		}
		keys = append(keys, Key{Key: key, Path: bn.Path, Samples: samples})
		ops = append(ops, patch.Op{Op: "setText", Path: bn.Path, Text: "{{" + key + "}}"})
	}

	// 3) 베이스의 사본에 패치를 적용해 템플릿을 만든다
	tp, err := opc.OpenBytes(pkgs[0].Source())
	if err != nil {
		return nil, nil, nil, err
	}
	errs, err := patch.Apply(tp, patch.Patch{Hash: tp.Hash, Ops: ops})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(errs) > 0 {
		return nil, nil, errs, nil
	}

	return tp, &Schema{Base: names[0], Hash: pkgs[0].Hash, Keys: keys}, nil, nil
}

// diffStructure 는 두 트리의 경로 순열이 같은지 본다.
func diffStructure(a, b *wml.Tree, an, bn string) *patch.Error {
	n := len(a.Nodes)
	if len(b.Nodes) < n {
		n = len(b.Nodes)
	}
	for i := 0; i < n; i++ {
		if a.Nodes[i].Path != b.Nodes[i].Path {
			return &patch.Error{
				Path:   a.Nodes[i].Path,
				Reason: "structure_mismatch",
				Detail: fmt.Sprintf("%s 는 %s, %s 는 %s", an, a.Nodes[i].Path, bn, b.Nodes[i].Path),
			}
		}
	}
	if len(a.Nodes) != len(b.Nodes) {
		longer, name, short := a, an, len(b.Nodes)
		if len(b.Nodes) > len(a.Nodes) {
			longer, name, short = b, bn, len(a.Nodes)
		}
		return &patch.Error{
			Path:   longer.Nodes[short].Path,
			Reason: "structure_mismatch",
			Detail: fmt.Sprintf("%s 에만 있는 노드 (노드 수 %d vs %d)", name, len(a.Nodes), len(b.Nodes)),
		}
	}
	return nil
}

// diffMarkup 은 요소 자신의 마크업(타입 + 휘발성 제외 속성)이 같은지 본다.
// 자손의 텍스트는 여기서 보지 않는다 — 그건 가변부 판별의 몫이다.
func diffMarkup(bn wml.Node, trees []*wml.Tree, idx int, names []string) *patch.Error {
	baseAttrs := stableAttrs(bn)
	for i := 1; i < len(trees); i++ {
		other := trees[i].Nodes[idx]
		if other.Type != bn.Type {
			return &patch.Error{
				Path:   bn.Path,
				Reason: "nontext_diff",
				Detail: fmt.Sprintf("%s 는 %s, %s 는 %s", names[0], bn.Type, names[i], other.Type),
			}
		}
		otherAttrs := stableAttrs(other)
		if len(otherAttrs) != len(baseAttrs) {
			return &patch.Error{
				Path:   bn.Path,
				Reason: "nontext_diff",
				Detail: fmt.Sprintf("속성 수가 다르다 (%s: %d, %s: %d)", names[0], len(baseAttrs), names[i], len(otherAttrs)),
			}
		}
		for j := range baseAttrs {
			if baseAttrs[j] != otherAttrs[j] {
				return &patch.Error{
					Path:   bn.Path,
					Reason: "nontext_diff",
					Detail: fmt.Sprintf("속성 %s: %s 는 %q, %s 는 %q",
						baseAttrs[j].Name, names[0], baseAttrs[j].Value, names[i], otherAttrs[j].Value),
				}
			}
		}
	}
	return nil
}

// stableAttrs 는 휘발성 속성을 뺀 속성 목록이다. 원문 순서를 유지한다.
func stableAttrs(n wml.Node) []wml.Attr {
	out := make([]wml.Attr, 0, len(n.Attrs))
	for _, a := range n.Attrs {
		if isVolatile(a.Name) {
			continue
		}
		out = append(out, a)
	}
	return out
}
```

- [ ] **Step 5: 테스트 통과 확인**

Run: `/opt/homebrew/bin/go test ./internal/tmpl/ -v`
Expected: 6개 전부 PASS

- [ ] **Step 6: 커밋**

```bash
git add internal/tmpl
git commit -m "feat: 템플릿 역추출 — 가변부 판별과 스키마 산출"
```

---

## Task 7: 템플릿 채우기 + I4 + `panto tmpl` CLI

**Files:**
- Create: `internal/tmpl/fill.go`
- Create: `cmd/panto/cmd_tmpl.go`
- Modify: `cmd/panto/main.go` — `tmpl` 케이스 추가
- Modify: `internal/tmpl/tmpl_test.go` — I4a/I4b 추가

**Interfaces:**
- Consumes: Task 6의 `tmpl.Extract`, `tmpl.Schema`, `patch.Apply`
- Produces:
  - `tmpl.Fill(tp *opc.Package, sch *Schema, data map[string]string) ([]patch.Error, error)`
    - `tp`를 제자리에서 수정한다. `Reason` 추가: `missing_key`, `template_drift`
  - `tmpl.Values(p *opc.Package, sch *Schema) (map[string]string, error)` — 문서에서 스키마 키의 실제 값을 뽑는다 (I4 검증에 필요)

**`Values`가 왜 필요한가:** I4는 "원래 값으로 되채우면 원본이 나온다"는 주장이다. 그 "원래 값"을 문서에서 기계적으로 뽑아야 테스트가 성립한다.

- [ ] **Step 1: 실패하는 테스트 추가**

`internal/tmpl/tmpl_test.go` 끝에 붙인다:

```go
// I4a — 베이스 문서에 대한 바이트 수준 가역성
func TestTemplateReversalBase(t *testing.T) {
	forms := [][]string{
		{"청구서", "홍길동", "1,200,000"},
		{"청구서", "김철수", "880,000"},
	}
	ps, names := pkgs(t, forms...)

	tp, sch, errs, err := tmpl.Extract(ps, names)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}

	vals, err := tmpl.Values(ps[0], sch)
	if err != nil {
		t.Fatalf("Values: %v", err)
	}

	filled, err := opc.OpenBytes(mustBytes(t, tp))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	fillErrs, err := tmpl.Fill(filled, sch, vals)
	if err != nil || len(fillErrs) != 0 {
		t.Fatalf("Fill: err=%v errs=%+v", err, fillErrs)
	}

	want, err := ps[0].Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	got, err := filled.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("I4a 위반 — 베이스로 되채웠는데 원본과 다르다\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// I4b — 나머지 문서에 대한 텍스트 수준 일치
func TestTemplateReversalOthersTextLevel(t *testing.T) {
	ps, names := pkgs(t,
		[]string{"청구서", "홍길동", "1,200,000"},
		[]string{"청구서", "김철수", "880,000"},
	)

	tp, sch, errs, err := tmpl.Extract(ps, names)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}

	vals, err := tmpl.Values(ps[1], sch)
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	filled, err := opc.OpenBytes(mustBytes(t, tp))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if fe, err := tmpl.Fill(filled, sch, vals); err != nil || len(fe) != 0 {
		t.Fatalf("Fill: err=%v errs=%+v", err, fe)
	}

	wantTexts := textsOf(t, ps[1])
	gotTexts := textsOf(t, filled)
	if len(wantTexts) != len(gotTexts) {
		t.Fatalf("텍스트 노드 수 %d vs %d", len(gotTexts), len(wantTexts))
	}
	for i := range wantTexts {
		if wantTexts[i] != gotTexts[i] {
			t.Fatalf("I4b 위반 — 텍스트 %d: %q, 기대 %q", i, gotTexts[i], wantTexts[i])
		}
	}
}

func TestFillRejectsMissingKey(t *testing.T) {
	ps, names := pkgs(t, []string{"고정", "A"}, []string{"고정", "B"})
	tp, sch, errs, err := tmpl.Extract(ps, names)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}
	fe, err := tmpl.Fill(tp, sch, map[string]string{})
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if len(fe) != 1 || fe[0].Reason != "missing_key" {
		t.Fatalf("빠진 키가 거절되지 않았다: %+v", fe)
	}
}

func TestFillRejectsTemplateDrift(t *testing.T) {
	ps, names := pkgs(t, []string{"고정", "A"}, []string{"고정", "B"})
	tp, sch, errs, err := tmpl.Extract(ps, names)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}
	// 템플릿에서 자리표시자를 지운다
	c, err := tp.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	c = bytes.ReplaceAll(c, []byte("{{k1}}"), []byte("엉뚱한 값"))
	if err := tp.Replace("word/document.xml", c); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	fe, err := tmpl.Fill(tp, sch, map[string]string{"k1": "X"})
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if len(fe) != 1 || fe[0].Reason != "template_drift" {
		t.Fatalf("템플릿 드리프트가 거절되지 않았다: %+v", fe)
	}
}

func mustBytes(t *testing.T, p *opc.Package) []byte {
	t.Helper()
	b, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return b
}

func textsOf(t *testing.T, p *opc.Package) []string {
	t.Helper()
	c, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	tr, err := wml.Scan(c)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var out []string
	for _, n := range tr.Nodes {
		if n.Type == "t" {
			out = append(out, n.Text)
		}
	}
	return out
}
```

`internal/tmpl/tmpl_test.go`의 import에 `wml`을 추가한다:

```go
	"github.com/SONGYEONGSIN/pantograph/internal/wml"
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `/opt/homebrew/bin/go test ./internal/tmpl/ -v`
Expected: 컴파일 실패 — `undefined: tmpl.Fill`, `undefined: tmpl.Values`

- [ ] **Step 3: 채우기 구현**

`internal/tmpl/fill.go`:

```go
package tmpl

import (
	"fmt"

	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/wml"
)

// placeholder 는 키의 자리표시자 문자열이다.
func placeholder(key string) string { return "{{" + key + "}}" }

// Values 는 문서에서 스키마 키의 실제 값을 뽑는다.
// I4 검증("원래 값으로 되채우면 원본이 나온다")의 입력을 만든다.
func Values(p *opc.Package, sch *Schema) (map[string]string, error) {
	content, err := p.Part(dump.ScannedPart)
	if err != nil {
		return nil, err
	}
	tree, err := wml.Scan(content)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(sch.Keys))
	for _, k := range sch.Keys {
		n, ok := tree.Lookup(k.Path)
		if !ok {
			return nil, fmt.Errorf("%s: 경로 없음 (%s)", k.Key, k.Path)
		}
		out[k.Key] = n.Text
	}
	return out, nil
}

// Fill 은 템플릿의 자리표시자를 데이터로 채운다. tp 를 제자리에서 수정한다.
//
// 새 엔진이 아니다 — setText 패치를 만들어 patch.Apply 에 넘긴다.
func Fill(tp *opc.Package, sch *Schema, data map[string]string) ([]patch.Error, error) {
	content, err := tp.Part(dump.ScannedPart)
	if err != nil {
		return nil, err
	}
	tree, err := wml.Scan(content)
	if err != nil {
		return nil, err
	}

	var errs []patch.Error
	ops := make([]patch.Op, 0, len(sch.Keys))
	for _, k := range sch.Keys {
		v, ok := data[k.Key]
		if !ok {
			errs = append(errs, patch.Error{
				Path:   k.Path,
				Reason: "missing_key",
				Detail: fmt.Sprintf("데이터에 %s 가 없다", k.Key),
			})
			continue
		}
		n, ok := tree.Lookup(k.Path)
		if !ok {
			errs = append(errs, patch.Error{
				Path:   k.Path,
				Reason: "template_drift",
				Detail: fmt.Sprintf("템플릿에 %s 경로가 없다", k.Path),
			})
			continue
		}
		if n.Text != placeholder(k.Key) {
			errs = append(errs, patch.Error{
				Path:   k.Path,
				Reason: "template_drift",
				Detail: fmt.Sprintf("자리표시자 %s 를 기대했는데 %q 다", placeholder(k.Key), n.Text),
			})
			continue
		}
		ops = append(ops, patch.Op{Op: "setText", Path: k.Path, Text: v})
	}
	if len(errs) > 0 {
		return errs, nil
	}

	// 템플릿 파일은 사용자가 따로 열므로 hash 대조는 하지 않는다.
	return patch.Apply(tp, patch.Patch{Ops: ops})
}
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `/opt/homebrew/bin/go test ./internal/tmpl/ -v`
Expected: 10개 전부 PASS

**`TestTemplateReversalBase`(I4a)가 실패하면 diff를 끝까지 읽을 것.** 어느 바이트가 왜 달라졌는지가 곧 역추출이 무엇을 잃었는지다. 테스트를 느슨하게 바꾸지 말고 원인을 고칠 것.

- [ ] **Step 5: `panto tmpl` 서브커맨드**

`cmd/panto/cmd_tmpl.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/tmpl"
)

func cmdTmpl(args []string) int {
	if len(args) == 0 {
		return die(exitInput, "사용법: panto tmpl extract|fill …")
	}
	switch args[0] {
	case "extract":
		return cmdTmplExtract(args[1:])
	case "fill":
		return cmdTmplFill(args[1:])
	default:
		return die(exitInput, "알 수 없는 하위 명령: %s", args[0])
	}
}

func cmdTmplExtract(args []string) int {
	var inputs []string
	var out, schemaPath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			i++
			if i >= len(args) {
				return die(exitInput, "-o 뒤에 출력 경로가 필요하다")
			}
			out = args[i]
		case "--schema":
			i++
			if i >= len(args) {
				return die(exitInput, "--schema 뒤에 출력 경로가 필요하다")
			}
			schemaPath = args[i]
		default:
			inputs = append(inputs, args[i])
		}
	}
	if len(inputs) < 2 || out == "" || schemaPath == "" {
		return die(exitInput, "사용법: panto tmpl extract <a.docx> <b.docx> [...] -o <tmpl.docx> --schema <schema.json>")
	}

	pkgs := make([]*opc.Package, len(inputs))
	names := make([]string, len(inputs))
	for i, in := range inputs {
		p, err := opc.Open(in)
		if err != nil {
			return die(exitInternal, "%v", err)
		}
		pkgs[i] = p
		names[i] = filepath.Base(in)
	}

	tp, sch, errs, err := tmpl.Extract(pkgs, names)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	if len(errs) > 0 {
		if err := emit(patch.Result{OK: false, Errors: errs}); err != nil {
			return die(exitInternal, "%v", err)
		}
		return exitInput
	}

	f, err := os.Create(out)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	defer f.Close()
	if err := tp.Write(f); err != nil {
		return die(exitInternal, "%v", err)
	}
	sb, err := json.MarshalIndent(sch, "", "  ")
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := os.WriteFile(schemaPath, append(sb, '\n'), 0o644); err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := emit(patch.Result{OK: true}); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}

func cmdTmplFill(args []string) int {
	var in, dataPath, out, schemaPath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-d":
			i++
			if i >= len(args) {
				return die(exitInput, "-d 뒤에 데이터 파일 경로가 필요하다")
			}
			dataPath = args[i]
		case "-o":
			i++
			if i >= len(args) {
				return die(exitInput, "-o 뒤에 출력 경로가 필요하다")
			}
			out = args[i]
		case "--schema":
			i++
			if i >= len(args) {
				return die(exitInput, "--schema 뒤에 스키마 경로가 필요하다")
			}
			schemaPath = args[i]
		default:
			if in != "" {
				return die(exitInput, "입력 파일이 둘 이상이다: %s, %s", in, args[i])
			}
			in = args[i]
		}
	}
	if in == "" || dataPath == "" || out == "" || schemaPath == "" {
		return die(exitInput, "사용법: panto tmpl fill <tmpl.docx> --schema <schema.json> -d <data.json> -o <out.docx>")
	}

	tp, err := opc.Open(in)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	sb, err := os.ReadFile(schemaPath)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	var sch tmpl.Schema
	if err := json.Unmarshal(sb, &sch); err != nil {
		return die(exitInput, "스키마 JSON 파싱 실패: %v", err)
	}
	db, err := os.ReadFile(dataPath)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	var data map[string]string
	if err := json.Unmarshal(db, &data); err != nil {
		return die(exitInput, "데이터 JSON 파싱 실패: %v", err)
	}

	errs, err := tmpl.Fill(tp, &sch, data)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	if len(errs) > 0 {
		if err := emit(patch.Result{OK: false, Errors: errs}); err != nil {
			return die(exitInternal, "%v", err)
		}
		return exitInput
	}

	f, err := os.Create(out)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	defer f.Close()
	if err := tp.Write(f); err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := emit(patch.Result{OK: true}); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
```

`cmd/panto/main.go`의 switch에 케이스를 추가한다:

```go
	case "tmpl":
		os.Exit(cmdTmpl(os.Args[2:]))
```

- [ ] **Step 6: 전체 테스트 + 실제 픽스처로 손 검증**

```bash
/opt/homebrew/bin/go vet ./...
/opt/homebrew/bin/go test ./... -v 2>&1 | tail -40
/opt/homebrew/bin/go build -o /tmp/panto ./cmd/panto

/tmp/panto tmpl extract testdata/real/form-a.docx testdata/real/form-b.docx \
  -o /tmp/tmpl.docx --schema /tmp/schema.json
cat /tmp/schema.json
```

Expected: `go vet` 무출력, 전체 테스트 PASS, `schema.json`에 키·경로·샘플이 보임

- [ ] **Step 7: 커밋**

```bash
git add internal/tmpl cmd/panto
git commit -m "feat: 템플릿 채우기와 panto tmpl (I4)"
```

---

## Task 8: README 갱신과 스펙 정합

**Files:**
- Modify: `README.md` — "지금 상태" 절
- Modify: `docs/superpowers/specs/2026-08-06-docx-roundtrip-design.md` — 경로 문법 예시

**Interfaces:**
- Consumes: 없음 (문서 작업)
- Produces: 없음

- [ ] **Step 1: 스펙의 경로 예시를 구현과 맞춘다**

`docs/superpowers/specs/2026-08-06-docx-roundtrip-design.md`에서 인덱스 없는 경로 예시를 전부 고친다:

| 고칠 곳 | 현재 | 바꿀 것 |
|---|---|---|
| §6 경로 부여 규칙 | `word/body/p[3]/r[1]/t[1]` | `word/body[1]/p[3]/r[1]/t[1]` |
| §6 덤프 JSON 예시 | `word/body/p[1]`, `word/body/p[1]/r[1]/t[1]` | `word/body[1]/p[1]`, `word/body[1]/p[1]/r[1]/t[1]` |
| §7 패치 JSON 예시 | `word/body/p[1]/r[1]`, `word/body/tbl[1]` | `word/body[1]/p[1]/r[1]/t[1]`, `word/body[1]/tbl[1]` |
| §8 스키마 예시 | `word/body/p[2]/r[1]/t[1]` 등 | `word/body[1]/…` |
| §9 에러 예시 | `word/body/p[99]` | `word/body[1]/p[99]` |

§6의 경로 부여 규칙 항목에 근거를 한 줄 추가한다:

```markdown
- 인덱스를 **항상** 붙인다. 형제가 하나일 때 생략하는 규칙은 문서를 끝까지 읽어야
  결정되므로 단일 패스 스캔이 불가능하고, 형제가 하나 늘면 기존 경로가 바뀌어
  경로 안정성도 깨진다
```

§8의 비교 방식 서술도 구현과 맞춘다 — "그 외 원문 바이트 비교" 문장 뒤에 붙인다:

```markdown
구현상 비교 대상은 **요소 자신의 마크업**(`Type` + 휘발성 제외 `Attrs`)이다. 비단말
노드의 원문 바이트는 자손의 가변 텍스트를 포함하므로 그대로 비교하면 가변부가
있는 모든 조상이 거짓 불일치를 낸다.
```

- [ ] **Step 2: README 갱신**

`README.md`의 "지금 상태" 절을 바꾼다:

```markdown
## 지금 상태

**docx 수직 슬라이스 구현 완료.** 덤프 · 패치 · 템플릿 역추출까지.

- [설계 문서](docs/2026-08-06-pantograph-design.md) — 이름·시각 재현 전략·조작 표면 계약·렌더링 엔진·진화 루프
- [설계 브리프](docs/2026-08-06-design-brief.md) — 전제와 확인이 필요한 가정
- [docx 왕복 무손실 설계](docs/superpowers/specs/2026-08-06-docx-roundtrip-design.md) — 첫 슬라이스의 확정 설계

### 쓰는 법

```
panto dump  <in.docx>                                     → stdout 덤프 JSON
panto apply <in.docx> -p patch.json -o out.docx
panto tmpl extract <a.docx> <b.docx> [...] -o tmpl.docx --schema schema.json
panto tmpl fill    <tmpl.docx> --schema schema.json -d data.json -o out.docx
```

빌드: `go build -o panto ./cmd/panto` (Go 1.26+, 외부 의존 없음)
```

"다음 작업" 절도 바꾼다:

```markdown
## 다음 작업

1. **벤치마크 문서 세트 구축** — 합격 임계를 여기서 도출한다
2. **렌더 → 비교 → 보정 루프** — `setProps` 연산과 함께
3. `{{key}}` 형식 간 병합의 의미 정의 (설계에서 미해결로 남은 유일한 항목)
4. 구조가 다른 문서 간 템플릿 추출 (LCS 정렬)
```

- [ ] **Step 3: 커밋**

```bash
git add README.md docs/superpowers/specs/2026-08-06-docx-roundtrip-design.md
git commit -m "docs: 구현과 스펙·README 정합"
```

---

## 완료 기준

전부 통과해야 이 슬라이스가 끝난 것이다.

```bash
/opt/homebrew/bin/go vet ./...
/opt/homebrew/bin/go test ./... -count=1
```

- `go vet` 무출력
- 전체 테스트 PASS (실제 픽스처 포함)
- `go.mod`에 `require` 블록 없음
- `grep -rn "func.*Marshal\|func.*Serialize\|func.*String() string" internal/wml/` 결과 없음 — 재직렬화 함수가 안 생겼는지 확인
