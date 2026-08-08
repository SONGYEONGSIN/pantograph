# 다중 파트 일반화 + pptx 바인딩 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** "본문 파트가 하나"라는 가정을 걷어내고 pptx 를 읽고 쓸 수 있게 만든다.

**Architecture:** 노드를 `(파트, 경로)` 로 식별하고 `Span` 은 순수 바이트 범위로 남긴다. 포맷 지식은 신규 `internal/parts` 한 곳에 가둔다 — `dump`·`patch`·`tmpl` 은 파트 지도만 받고 포맷을 모른다. 스캐너는 원래 포맷 무관이었으므로 `internal/wml` 을 `internal/xmlscan` 으로 개명하고 루트 별칭을 주입받게 한다.

**Tech Stack:** Go 1.26.5, 표준 라이브러리만

**설계 문서:** [`docs/superpowers/specs/2026-08-08-multipart-design.md`](../specs/2026-08-08-multipart-design.md)

## Global Constraints

- **Go 1.26.5** — `/opt/homebrew/bin/go` (기본 PATH 에 없다)
- **외부 의존 0.** `go.mod` 에 `require` 블록이 생기면 안 된다
- **`internal/xmlscan` 에 재직렬화 함수를 두지 않는다** — `Marshal`/`Encode`/markup 을 재구성하는 `String()` 금지. 개명 후에도 이 금지는 그대로다
- **폴백 금지.** 처리할 수 없는 입력은 거절한다. 에러는 파트와 경로 중 어디서 틀렸는지 구분해서 말한다
- **부분 적용 금지.** 파트가 여럿이어도 전부 적용되거나 전무다
- **결정성 (I3)**: 난수·시각·Go 맵 순회 순서가 출력에 새면 안 된다. `parts.Plan` 의 출력 순서도 결정론적이어야 한다
- **지연 로딩**: `apply` 는 op 이 가리킨 파트만 압축 해제한다
- 모든 CLI 출력은 stdout JSON, 진단은 stderr, 종료 코드 0/1/2
- 커밋 메시지: Conventional Commit 접두사(영어) + 한국어 본문, 제목 50자 이내
- 코드 주석은 한국어 — 설계 근거를 담고 있으므로 그대로 유지한다

## 선행 조건

`testdata/real/deck-a.pptx` 가 이미 작업 트리에 있다(미커밋). PowerPoint 16.x(macOS) 산출물, 3 장 덱. Task 6 이 커밋한다.

`testdata/real/deck-b.pptx`(같은 양식 2 벌째)는 Task 9 가 만든다. 절차는 프로젝트 메모리의 `word-fixture-generation` 과 같되 PowerPoint 를 쓴다 — 컨테이너에 저장시키고 복사해 온다.

## 파일 구조

| 파일 | 책임 |
|---|---|
| `internal/xmlscan/node.go` | `Span`·`Attr`·`Node`·`Tree` 타입과 조회 (`wml` 에서 개명) |
| `internal/xmlscan/scan.go` | `Scan(src, rootAlias)` |
| `internal/parts/content_types.go` | `[Content_Types].xml` 파싱 → 파트별 ContentType |
| `internal/parts/plan.go` | `Plan` — 본문 파트 선별, pptx 슬라이드 순서 |
| `internal/parts/document.go` | `Document` — 지연 스캔, `Lookup`/`Resolve`/`Select` |
| `internal/dump/dump.go` | 파트별 노드 집합 JSON |
| `internal/patch/apply.go` | 파트 인식 해석·겹침·원자 적용 |
| `internal/tmpl/extract.go`, `fill.go`, `schema.go` | 파트 인식 템플릿 |
| `cmd/panto/cmd_dump.go` | `--part` 선택자 |

---

## Task 1: `wml` → `xmlscan` 순수 개명

**Files:**
- Rename: `internal/wml/` → `internal/xmlscan/` (node.go, scan.go, scan_test.go)
- Modify: `internal/dump/dump.go`, `internal/patch/apply.go`, `internal/tmpl/extract.go`, `internal/tmpl/fill.go`, `internal/tmpl/tmpl_test.go`, `internal/patch/apply_test.go` — import 경로와 패키지 한정자

**Interfaces:**
- Consumes: 없음
- Produces: `xmlscan` 패키지가 `wml` 과 **동일한 API** 를 갖는다 — `Span`, `Attr`, `Node`, `Tree`, `XMLNS`, `Scan(src []byte) (*Tree, error)`, `(*Tree).Lookup/Raw/InnerRaw`, `(Node).Attr/AttrNS`

**이 태스크는 동작을 하나도 바꾸지 않는다.** 패키지 이름과 import 경로만 바뀐다. 테스트 파일의 단언은 한 글자도 고치지 않는다 — 고쳐야 한다면 개명이 아니라 무언가 부순 것이다.

- [ ] **Step 1: 개명 전 기준선 기록**

```bash
cd /Users/yss/개발/build/pantograph
/opt/homebrew/bin/go test ./... -count=1 2>&1 | tee /tmp/before.txt | grep -E '^(ok|FAIL|---)'
```

전 패키지 PASS 여야 한다. 이 출력이 개명 후와 같아야 한다.

- [ ] **Step 2: 디렉토리와 패키지 선언 변경**

```bash
git mv internal/wml internal/xmlscan
cd internal/xmlscan
sed -i '' 's/^package wml$/package xmlscan/' node.go scan.go
sed -i '' 's/^package wml_test$/package xmlscan_test/' scan_test.go
```

- [ ] **Step 3: import 경로와 한정자 일괄 치환**

```bash
cd /Users/yss/개발/build/pantograph
grep -rl 'internal/wml' --include='*.go' . | while read -r f; do
  sed -i '' \
    -e 's#github.com/SONGYEONGSIN/pantograph/internal/wml#github.com/SONGYEONGSIN/pantograph/internal/xmlscan#g' \
    -e 's/\bwml\./xmlscan./g' "$f"
done
grep -rn '\bwml\b' --include='*.go' . || echo "wml 잔존 0건"
```

- [ ] **Step 4: 패키지 문서 주석의 이름 정정**

`internal/xmlscan/node.go` 의 패키지 주석 첫 줄이 `// Package wml 은 …` 이면 `// Package xmlscan 은 …` 으로 바꾼다. **재직렬화 함수를 두지 않는다는 문장은 그대로 둔다** — 그것이 이 패키지의 강제 장치다.

- [ ] **Step 5: 개명이 동작을 안 바꿨는지 확인**

```bash
/opt/homebrew/bin/go vet ./...
/opt/homebrew/bin/go test ./... -count=1 2>&1 | grep -E '^(ok|FAIL|---)' > /tmp/after.txt
diff <(sed 's#/internal/wml#/internal/xmlscan#' /tmp/before.txt | grep -E '^(ok|FAIL|---)') /tmp/after.txt \
  && echo "동일 ✓" || echo "달라졌다 — 개명이 무언가 부쉈다"
```

Expected: `동일 ✓`. 다르면 **멈추고 보고할 것.** 순수 개명이 결과를 바꿨다면 진단이 필요하다.

- [ ] **Step 6: 재직렬화 함수 금지 확인**

```bash
grep -rn "func.*Marshal\|func.*Encode\|func.*String() string" internal/xmlscan/ || echo "0건 ✓"
```

- [ ] **Step 7: 커밋**

```bash
git add -A
git commit -m "refactor: wml 을 xmlscan 으로 개명"
```

---

## Task 2: 루트 별칭 주입

**Files:**
- Modify: `internal/xmlscan/scan.go` — `Scan` 시그니처
- Modify: `internal/xmlscan/scan_test.go`, `internal/dump/dump.go`, `internal/patch/apply.go`, `internal/patch/apply_test.go`, `internal/tmpl/extract.go`, `internal/tmpl/fill.go`, `internal/tmpl/tmpl_test.go`

**Interfaces:**
- Consumes: Task 1 의 `xmlscan`
- Produces: `xmlscan.Scan(src []byte, rootAlias string) (*Tree, error)` — 루트 요소의 경로가 `rootAlias` 가 된다

**docx 경로가 바뀐다**: `word/body[1]/p[1]` → `document/body[1]/p[1]`. 선행 슬라이스의 `"word"` 는 루트 요소가 아니라 디렉토리 이름과 우연히 같은 문자열이었다 (설계 §3).

- [ ] **Step 1: 실패하는 테스트로 고친다**

`internal/xmlscan/scan_test.go` 의 `docXML` 헬퍼 아래에 새 테스트를 추가한다:

```go
func TestScanUsesInjectedRootAlias(t *testing.T) {
	src := docXML(t, []string{"제목"})

	tree, err := xmlscan.Scan(src, "document")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, ok := tree.Lookup("document/body[1]/p[1]"); !ok {
		t.Error("document/body[1]/p[1] 없음")
	}
	if _, ok := tree.Lookup("word/body[1]/p[1]"); ok {
		t.Error("옛 루트 별칭 word 가 여전히 붙는다")
	}

	// 별칭은 주입값 그대로다 — 루트 요소의 로컬명과 무관하게
	tree2, err := xmlscan.Scan(src, "sld")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, ok := tree2.Lookup("sld/body[1]/p[1]"); !ok {
		t.Error("sld/body[1]/p[1] 없음 — 별칭이 주입되지 않았다")
	}
}
```

그리고 `scan_test.go` 의 기존 `xmlscan.Scan(x)` 호출을 전부 `xmlscan.Scan(x, "document")` 로, 경로 문자열 `word/` 를 `document/` 로 바꾼다.

- [ ] **Step 2: RED 확인**

Run: `/opt/homebrew/bin/go test ./internal/xmlscan/ -run TestScanUsesInjectedRootAlias -v`
Expected: 컴파일 실패 — `too many arguments in call to xmlscan.Scan`

- [ ] **Step 3: 구현**

`internal/xmlscan/scan.go` 의 `Scan` 시그니처와 루트 경로 결정부를 고친다:

```go
// Scan 은 XML 바이트를 훑어 노드마다 경로와 바이트 범위를 부여한다.
//
// rootAlias 는 루트 요소가 가질 경로다. 파트마다 다르다 —
// word/document.xml 은 "document", ppt/slides/slideN.xml 은 "sld".
// 경로는 파트 안으로 스코프되므로 유일성은 (파트, 경로) 쌍이 만든다.
func Scan(src []byte, rootAlias string) (*Tree, error) {
```

함수 본문의 `path := "word"` 를 `path := rootAlias` 로 바꾼다. 그 외는 손대지 않는다.

- [ ] **Step 4: 호출부 갱신**

```bash
cd /Users/yss/개발/build/pantograph
grep -rn 'xmlscan.Scan(' --include='*.go' . | grep -v xmlscan/
```

각 호출에 `, "document"` 를 더한다 — 이 시점에는 아직 docx 만 다루므로 전부 `"document"` 다. 대상:
`internal/dump/dump.go`, `internal/patch/apply.go`(2곳: 본 스캔과 재스캔, 그리고 `blame` 안), `internal/tmpl/extract.go`, `internal/tmpl/fill.go`.

- [ ] **Step 5: 테스트의 경로 문자열 갱신**

```bash
grep -rln '"word/' --include='*_test.go' .
sed -i '' 's#"word/#"document/#g' internal/patch/apply_test.go internal/tmpl/tmpl_test.go
grep -rn '"word/' --include='*.go' . || echo "word/ 경로 잔존 0건 ✓"
```

`internal/patch/apply_test.go` 의 `nearbyHint` 관련 단언(“body 에 w:p 는 N개” 류 detail 문자열)에 `word` 가 섞여 있으면 함께 고친다.

- [ ] **Step 6: GREEN 확인**

Run: `/opt/homebrew/bin/go test ./... -count=1`
Expected: 전 패키지 PASS. **실제 Word 픽스처 테스트(`TestIdentityReal`·`TestLocalityReal`·`TestTemplateReversalReal`)가 통과해야 한다** — 경로 표기가 바뀌어도 바이트 수준 불변식은 그대로여야 하고, 이 셋이 그 증거다.

- [ ] **Step 7: 커밋**

```bash
git add -A
git commit -m "refactor: 루트 별칭을 주입받게 하고 docx 를 document 로"
```

---

## Task 3: `parts.Plan` — 파트 지도

**Files:**
- Create: `internal/parts/content_types.go`
- Create: `internal/parts/plan.go`
- Test: `internal/parts/plan_test.go`

**Interfaces:**
- Consumes: `opc.Package` (`Names()`, `Part(name)`)
- Produces:
  - `parts.Part{Name, Ref, Root string}`
  - `parts.Plan(p *opc.Package) (format string, ps []Part, err error)`
  - `parts.ErrUnsupportedFormat` — 알려진 본문 파트를 못 찾았을 때

**Ref 규칙**: docx 본문은 `docx/document`, pptx 슬라이드는 `pptx/slide[N]` (N 은 `sldIdLst` 순서, 1-base).
**Root 규칙**: ContentType 이 정한다 — docx 본문 `document`, pptx 슬라이드 `sld`.

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/parts/plan_test.go`:

```go
package parts_test

import (
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

func openReal(t *testing.T, name string) *opc.Package {
	t.Helper()
	p, err := opc.Open(filepath.Join("..", "..", "testdata", "real", name))
	if err != nil {
		t.Fatalf("Open %s: %v", name, err)
	}
	return p
}

func TestPlanDocx(t *testing.T) {
	p, err := opc.OpenBytes(testutil.MinimalDocx([]string{"제목"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	format, ps, err := parts.Plan(p)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if format != "docx" {
		t.Fatalf("format = %q, want docx", format)
	}
	if len(ps) != 1 {
		t.Fatalf("본문 파트 %d개, want 1: %+v", len(ps), ps)
	}
	if ps[0].Name != "word/document.xml" || ps[0].Ref != "docx/document" || ps[0].Root != "document" {
		t.Fatalf("%+v", ps[0])
	}
}

// pptx 는 실제 PowerPoint 산출물로만 의미가 있다.
func TestPlanOrdersSlidesByPresentation(t *testing.T) {
	p := openReal(t, "deck-a.pptx")
	format, ps, err := parts.Plan(p)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if format != "pptx" {
		t.Fatalf("format = %q, want pptx", format)
	}
	if len(ps) != 3 {
		t.Fatalf("슬라이드 %d개, want 3: %+v", len(ps), ps)
	}
	for i, want := range []string{"pptx/slide[1]", "pptx/slide[2]", "pptx/slide[3]"} {
		if ps[i].Ref != want {
			t.Errorf("ps[%d].Ref = %q, want %q", i, ps[i].Ref, want)
		}
		if ps[i].Root != "sld" {
			t.Errorf("ps[%d].Root = %q, want sld", i, ps[i].Root)
		}
	}

	// 순서는 sldIdLst 가 정한다. 파일명 정렬과 우연히 같을 수는 있어도
	// 그것에 기대지 않는다 — 여기서는 세 파트가 모두 슬라이드인지만 본다.
	for i, pt := range ps {
		if got := pt.Name; len(got) < 17 || got[:17] != "ppt/slides/slide" {
			t.Errorf("ps[%d].Name = %q — 슬라이드 파트가 아니다", i, got)
		}
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	p := openReal(t, "deck-a.pptx")
	_, a, err := parts.Plan(p)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for i := 0; i < 10; i++ {
		_, b, err := parts.Plan(p)
		if err != nil {
			t.Fatalf("Plan: %v", err)
		}
		if len(a) != len(b) {
			t.Fatalf("길이가 달라졌다: %d vs %d", len(a), len(b))
		}
		for j := range a {
			if a[j] != b[j] {
				t.Fatalf("반복 %d, 파트 %d 이 달라졌다: %+v vs %+v", i, j, a[j], b[j])
			}
		}
	}
}

func TestPlanRejectsUnknownFormat(t *testing.T) {
	// 본문 파트가 없는 최소 OPC 컨테이너
	src := testutil.ZipOf(map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="text/xml"/></Types>`,
		"junk.xml":            `<a/>`,
	})
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if _, _, err := parts.Plan(p); err == nil {
		t.Fatal("알려진 본문 파트가 없는데 에러가 없다")
	}
}
```

- [ ] **Step 2: 테스트 헬퍼 `testutil.ZipOf` 추가**

`internal/testutil/gen.go` 끝에 붙인다:

```go
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
```

import 에 `"sort"` 를 더한다.

- [ ] **Step 3: RED 확인**

Run: `/opt/homebrew/bin/go test ./internal/parts/ -v`
Expected: 컴파일 실패 — `no required module provides package .../internal/parts`

- [ ] **Step 4: ContentType 파서**

`internal/parts/content_types.go`:

```go
package parts

import (
	"encoding/xml"
	"fmt"
	"path"
	"strings"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
)

// contentTypes 는 [Content_Types].xml 이 선언한 파트별 ContentType 이다.
// Override 가 Default 를 이긴다 (OPC 규약).
type contentTypes struct {
	byExt  map[string]string // 확장자(소문자, 점 없음) → ContentType
	byPart map[string]string // 파트 경로(선행 / 없음) → ContentType
}

func readContentTypes(p *opc.Package) (*contentTypes, error) {
	raw, err := p.Part("[Content_Types].xml")
	if err != nil {
		return nil, fmt.Errorf("[Content_Types].xml 없음: %w", err)
	}
	var doc struct {
		Defaults []struct {
			Extension   string `xml:"Extension,attr"`
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Default"`
		Overrides []struct {
			PartName    string `xml:"PartName,attr"`
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Override"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("[Content_Types].xml 파싱 실패: %w", err)
	}

	ct := &contentTypes{byExt: map[string]string{}, byPart: map[string]string{}}
	for _, d := range doc.Defaults {
		ct.byExt[strings.ToLower(d.Extension)] = d.ContentType
	}
	for _, o := range doc.Overrides {
		// PartName 은 "/word/document.xml" 처럼 선행 / 를 갖는다. opc 의 이름에는 없다.
		ct.byPart[strings.TrimPrefix(o.PartName, "/")] = o.ContentType
	}
	return ct, nil
}

func (ct *contentTypes) of(partName string) string {
	if t, ok := ct.byPart[partName]; ok {
		return t
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(partName), "."))
	return ct.byExt[ext]
}
```

- [ ] **Step 5: `Plan` 구현**

`internal/parts/plan.go`:

```go
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

// ErrUnsupportedFormat 은 알려진 본문 파트를 하나도 못 찾았을 때다.
// CLI 는 이것을 stdout JSON + 종료 코드 1 로 낸다 — 입력 파일의 성질이지 도구의 고장이 아니다.
var ErrUnsupportedFormat = errors.New("알려진 본문 파트를 찾지 못했다")

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
		return "", nil, err
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

	return "", nil, ErrUnsupportedFormat
}

// orderSlides 는 presentation.xml 의 sldIdLst 순서로 슬라이드 파트를 낸다.
// 파일명 순서를 쓰지 않는 이유: 파일명이 발표 순서와 일치한다는 보장이 없고,
// 어긋났을 때 알아낼 방법도 없다 (설계 §4).
func orderSlides(p *opc.Package, ct *contentTypes) ([]Part, error) {
	presRaw, err := p.Part("ppt/presentation.xml")
	if err != nil {
		return nil, fmt.Errorf("ppt/presentation.xml 없음: %w", err)
	}
	relsRaw, err := p.Part("ppt/_rels/presentation.xml.rels")
	if err != nil {
		return nil, fmt.Errorf("ppt/_rels/presentation.xml.rels 없음: %w", err)
	}

	var pres struct {
		SldIDs []struct {
			RID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sldIdLst>sldId"`
	}
	if err := xml.Unmarshal(presRaw, &pres); err != nil {
		return nil, fmt.Errorf("presentation.xml 파싱 실패: %w", err)
	}

	var rels struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(relsRaw, &rels); err != nil {
		return nil, fmt.Errorf("presentation.xml.rels 파싱 실패: %w", err)
	}
	target := make(map[string]string, len(rels.Rels))
	for _, r := range rels.Rels {
		target[r.ID] = r.Target
	}

	out := make([]Part, 0, len(pres.SldIDs))
	for i, s := range pres.SldIDs {
		tgt, ok := target[s.RID]
		if !ok {
			return nil, fmt.Errorf("sldId 의 관계 %s 를 rels 에서 못 찾았다", s.RID)
		}
		// Target 은 ppt/ 기준 상대 경로다 ("slides/slide1.xml").
		name := path.Join("ppt", tgt)
		if ct.of(name) != ctPptxSlide {
			return nil, fmt.Errorf("%s 는 슬라이드 ContentType 이 아니다", name)
		}
		out = append(out, Part{
			Name: name,
			Ref:  "pptx/slide[" + strconv.Itoa(i+1) + "]",
			Root: "sld",
		})
	}
	if len(out) == 0 {
		return nil, ErrUnsupportedFormat
	}
	return out, nil
}
```

- [ ] **Step 6: GREEN 확인**

Run: `/opt/homebrew/bin/go test ./internal/parts/ -v`
Expected: 4개 PASS

`TestPlanOrdersSlidesByPresentation` 이 실패하면 `deck-a.pptx` 의 `sldIdLst` 와 rels 를 직접 덤프해서 확인할 것. 테스트를 느슨하게 하지 말 것.

- [ ] **Step 7: 커밋**

```bash
git add internal/parts internal/testutil
git commit -m "feat: parts.Plan — 파트 지도와 pptx 슬라이드 순서"
```

---

## Task 4: `parts.Document` — 지연 스캔과 조회

**Files:**
- Create: `internal/parts/document.go`
- Test: `internal/parts/document_test.go`

**Interfaces:**
- Consumes: Task 2 의 `xmlscan.Scan(src, rootAlias)`, Task 3 의 `Plan`/`Part`
- Produces:
  - `parts.Open(p *opc.Package) (*Document, error)`
  - `(*Document).Format() string`
  - `(*Document).Parts() []Part` — 계획 순서 사본
  - `(*Document).Tree(part string) (*xmlscan.Tree, error)` — **지연** 스캔 + 캐시
  - `(*Document).Lookup(part, path string) (xmlscan.Node, bool)`
  - `(*Document).Resolve(sel string) (string, bool)` — 논리 참조 먼저, 없으면 물리 파트명 정확 일치
  - `(*Document).Select(sels []string) ([]Part, error)` — `--part` 선택자 해석. 빈 슬라이스면 계획 전체
  - `(*Document).Loaded() []string` — 실제로 스캔한 파트 (지연 검증용)

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/parts/document_test.go`:

```go
package parts_test

import (
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/parts"
)

func TestDocumentLazyScan(t *testing.T) {
	d, err := parts.Open(openReal(t, "deck-a.pptx"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := len(d.Loaded()); got != 0 {
		t.Fatalf("열자마자 %d개 파트가 스캔됐다 — 지연이 아니다", got)
	}

	name := d.Parts()[1].Name
	if _, err := d.Tree(name); err != nil {
		t.Fatalf("Tree: %v", err)
	}
	loaded := d.Loaded()
	if len(loaded) != 1 || loaded[0] != name {
		t.Fatalf("스캔된 파트 %v, want [%s]", loaded, name)
	}

	// 같은 파트를 다시 요청해도 늘지 않는다 (캐시)
	if _, err := d.Tree(name); err != nil {
		t.Fatalf("Tree 재호출: %v", err)
	}
	if got := len(d.Loaded()); got != 1 {
		t.Fatalf("캐시가 안 된다: %d", got)
	}
}

func TestDocumentResolveLogicalAndPhysical(t *testing.T) {
	d, err := parts.Open(openReal(t, "deck-a.pptx"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := d.Parts()[2].Name

	got, ok := d.Resolve("pptx/slide[3]")
	if !ok || got != want {
		t.Fatalf("논리 해석 = %q,%v, want %q", got, ok, want)
	}
	got, ok = d.Resolve(want)
	if !ok || got != want {
		t.Fatalf("물리 해석 = %q,%v, want %q", got, ok, want)
	}
	if _, ok := d.Resolve("pptx/slide[99]"); ok {
		t.Error("없는 논리 참조가 풀렸다")
	}
	if _, ok := d.Resolve("ppt/slides/slide99.xml"); ok {
		t.Error("없는 물리 파트가 풀렸다")
	}
}

func TestDocumentSelect(t *testing.T) {
	d, err := parts.Open(openReal(t, "deck-a.pptx"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	all, err := d.Select(nil)
	if err != nil {
		t.Fatalf("Select(nil): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("선택자 없음 → %d개, want 3", len(all))
	}

	// 논리 참조는 정확 일치 — [ 가 glob 문자 클래스로 오독되면 안 된다
	one, err := d.Select([]string{"pptx/slide[2]"})
	if err != nil {
		t.Fatalf("Select 논리: %v", err)
	}
	if len(one) != 1 || one[0].Ref != "pptx/slide[2]" {
		t.Fatalf("논리 선택 = %+v", one)
	}

	// 물리 glob
	globbed, err := d.Select([]string{"ppt/slides/*"})
	if err != nil {
		t.Fatalf("Select glob: %v", err)
	}
	if len(globbed) != 3 {
		t.Fatalf("glob → %d개, want 3", len(globbed))
	}

	// 아무것도 못 고르는 선택자는 거절한다 — 조용한 빈 덤프는 오타를 숨긴다
	if _, err := d.Select([]string{"ppt/nope/*"}); err == nil {
		t.Error("빈 선택자가 거절되지 않았다")
	}
}

func TestDocumentLookup(t *testing.T) {
	d, err := parts.Open(openReal(t, "deck-a.pptx"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	name := d.Parts()[0].Name
	if _, ok := d.Lookup(name, "sld"); !ok {
		t.Error("루트 노드 sld 를 못 찾았다")
	}
	if _, ok := d.Lookup(name, "없는/경로[1]"); ok {
		t.Error("없는 경로가 찾아졌다")
	}
	if _, ok := d.Lookup("ppt/slides/slide99.xml", "sld"); ok {
		t.Error("없는 파트에서 노드가 찾아졌다")
	}
}
```

- [ ] **Step 2: RED 확인**

Run: `/opt/homebrew/bin/go test ./internal/parts/ -run TestDocument -v`
Expected: 컴파일 실패 — `undefined: parts.Open`

- [ ] **Step 3: 구현**

`internal/parts/document.go`:

```go
package parts

import (
	"fmt"
	"path"
	"sort"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// Document 는 파트 지도와 지연 스캔 캐시를 묶은 것이다.
// 파트를 가로지르는 조회는 전부 여기를 지난다.
type Document struct {
	pkg    *opc.Package
	format string
	plan   []Part
	byName map[string]Part
	byRef  map[string]Part
	trees  map[string]*xmlscan.Tree
	order  []string // 스캔한 순서 — Loaded() 의 결정성을 위해
}

func Open(p *opc.Package) (*Document, error) {
	format, plan, err := Plan(p)
	if err != nil {
		return nil, err
	}
	d := &Document{
		pkg:    p,
		format: format,
		plan:   plan,
		byName: make(map[string]Part, len(plan)),
		byRef:  make(map[string]Part, len(plan)),
		trees:  make(map[string]*xmlscan.Tree),
	}
	for _, pt := range plan {
		d.byName[pt.Name] = pt
		if pt.Ref != "" {
			d.byRef[pt.Ref] = pt
		}
	}
	return d, nil
}

func (d *Document) Format() string { return d.format }

func (d *Document) Parts() []Part {
	out := make([]Part, len(d.plan))
	copy(out, d.plan)
	return out
}

// Tree 는 파트를 스캔한다. 처음 요청될 때만 압축을 풀고, 결과는 캐시된다.
// 50장 덱에서 3장만 고치면 3장만 풀린다.
func (d *Document) Tree(name string) (*xmlscan.Tree, error) {
	if t, ok := d.trees[name]; ok {
		return t, nil
	}
	pt, ok := d.byName[name]
	if !ok {
		return nil, fmt.Errorf("스캔 대상 파트가 아니다: %s", name)
	}
	content, err := d.pkg.Part(name)
	if err != nil {
		return nil, err
	}
	t, err := xmlscan.Scan(content, pt.Root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	d.trees[name] = t
	d.order = append(d.order, name)
	return t, nil
}

// Loaded 는 실제로 스캔된 파트를 스캔 순서로 돌려준다. 지연 로딩 검증용이다.
func (d *Document) Loaded() []string {
	out := make([]string, len(d.order))
	copy(out, d.order)
	return out
}

func (d *Document) Lookup(name, nodePath string) (xmlscan.Node, bool) {
	t, err := d.Tree(name)
	if err != nil {
		return xmlscan.Node{}, false
	}
	return t.Lookup(nodePath)
}

// Resolve 는 선택자 하나를 물리 파트명으로 푼다.
// 논리 참조를 먼저 정확 일치로 보고, 없으면 물리 파트명으로 본다 —
// pptx/slide[3] 의 [ 가 glob 문자 클래스로 오독되지 않도록 이 순서가 필요하다.
func (d *Document) Resolve(sel string) (string, bool) {
	if pt, ok := d.byRef[sel]; ok {
		return pt.Name, true
	}
	if pt, ok := d.byName[sel]; ok {
		return pt.Name, true
	}
	return "", false
}

// Select 는 --part 선택자들을 계획의 부분집합으로 푼다.
// 선택자가 없으면 계획 전체다. 합집합이며 계획 순서를 유지한다.
// 어느 선택자도 파트를 하나도 못 고르면 거절한다 — 조용한 빈 덤프는 오타를 숨긴다.
func (d *Document) Select(sels []string) ([]Part, error) {
	if len(sels) == 0 {
		return d.Parts(), nil
	}

	picked := make(map[string]bool)
	for _, sel := range sels {
		before := len(picked)

		if name, ok := d.Resolve(sel); ok {
			picked[name] = true
		} else {
			for _, pt := range d.plan {
				ok, err := path.Match(sel, pt.Name)
				if err != nil {
					return nil, fmt.Errorf("선택자 %q 가 올바른 glob 이 아니다: %w", sel, err)
				}
				if ok {
					picked[pt.Name] = true
				}
			}
		}

		if len(picked) == before {
			return nil, fmt.Errorf("선택자 %q 가 아무 파트도 고르지 못했다", sel)
		}
	}

	out := make([]Part, 0, len(picked))
	for _, pt := range d.plan { // 맵이 아니라 계획 순서로 — 결정성
		if picked[pt.Name] {
			out = append(out, pt)
		}
	}
	_ = sort.SearchStrings // (정렬은 계획 순서가 대신한다)
	return out, nil
}
```

`sort` 를 실제로 안 쓰면 import 와 마지막 `_ = sort.SearchStrings` 줄을 지운다. 계획 순서가 결정성을 주므로 정렬은 필요 없다.

- [ ] **Step 4: GREEN 확인**

Run: `/opt/homebrew/bin/go test ./internal/parts/ -v`
Expected: 8개 PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/parts
git commit -m "feat: parts.Document — 지연 스캔과 파트 조회"
```

---

## Task 5: 덤프를 파트별로

**Files:**
- Modify: `internal/dump/dump.go` — 전면 교체
- Modify: `internal/dump/dump_test.go`
- Modify: `internal/xmlscan/node.go` — `Node.Part` 필드 추가
- Modify: `internal/xmlscan/scan.go` — `Scan` 이 `Part` 를 채우지 **않는다**(파트를 모른다). `parts.Document.Tree` 가 채운다

**Interfaces:**
- Consumes: `parts.Document`, `parts.Part`
- Produces:
  - `xmlscan.Node.Part string` — JSON 에는 싣지 않는다 (`json:"-"`)
  - `dump.Doc{Format, Hash string; Parts, Scanned []string}`
  - `dump.ScannedPart{Part, Ref, Root string; Nodes []xmlscan.Node}`
  - `dump.Dump{Doc Doc; ScannedParts []ScannedPart}`
  - `dump.Build(d *parts.Document, sels []string) (*Dump, error)`
  - `dump.Marshal(*Dump) ([]byte, error)`
  - **`const ScannedPart = "word/document.xml"` 는 이 태스크에서 남겨둔다.** `dump` 는 더 이상 쓰지 않지만 `patch`·`tmpl` 이 아직 참조한다. Task 7 이 마지막 사용처를 걷어낸 뒤 지운다 — 그래야 **모든 커밋이 빌드되고 테스트가 통과**하고, 중간에 회귀가 생겼을 때 어느 태스크 탓인지 가릴 수 있다

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/dump/dump_test.go` 를 통째로 바꾼다:

```go
package dump_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

func docOf(t *testing.T, src []byte) *parts.Document {
	t.Helper()
	p, err := opc.OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	return d
}

func realDoc(t *testing.T, name string) *parts.Document {
	t.Helper()
	p, err := opc.Open(filepath.Join("..", "..", "testdata", "real", name))
	if err != nil {
		t.Fatalf("Open %s: %v", name, err)
	}
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	return d
}

// I3 결정성
func TestDumpIsDeterministic(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문", "꼬리말"})
	run := func() []byte {
		d, err := dump.Build(docOf(t, src), nil)
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
			t.Fatalf("반복 %d 에서 덤프가 달라졌다", i)
		}
	}
}

func TestDumpDocxShape(t *testing.T) {
	d, err := dump.Build(docOf(t, testutil.MinimalDocx([]string{"제목"})), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if d.Doc.Format != "docx" {
		t.Fatalf("Format = %q", d.Doc.Format)
	}
	if len(d.Doc.Parts) != 3 {
		t.Fatalf("Parts %d개, want 3", len(d.Doc.Parts))
	}
	if len(d.Doc.Scanned) != 1 || d.Doc.Scanned[0] != "word/document.xml" {
		t.Fatalf("Scanned = %v", d.Doc.Scanned)
	}
	if len(d.ScannedParts) != 1 {
		t.Fatalf("ScannedParts %d개, want 1", len(d.ScannedParts))
	}
	sp := d.ScannedParts[0]
	if sp.Ref != "docx/document" || sp.Root != "document" {
		t.Fatalf("%+v", sp)
	}
	if len(sp.Nodes) == 0 {
		t.Fatal("노드가 비었다")
	}
	if sp.Nodes[0].Path != "document" {
		t.Fatalf("첫 노드 Path = %q, want document", sp.Nodes[0].Path)
	}
}

func TestDumpPptxHasAllSlides(t *testing.T) {
	d, err := dump.Build(realDoc(t, "deck-a.pptx"), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if d.Doc.Format != "pptx" {
		t.Fatalf("Format = %q", d.Doc.Format)
	}
	if len(d.ScannedParts) != 3 {
		t.Fatalf("슬라이드 %d개, want 3", len(d.ScannedParts))
	}
	for i, want := range []string{"pptx/slide[1]", "pptx/slide[2]", "pptx/slide[3]"} {
		if d.ScannedParts[i].Ref != want {
			t.Errorf("[%d].Ref = %q, want %q", i, d.ScannedParts[i].Ref, want)
		}
	}
}

func TestDumpSelectorNarrows(t *testing.T) {
	d, err := dump.Build(realDoc(t, "deck-a.pptx"), []string{"pptx/slide[2]"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(d.ScannedParts) != 1 || d.ScannedParts[0].Ref != "pptx/slide[2]" {
		t.Fatalf("선택자가 안 먹었다: %+v", d.Doc.Scanned)
	}
	// doc.parts 는 컨테이너 전체 그대로다 — 선택자가 좁히는 건 스캔 대상뿐
	if len(d.Doc.Parts) < 10 {
		t.Fatalf("doc.parts 가 선택자에 같이 좁혀졌다: %d개", len(d.Doc.Parts))
	}
}

// 노드 JSON 에 part 가 실리면 안 된다 — 묶음 머리에 이미 있다
func TestNodeJSONOmitsPart(t *testing.T) {
	d, err := dump.Build(docOf(t, testutil.MinimalDocx([]string{"제목"})), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	b, err := json.Marshal(d.ScannedParts[0].Nodes[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if bytes.Contains(b, []byte(`"part"`)) {
		t.Fatalf("노드 JSON 에 part 가 실렸다: %s", b)
	}
	// Go 쪽에는 채워져 있어야 한다
	if d.ScannedParts[0].Nodes[0].Part != "word/document.xml" {
		t.Fatalf("Node.Part = %q", d.ScannedParts[0].Nodes[0].Part)
	}
}
```

- [ ] **Step 2: RED 확인**

Run: `/opt/homebrew/bin/go test ./internal/dump/ -v`
Expected: 컴파일 실패 — `dump.Build` 의 인자 수가 맞지 않는다

- [ ] **Step 3: `Node.Part` 추가**

`internal/xmlscan/node.go` 의 `Node` 에 필드를 더한다:

```go
type Node struct {
	// Part 는 이 노드가 속한 파트의 물리 경로다.
	// Scan 은 파트를 모르므로 채우지 않는다 — parts.Document.Tree 가 채운다.
	// JSON 에는 싣지 않는다: 덤프가 파트로 묶여 나가므로 묶음 머리에 이미 있다.
	Part string `json:"-"`

	Path  string `json:"path"`
	Type  string `json:"type"`
	Span  Span   `json:"span"`
	Inner Span   `json:"inner"`
	Attrs []Attr `json:"attrs,omitempty"`
	Text  string `json:"text,omitempty"`
}
```

- [ ] **Step 4: `Document.Tree` 가 `Part` 를 채우게 한다**

`internal/parts/document.go` 의 `Tree` 에서 스캔 직후 한 줄 더한다:

```go
	t, err := xmlscan.Scan(content, pt.Root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	for i := range t.Nodes {
		t.Nodes[i].Part = name
	}
```

- [ ] **Step 5: `dump` 전면 교체**

`internal/dump/dump.go`:

```go
// Package dump 는 문서를 에이전트가 읽을 JSON 으로 내보낸다.
package dump

import (
	"encoding/json"

	"github.com/SONGYEONGSIN/pantograph/internal/parts"
	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

type Doc struct {
	Format  string   `json:"format"`
	Hash    string   `json:"hash"`
	Parts   []string `json:"parts"`   // 컨테이너의 전 엔트리
	Scanned []string `json:"scanned"` // 그중 파싱한 것
}

// ScannedPart 는 파싱한 파트 하나와 그 노드들이다.
type ScannedPart struct {
	Part  string         `json:"part"`
	Ref   string         `json:"ref,omitempty"`
	Root  string         `json:"root"`
	Nodes []xmlscan.Node `json:"nodes"`
}

type Dump struct {
	Doc          Doc           `json:"doc"`
	ScannedParts []ScannedPart `json:"scannedParts"`
}

// Build 는 문서를 덤프 구조로 바꾼다.
// sels 가 비면 계획의 본문 파트를 전부 스캔한다.
func Build(d *parts.Document, sels []string) (*Dump, error) {
	selected, err := d.Select(sels)
	if err != nil {
		return nil, err
	}

	out := &Dump{
		Doc: Doc{
			Format:  d.Format(),
			Hash:    d.Hash(),
			Parts:   d.Names(),
			Scanned: make([]string, 0, len(selected)),
		},
		ScannedParts: make([]ScannedPart, 0, len(selected)),
	}
	for _, pt := range selected {
		tree, err := d.Tree(pt.Name)
		if err != nil {
			return nil, err
		}
		out.Doc.Scanned = append(out.Doc.Scanned, pt.Name)
		out.ScannedParts = append(out.ScannedParts, ScannedPart{
			Part:  pt.Name,
			Ref:   pt.Ref,
			Root:  pt.Root,
			Nodes: tree.Nodes,
		})
	}
	return out, nil
}

// Marshal 은 덤프를 JSON 으로 직렬화한다.
// 모든 필드가 슬라이스·문자열이라 맵 순회 순서가 개입하지 않는다 (I3).
func Marshal(d *Dump) ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}
```

- [ ] **Step 6: `Document` 에 `Hash`·`Names` 추가**

`internal/parts/document.go` 에 두 메서드를 더한다 — `dump` 가 `opc.Package` 를 직접 잡지 않게 하기 위해서다:

```go
// Hash 는 컨테이너 전체의 sha256 이다.
func (d *Document) Hash() string { return d.pkg.Hash }

// Names 는 컨테이너의 전 엔트리다 (스캔 대상만이 아니다).
func (d *Document) Names() []string { return d.pkg.Names() }
```

- [ ] **Step 7: GREEN 확인**

Run: `/opt/homebrew/bin/go test ./... -count=1`
Expected: **전 패키지 PASS.** `ScannedPart` 상수를 남겨뒀으므로 `patch`·`tmpl` 은 그대로 컴파일된다. 이 태스크가 초록을 깨면 안 된다 — 깨진다면 상수를 지웠거나 `Node.Part` 추가가 무언가를 부순 것이다.

`dump` 안에서 상수가 더 이상 쓰이지 않는지 확인한다:

```bash
grep -n 'ScannedPart' internal/dump/dump.go
```

`const ScannedPart` 선언 한 줄만 남아야 한다. `Build` 안에서 참조하면 교체가 덜 된 것이다.

- [ ] **Step 8: 커밋**

```bash
git add internal/dump internal/parts internal/xmlscan
git commit -m "feat: 덤프를 파트별 노드 집합으로"
```

---

## Task 6: 패치를 파트 인식으로

**Files:**
- Modify: `internal/patch/patch.go` — `Op.Part` 추가
- Modify: `internal/patch/apply.go` — 전면 개편
- Modify: `internal/patch/apply_test.go`
- Modify: `cmd/panto/cmd_dump.go` — `--part` 플래그
- Commit: `testdata/real/deck-a.pptx` (작업 트리에 이미 있다)

**Interfaces:**
- Consumes: `parts.Document`, `parts.ErrUnsupportedFormat`
- Produces:
  - `patch.Op{Op, Part, Path, Text, XML string}` — `Part` 는 물리 또는 논리
  - `patch.Apply(p *opc.Package, pt Patch) ([]Error, error)` — 시그니처 유지
  - 새 `Reason`: `part_not_found`, `ref_not_found`, `part_not_scannable`, `unsupported_format`

- [ ] **Step 1: 실패하는 테스트 추가**

`internal/patch/apply_test.go` 에 더한다 (기존 테스트는 `Part: "word/document.xml"` 를 명시하도록 갱신):

```go
func realPkg(t *testing.T, name string) *opc.Package {
	t.Helper()
	p, err := opc.Open(filepath.Join("..", "..", "testdata", "real", name))
	if err != nil {
		t.Fatalf("Open %s: %v", name, err)
	}
	return p
}

func slideText(t *testing.T, p *opc.Package, part string) string {
	t.Helper()
	b, err := p.Part(part)
	if err != nil {
		t.Fatalf("Part %s: %v", part, err)
	}
	return string(b)
}

func TestApplyAcrossParts(t *testing.T) {
	p := realPkg(t, "deck-a.pptx")
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	s1, s2 := d.Parts()[0].Name, d.Parts()[1].Name

	// 각 슬라이드의 첫 a:t 노드를 찾는다
	find := func(part string) string {
		tr, err := d.Tree(part)
		if err != nil {
			t.Fatalf("Tree: %v", err)
		}
		for _, n := range tr.Nodes {
			if n.Type == "t" && n.Text != "" {
				return n.Path
			}
		}
		t.Fatalf("%s 에 텍스트 노드가 없다", part)
		return ""
	}
	p1, p2 := find(s1), find(s2)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "setText", Part: s1, Path: p1, Text: "첫째 바뀜"},
			{Op: "setText", Part: "pptx/slide[2]", Path: p2, Text: "둘째 바뀜"},
		},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("Apply: err=%v errs=%+v", err, errs)
	}
	if !strings.Contains(slideText(t, p, s1), "첫째 바뀜") {
		t.Error("슬라이드 1 이 안 바뀌었다")
	}
	if !strings.Contains(slideText(t, p, s2), "둘째 바뀜") {
		t.Error("슬라이드 2 가 논리 참조로 안 바뀌었다")
	}
}

func TestApplyAtomicAcrossParts(t *testing.T) {
	p := realPkg(t, "deck-a.pptx")
	before, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	s1 := d.Parts()[0].Name
	tr, _ := d.Tree(s1)
	var valid string
	for _, n := range tr.Nodes {
		if n.Type == "t" && n.Text != "" {
			valid = n.Path
			break
		}
	}

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "setText", Part: s1, Path: valid, Text: "유효"},
			{Op: "setText", Part: "pptx/slide[2]", Path: "sld/없는[99]", Text: "무효"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "path_not_found" {
		t.Fatalf("에러가 부정확하다: %+v", errs)
	}
	after, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("한 파트가 무효인데 다른 파트가 바뀌었다 — 원자성 위반")
	}
}

func TestPartResolutionErrors(t *testing.T) {
	p := realPkg(t, "deck-a.pptx")
	cases := []struct{ part, reason string }{
		{"ppt/slides/slide99.xml", "part_not_found"},
		{"pptx/slide[99]", "ref_not_found"},
		{"ppt/theme/theme1.xml", "part_not_scannable"},
	}
	for _, c := range cases {
		errs, err := patch.Apply(p, patch.Patch{
			Hash: p.Hash,
			Ops:  []patch.Op{{Op: "setText", Part: c.part, Path: "sld", Text: "x"}},
		})
		if err != nil {
			t.Fatalf("%s: Apply: %v", c.part, err)
		}
		if len(errs) != 1 || errs[0].Reason != c.reason {
			t.Errorf("%s → %+v, want reason %s", c.part, errs, c.reason)
		}
	}
}

func TestOverlapIsPerPart(t *testing.T) {
	p := realPkg(t, "deck-a.pptx")
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	// 두 슬라이드에서 같은 경로(구조가 같으므로 오프셋도 겹칠 수 있다)를 동시에 고친다
	s1, s2 := d.Parts()[0].Name, d.Parts()[1].Name
	find := func(part string) string {
		tr, _ := d.Tree(part)
		for _, n := range tr.Nodes {
			if n.Type == "t" && n.Text != "" {
				return n.Path
			}
		}
		return ""
	}
	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "setText", Part: s1, Path: find(s1), Text: "A"},
			{Op: "setText", Part: s2, Path: find(s2), Text: "B"},
		},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("다른 파트인데 겹침으로 거절됐다: err=%v errs=%+v", err, errs)
	}
}

func TestLazyPartLoading(t *testing.T) {
	p := realPkg(t, "deck-a.pptx")
	d, err := parts.Open(p)
	if err != nil {
		t.Fatalf("parts.Open: %v", err)
	}
	target := d.Parts()[1].Name
	tr, _ := d.Tree(target)
	var path string
	for _, n := range tr.Nodes {
		if n.Type == "t" && n.Text != "" {
			path = n.Path
			break
		}
	}

	// Apply 는 자기 Document 를 새로 연다. 그 안에서 몇 개를 스캔하는지 본다.
	loaded := patch.PartsLoadedBy(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Part: target, Path: path, Text: "X"}},
	})
	if len(loaded) != 1 || loaded[0] != target {
		t.Fatalf("스캔된 파트 %v — 1개(%s)만 스캔해야 한다", loaded, target)
	}
}
```

- [ ] **Step 2: RED 확인**

Run: `/opt/homebrew/bin/go test ./internal/patch/ -v`
Expected: 컴파일 실패 — `patch.Op` 에 `Part` 없음, `patch.PartsLoadedBy` 없음

- [ ] **Step 3: `Op.Part` 추가**

`internal/patch/patch.go`:

```go
// Op 는 패치 연산 하나다.
// Part 는 물리 파트 경로("ppt/slides/slide1.xml") 또는 논리 참조("pptx/slide[1]")다.
// 비어 있으면 본문 파트가 하나인 문서에 한해 그것으로 간주한다 — docx 하위호환.
type Op struct {
	Op   string `json:"op"`
	Part string `json:"part,omitempty"`
	Path string `json:"path"`
	Text string `json:"text,omitempty"`
	XML  string `json:"xml,omitempty"`
}
```

- [ ] **Step 4: `Apply` 개편**

`internal/patch/apply.go` 의 `Apply` 를 아래 구조로 바꾼다. 스플라이스 생성·거절 규칙(`type_mismatch`·`whitespace_needs_preserve`·`self_closing_target`·`duplicate_path`·`invalid_xml`)은 **한 글자도 바꾸지 않는다** — 파트별로 나뉘어 돌 뿐이다.

```go
// Apply 는 패치를 적용한다. 파트가 여럿이어도 전부 적용되거나 전무다.
func Apply(p *opc.Package, pt Patch) ([]Error, error) {
	if pt.Hash != "" && pt.Hash != p.Hash {
		return []Error{{Reason: "hash_mismatch",
			Detail: fmt.Sprintf("패치 hash=%s, 문서 hash=%s", pt.Hash, p.Hash)}}, nil
	}

	doc, err := parts.Open(p)
	if err != nil {
		if errors.Is(err, parts.ErrUnsupportedFormat) {
			return []Error{{Reason: "unsupported_format", Detail: err.Error()}}, nil
		}
		return nil, err
	}

	// 1) op 을 파트별로 가른다. 파트 해석 실패는 여기서 모은다.
	byPart := map[string][]Op{}
	var errs []Error
	for _, op := range pt.Ops {
		name, e := resolvePart(doc, op)
		if e != nil {
			errs = append(errs, *e)
			continue
		}
		byPart[name] = append(byPart[name], op)
	}
	if len(errs) > 0 {
		return errs, nil
	}
	if len(byPart) == 0 {
		return nil, nil // 빈 패치 — 아무것도 건드리지 않는다 (I1)
	}

	// 2) 파트별로 검증하고 버퍼를 만든다. 아직 아무것도 쓰지 않는다.
	//    맵이 아니라 계획 순서로 돌아 결정성을 지킨다.
	type pending struct {
		name string
		out  []byte
	}
	var buffers []pending
	for _, part := range doc.Parts() {
		ops, ok := byPart[part.Name]
		if !ok {
			continue
		}
		tree, err := doc.Tree(part.Name)
		if err != nil {
			return nil, err
		}
		out, es := spliceOne(tree, part, ops)
		if len(es) > 0 {
			return es, nil
		}
		buffers = append(buffers, pending{name: part.Name, out: out})
	}

	// 3) 모든 버퍼를 검증한 뒤에야 쓴다.
	//    파트 A 만 Replace 하고 B 가 깨지면 문서가 반쯤 바뀐다.
	for _, b := range buffers {
		if err := p.Replace(b.name, b.out); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// resolvePart 는 op 의 Part 를 물리 파트명으로 푼다.
// 에러가 파트와 경로 중 어디서 났는지 구분해서 말한다 — 에이전트의 재시도에 필요하다.
func resolvePart(doc *parts.Document, op Op) (string, *Error) {
	if op.Part == "" {
		ps := doc.Parts()
		if len(ps) == 1 {
			return ps[0].Name, nil
		}
		return "", &Error{Path: op.Path, Reason: "part_not_found",
			Detail: fmt.Sprintf("본문 파트가 %d개다 — op 에 part 를 명시해야 한다", len(ps))}
	}
	if name, ok := doc.Resolve(op.Part); ok {
		return name, nil
	}
	// 논리 참조 모양인가로 사유를 가른다
	if strings.Contains(op.Part, "[") && !strings.Contains(op.Part, "/") {
		// 물리 경로에는 / 가 있다
	}
	if isRefShaped(op.Part) {
		return "", &Error{Path: op.Path, Reason: "ref_not_found",
			Detail: fmt.Sprintf("논리 참조 %q 가 풀리지 않는다", op.Part)}
	}
	if doc.Exists(op.Part) {
		return "", &Error{Path: op.Path, Reason: "part_not_scannable",
			Detail: fmt.Sprintf("%s 는 컨테이너에 있으나 스캔 대상이 아니다", op.Part)}
	}
	return "", &Error{Path: op.Path, Reason: "part_not_found",
		Detail: fmt.Sprintf("파트 %q 가 문서에 없다", op.Part)}
}

// isRefShaped 는 선택자가 논리 참조 모양인지 본다 ("pptx/slide[3]", "docx/document").
func isRefShaped(s string) bool {
	return strings.HasPrefix(s, "pptx/") || strings.HasPrefix(s, "docx/")
}
```

`resolvePart` 안의 빈 `if strings.Contains(...)` 블록은 지운다 — 초안의 잔재다.

- [ ] **Step 4b: `spliceOne` 추출**

`spliceOne` 은 새로 쓰는 코드가 아니라 **기존 `Apply` 본문의 아래 구간을 잘라낸 것**이다.

```go
func spliceOne(tree *xmlscan.Tree, part parts.Part, ops []Op) ([]byte, []Error)
```

옮길 구간과 그 안에서 바뀌는 것:

| 기존 `Apply` 안의 부분 | `spliceOne` 에서 |
|---|---|
| `seen[op.Path]` 중복 검사 | 그대로. **파트별로 초기화**되므로 다른 파트의 같은 경로는 중복이 아니다 |
| `tree.Lookup(op.Path)` → `path_not_found` | 그대로 |
| `setText` 세 거절 규칙 (`type_mismatch`·`whitespace_needs_preserve`·`self_closing_target`) | 그대로 — **한 글자도 바꾸지 않는다** |
| `replaceRaw` 스플라이스 | 그대로 |
| `default:` → `unknown_op` | 그대로 |
| 정렬 + 겹침 검사 (`overlap`) | 그대로. 파트별로 도므로 다른 파트끼리는 비교되지 않는다 |
| `len(splices)==0` 빈 가드 | **제거** — 파트 선별을 `Apply` 가 이미 했으므로 여기 오면 op 이 있다 |
| 내림차순 스플라이스 적용 | 그대로 |
| `xmlscan.Scan(out)` 재검증 → `invalid_xml` | `xmlscan.Scan(out, part.Root)` — **루트 별칭을 넘긴다** |
| `blame(...)` | 그대로. 안의 재스캔도 `part.Root` 를 넘긴다 |
| `p.Replace(...)` | **제거** — `Apply` 가 모든 버퍼를 검증한 뒤에 한다 |

반환은 `(out []byte, nil)` 또는 `(nil, errs)` 다. 에러가 있으면 버퍼를 만들지 않는다.

`internal/patch/apply.go` 의 `dump` import 를 지운다 — `dump.ScannedPart` 상수가 사라졌고 이제 `parts` 를 쓴다.

`Error.Path` 는 문서 수준 오류(`hash_mismatch`·`unsupported_format`)에서 빈 문자열이다. 의도된 것이다 — 그 실패에는 가리킬 경로가 없다.

- [ ] **Step 5: `Document.Exists` 와 `patch.PartsLoadedBy` 추가**

`internal/parts/document.go`:

```go
// Exists 는 파트가 컨테이너에 있는지 본다 (스캔 대상 여부와 무관하다).
func (d *Document) Exists(name string) bool {
	for _, n := range d.pkg.Names() {
		if n == name {
			return true
		}
	}
	return false
}
```

`internal/patch/apply.go` 끝에 (테스트 전용이지만 프로덕션 코드에 둔다 — 지연 로딩은 계약이다):

```go
// PartsLoadedBy 는 이 패치를 적용할 때 실제로 스캔되는 파트를 돌려준다.
// 지연 로딩이 주석이 아니라 계약임을 테스트가 고정하기 위한 것이다.
// 패키지를 변경하지 않는다 — 검증만 하고 버린다.
func PartsLoadedBy(p *opc.Package, pt Patch) []string {
	doc, err := parts.Open(p)
	if err != nil {
		return nil
	}
	for _, op := range pt.Ops {
		name, e := resolvePart(doc, op)
		if e != nil {
			continue
		}
		if _, err := doc.Tree(name); err != nil {
			continue
		}
	}
	return doc.Loaded()
}
```

- [ ] **Step 6: `cmd/panto` 갱신**

`cmd/panto/cmd_dump.go` 에 `--part` 를 더한다 (반복 가능):

```go
func cmdDump(args []string) int {
	var in string
	var sels []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--part":
			i++
			if i >= len(args) {
				return die(exitInput, "--part 뒤에 선택자가 필요하다")
			}
			sels = append(sels, args[i])
		default:
			if in != "" {
				return die(exitInput, "입력 파일이 둘 이상이다: %s, %s", in, args[i])
			}
			in = args[i]
		}
	}
	if in == "" {
		return die(exitInput, "사용법: panto dump <in.docx|in.pptx> [--part <선택자>]")
	}

	p, code := openInput(in)
	if p == nil {
		return code
	}
	doc, err := parts.Open(p)
	if err != nil {
		return failFormat(in, err)
	}
	d, err := dump.Build(doc, sels)
	if err != nil {
		return fail(in, err)
	}
	if err := emit(d); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
```

`cmd/panto/main.go` 에 `failFormat` 을 더한다 — `ErrUnsupportedFormat` 과 `Select` 실패를 stdout JSON + 코드 1 로 낸다:

```go
// failFormat 은 포맷·선택자 오류를 입력 오류로 낸다.
// unsupported_container 와 같은 취급이다 — 입력 파일의 성질이지 도구의 고장이 아니다.
func failFormat(path string, err error) int {
	reason := "unsupported_format"
	if !errors.Is(err, parts.ErrUnsupportedFormat) {
		reason = "part_not_found"
	}
	if e := emit(patch.Result{OK: false, Errors: []patch.Error{
		{Path: path, Reason: reason, Detail: err.Error()},
	}}); e != nil {
		return die(exitInternal, "%v", e)
	}
	return exitInput
}
```

`main.go` 의 usage 배너에 `[--part <선택자>]` 를 더한다.

- [ ] **Step 7: GREEN 확인**

Run: `/opt/homebrew/bin/go test ./... -count=1`
Expected: 전 패키지 PASS. **실제 Word 픽스처 3종이 통과해야 한다** — 파트가 하나인 경우가 다중 파트의 특수 경우로 정확히 동작한다는 증거다.

- [ ] **Step 8: 손 검증**

```bash
/opt/homebrew/bin/go build -o /tmp/panto ./cmd/panto
/tmp/panto dump testdata/real/deck-a.pptx | python3 -c "
import json,sys
d=json.load(sys.stdin)
print('format:', d['doc']['format'])
print('스캔:', d['doc']['scanned'])
for sp in d['scannedParts']:
    print(f\"  {sp['ref']:16s} {sp['part']:26s} 노드 {len(sp['nodes'])}개\")
"
/tmp/panto dump testdata/real/deck-a.pptx --part 'pptx/slide[2]' | python3 -c "
import json,sys; d=json.load(sys.stdin); print('선택자 적용:', d['doc']['scanned'])
"
echo '{"ops":[]}' > /tmp/e.json
/tmp/panto apply testdata/real/deck-a.pptx -p /tmp/e.json -o /tmp/rt.pptx >/dev/null
cmp -s testdata/real/deck-a.pptx /tmp/rt.pptx && echo "I1 pptx 바이트 동일 ✓" || echo "다름 ✗"
```

- [ ] **Step 9: 커밋**

```bash
git add -A
git commit -m "feat: 패치를 파트 인식으로, pptx 픽스처 추가"
```

---

## Task 7: 템플릿을 파트 인식으로

**Files:**
- Modify: `internal/tmpl/schema.go` — `Key.Part` 추가
- Modify: `internal/tmpl/extract.go`, `internal/tmpl/fill.go`
- Modify: `internal/tmpl/tmpl_test.go`

**Interfaces:**
- Consumes: `parts.Document`, Task 6 의 `patch.Op.Part`
- Produces:
  - `tmpl.Key{Key, Part, Path string; Samples []string}`
  - `tmpl.Extract`/`Fill`/`Values` 시그니처 유지

- [ ] **Step 1: 실패하는 테스트 추가**

`internal/tmpl/tmpl_test.go` 에 더한다:

```go
func TestExtractRejectsPartSetMismatch(t *testing.T) {
	a, err := opc.OpenBytes(testutil.MinimalDocx([]string{"고정", "A"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	b, err := opc.Open(filepath.Join("..", "..", "testdata", "real", "deck-a.pptx"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, _, errs, err := tmpl.Extract([]*opc.Package{a, b}, []string{"a.docx", "deck.pptx"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "structure_mismatch" {
		t.Fatalf("포맷이 다른 문서가 거절되지 않았다: %+v", errs)
	}
}
```

기존 테스트의 `Key` 단언에 `Part` 를 더한다 — docx 는 전부 `"word/document.xml"` 이다.

- [ ] **Step 2: RED 확인**

Run: `/opt/homebrew/bin/go test ./internal/tmpl/ -v`
Expected: 컴파일 실패 — `tmpl.Key` 에 `Part` 없음

- [ ] **Step 3: `Key.Part` 추가**

`internal/tmpl/schema.go`:

```go
// Key 는 템플릿의 가변 자리 하나다.
type Key struct {
	Key     string   `json:"key"`
	Part    string   `json:"part"`
	Path    string   `json:"path"`
	Samples []string `json:"samples"`
}
```

- [ ] **Step 4: `Extract` 를 파트별로**

`internal/tmpl/extract.go` 의 흐름을 바꾼다:

```
1. 각 문서를 parts.Open
2. **파트 집합 비교** — 계획의 (Name, Ref, Root) 열이 완전히 일치해야 한다
     불일치 → structure_mismatch, 최초로 갈린 파트 보고
3. 파트별로:
     기존 구조 정렬(경로 열 비교) → diffMarkup → 가변부 판별
     키 번호는 **파트를 가로질러 이어진다** — k1, k2, … 전역 1-base
4. setText 패치를 만들어 베이스 사본에 적용 (op 마다 Part 를 채운다)
```

키 번호가 전역인 이유: 스키마의 데이터 파일이 `{"k1": "...", "k2": "..."}` 형태라 파트별로 번호가 겹치면 충돌한다.

`diffStructure`·`diffMarkup`·`stableAttrs` 는 **파트 하나에 대한 함수로 그대로 둔다**. 호출부가 파트별로 돌 뿐이다.

- [ ] **Step 5: `Fill`·`Values` 를 파트별로**

`internal/tmpl/fill.go` 의 `Values` 와 `Fill` 이 `k.Part` 를 써서 노드를 찾고, 패치 op 에 `Part` 를 채운다. `template_drift`·`missing_key` 판정은 그대로다.

- [ ] **Step 6: `dump.ScannedPart` 상수 제거**

`tmpl` 이 마지막 사용처였다. 이제 지운다:

```bash
grep -rn 'dump.ScannedPart\|ScannedPart =' --include='*.go' .
```

`internal/dump/dump.go` 의 `const ScannedPart = "word/document.xml"` 선언과 그 주석을 지운다. 남은 참조가 있으면 그것부터 걷어낸다. `dump.ScannedPart` **타입**(파트별 노드 묶음)은 남는다 — 이름이 같지만 다른 것이다.

- [ ] **Step 7: GREEN 확인**

Run: `/opt/homebrew/bin/go test ./... -count=1`
Expected: 전 패키지 PASS. docx 템플릿 테스트(I4a 포함)가 그대로 통과해야 한다.

```bash
grep -rn 'const ScannedPart' --include='*.go' . || echo "상수 제거됨 ✓"
```

- [ ] **Step 8: 커밋**

```bash
git add internal/tmpl internal/dump
git commit -m "feat: 템플릿을 파트 인식으로, ScannedPart 상수 제거"
```

---

## Task 8: pptx 실제 픽스처 불변식

**Files:**
- Create: `testdata/real/deck-b.pptx` (생성)
- Modify: `testdata/real/README.md`
- Test: `internal/opc/package_test.go`, `internal/tmpl/tmpl_test.go`

**Interfaces:**
- Consumes: Task 6·7 의 전부
- Produces: 없음 (검증 태스크)

- [ ] **Step 1: `deck-b.pptx` 생성**

`deck-a.pptx` 와 **같은 양식**(슬라이드 3장, 같은 레이아웃)이되 제목 텍스트만 다른 덱을 만든다. PowerPoint 를 AppleScript 로 구동하고 샌드박스 컨테이너에 저장한 뒤 복사해 온다.

```bash
osascript <<'EOS'
tell application "Microsoft PowerPoint"
  set p to make new presentation
  set L to custom layout 2 of slide master of p
  repeat with txt in {"표지 B", "둘째 장 B", "셋째 장 B"}
    set s to make new slide at end of p with properties {custom layout:L}
    set content of text range of text frame of shape 1 of s to (txt as text)
  end repeat
  save p in ((path to documents folder as text) & "deck-b.pptx")
end tell
EOS
osascript -e 'tell application "Microsoft PowerPoint" to close every presentation saving no'
pkill -9 -x "Microsoft PowerPoint" 2>/dev/null
cp "$HOME/Library/Containers/com.microsoft.Powerpoint/Data/Documents/deck-b.pptx" testdata/real/deck-b.pptx
```

주의: 여러 슬라이드를 한 osascript 안에서 만들면 `-1728` 이 날 수 있다. 나면 슬라이드마다 호출을 쪼갠다 (프로젝트 메모리 `word-fixture-generation` 참조).

**`deck-a` 와 파트 집합·슬라이드 수가 같아야 한다.** 다르면 `structure_mismatch` 로 거절되고 I4a 를 시험할 수 없다. 만든 뒤 확인:

```bash
for f in deck-a deck-b; do
  echo "$f: $(unzip -l testdata/real/$f.pptx | grep -c 'slides/slide[0-9]*\.xml') 슬라이드"
done
```

- [ ] **Step 2: I1 을 pptx 로 확장**

`internal/opc/package_test.go` 의 `TestIdentityReal` 이 이미 `testdata/real/*.docx` 를 glob 한다. **glob 을 `*` 로 넓혀** pptx 도 포함시킨다:

```go
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "real", "*.*"))
	// README.md 는 건너뛴다
```

`.md` 를 거르고 `.docx`·`.pptx` 만 왕복시킨다. `t.Fatal` on empty glob 은 그대로 둔다.

`internal/patch/apply_test.go` 의 `TestLocalityReal` 도 같이 넓힌다 — I2 는 pptx 에서 더 강한 주장이 된다(설계 §7). 50 장 중 1 장을 고쳤을 때 나머지의 압축 데이터가 그대로여야 한다. 지금 `paths[0]` 하나만 쓰는 것을 `.docx`·`.pptx` 각각 하나씩 돌게 바꾸고, pptx 쪽은 슬라이드 하나만 패치한 뒤 **나머지 슬라이드 파트의 압축 데이터가 동일한지** 단언한다.

- [ ] **Step 3: I4a 를 pptx 로**

`internal/tmpl/tmpl_test.go` 에 더한다:

```go
func TestPptxTemplateReversalReal(t *testing.T) {
	var ps []*opc.Package
	var names []string
	for _, n := range []string{"deck-a.pptx", "deck-b.pptx"} {
		p, err := opc.Open(filepath.Join("..", "..", "testdata", "real", n))
		if err != nil {
			t.Fatalf("Open %s: %v", n, err)
		}
		ps = append(ps, p)
		names = append(names, n)
	}

	tp, sch, errs, err := tmpl.Extract(ps, names)
	if err != nil || len(errs) != 0 {
		t.Fatalf("Extract: err=%v errs=%+v", err, errs)
	}
	if len(sch.Keys) == 0 {
		t.Fatal("가변부가 하나도 안 잡혔다")
	}
	// 키가 여러 파트에 걸쳐 있어야 다중 파트 템플릿을 시험한 것이다
	seen := map[string]bool{}
	for _, k := range sch.Keys {
		seen[k.Part] = true
	}
	if len(seen) < 2 {
		t.Fatalf("키가 파트 %d개에만 있다 — 다중 파트를 시험하지 못했다", len(seen))
	}

	vals, err := tmpl.Values(ps[0], sch)
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

	for _, k := range sch.Keys {
		want, err := ps[0].Part(k.Part)
		if err != nil {
			t.Fatalf("Part: %v", err)
		}
		got, err := filled.Part(k.Part)
		if err != nil {
			t.Fatalf("Part: %v", err)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("I4a 위반 — %s 가 원본과 다르다", k.Part)
		}
	}
}
```

- [ ] **Step 4: 전체 확인**

Run: `/opt/homebrew/bin/go test ./... -count=1 -v 2>&1 | grep -E '^(=== RUN|--- (PASS|FAIL)|ok|FAIL)' | tail -40`
Expected: 전부 PASS

**`TestPptxTemplateReversalReal` 이 실패하면 완화하지 말 것.** 어느 파트의 어느 바이트가 다른지 끝까지 읽고 보고한다 — pptx 에서 역추출이 무언가 잃는다는 뜻이고, 그건 설계가 들어야 할 발견이다.

- [ ] **Step 5: 픽스처 README 갱신**

`testdata/real/README.md` 에 pptx 두 파일의 용도를 더한다 — 어떤 불변식을 받치는지, 같은 양식이어야 하는 이유.

- [ ] **Step 6: 커밋**

```bash
git add testdata internal/opc internal/tmpl
git commit -m "test: pptx 실제 픽스처로 I1·I4a 검증"
```

---

## Task 9: 문서 정합

**Files:**
- Modify: `docs/superpowers/specs/2026-08-06-docx-roundtrip-design.md` — 경로 표기
- Modify: `docs/superpowers/specs/2026-08-08-multipart-design.md` — 구현과 어긋난 곳
- Modify: `README.md`

**Interfaces:** 없음 (문서 작업)

- [ ] **Step 1: 선행 스펙의 경로 표기 갱신**

`2026-08-06-docx-roundtrip-design.md` 의 모든 `word/body[1]/...` 를 `document/body[1]/...` 로 바꾸고, §6 경로 부여 규칙에 루트 별칭이 주입된다는 한 줄을 더한다. 선행 슬라이스의 결정을 지우지 말고 **바뀐 사실만 정정한다.**

```bash
grep -rn 'word/body' docs/ README.md
```

- [ ] **Step 2: 다중 파트 스펙을 구현과 대조**

`2026-08-08-multipart-design.md` 를 읽고 실제 구현과 다른 곳을 고친다. 특히:
- 덤프 JSON 예시가 실물과 같은지 (`panto dump` 결과와 대조)
- `Reason` 목록이 코드가 내는 것과 정확히 같은지 (`grep -rn 'Reason:' --include='*.go' .`)
- §10 픽스처 표의 상태를 실제로 갱신

- [ ] **Step 3: README 갱신**

- 쓰는 법에 `[--part <선택자>]` 와 pptx 를 더한다
- 지금 상태를 "docx + pptx" 로
- 알려진 한계: pptx 도 PowerPoint 16.x 산출물 2벌로만 검증됐다는 것, 슬라이드 추가·삭제·재정렬은 범위 밖이라는 것, xlsx 는 `sharedStrings` 간접 참조 때문에 별도 슬라이스라는 것

- [ ] **Step 4: 확인 후 커밋**

```bash
/opt/homebrew/bin/go test ./... -count=1 2>&1 | tail -10
git add docs README.md
git commit -m "docs: 다중 파트·pptx 를 스펙과 README 에 반영"
```

---

## 완료 기준

```bash
/opt/homebrew/bin/go vet ./...
/opt/homebrew/bin/go test ./... -count=1
```

- `go vet` 무출력, 전 패키지 PASS (실제 docx·pptx 픽스처 포함)
- `go.mod` 에 `require` 없음
- `grep -rn "func.*Marshal\|func.*Encode\|func.*String() string" internal/xmlscan/` 결과 없음
- `grep -rn 'ScannedPart' --include='*.go' .` 에 **상수는 없고** `dump.ScannedPart` 타입만 있음
- `panto dump deck-a.pptx --part 'pptx/slide[2]'` 가 슬라이드 하나만 낸다
- `panto apply deck-a.pptx -p 빈패치 -o out.pptx` 가 바이트 동일 (I1)
