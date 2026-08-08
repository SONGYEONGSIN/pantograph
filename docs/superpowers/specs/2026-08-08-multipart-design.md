# 다중 파트 일반화 + pptx 바인딩 (설계)

> 상위 설계: [`docs/2026-08-06-pantograph-design.md`](../../2026-08-06-pantograph-design.md)
> 선행 슬라이스: [`2026-08-06-docx-roundtrip-design.md`](2026-08-06-docx-roundtrip-design.md)
> 이 문서는 "본문 파트가 하나"라는 가정을 걷어내고 pptx 를 들이는 슬라이스를 확정한다.

## 1. 범위

| | 포함 | 근거 |
|---|---|---|
| 다중 파트 일반화 (타입·흐름·경로) | ✅ | pptx·xlsx 로 가는 유일한 관문 |
| pptx 읽기 (`dump`) | ✅ | 실제 다중 파트 문서가 설계를 강제한다 |
| pptx 쓰기 (`apply`, `tmpl`) | ✅ | 사용자 결정 |
| `internal/wml` → `internal/xmlscan` 개명 | ✅ | 스캐너는 원래 포맷 무관이었다 (§5) |
| xlsx | ❌ | `sharedStrings.xml` 간접 참조가 별도 주제다 (§12) |
| 노드 수준 논리 선택자 (`shape[title]`) | ❌ | 포맷 의미론이 코어로 샌다 (§3) |
| 렌더 · 픽셀 비교 | ❌ | 여전히 임계가 없다 |

## 2. 왜 지금인가

현재 `dump.ScannedPart` 는 문자열 **상수**다.

```go
// ScannedPart 는 이번 슬라이스가 파싱하는 유일한 파트다.
const ScannedPart = "word/document.xml"
```

docx 는 본문이 한 파트라 이 가정이 성립했다. pptx 는 성립하지 않는다 — 본문이 슬라이드마다 파트로 쪼개진다. 이 상수는 4 개 패키지 11 곳이 참조하고, 바뀌는 것은 이름이 아니라 **타입**이다: `Span` 은 "어느 파트의 바이트인지" 없이는 의미가 없다.

그래서 이것은 pptx 기능이 아니라 **코어 일반화**다. 지금 하면 docx 한 포맷만 고치면 되고, pptx 를 만든 뒤에 하면 두 포맷을 동시에 고쳐야 한다.

## 3. 주소 체계

### 노드는 `(파트, 경로)` 로 식별된다

```go
type Node struct {
    Part  string   // "ppt/slides/slide1.xml" — 물리 파트 경로
    Path  string   // 그 파트 안에서의 경로
    Span  Span     // 그 파트의 압축 해제 바이트 기준 [Start, End)
    Inner Span
    Attrs []Attr
    Text  string
}
```

`Path` 가 파트 안으로 스코프된다. 유일성은 `(Part, Path)` 쌍이 만든다. `Span` 은 순수 바이트 범위로 남고, 어느 파트인지는 노드가 안다.

**대안 기각**: `Span{Part, Start, End}` 로 범위마다 파트를 달면 모든 범위가 자기설명적이지만, 노드마다 파트 문자열이 두 번(`Span`·`Inner`) 반복돼 덤프가 배로 붇는다. 노드는 반드시 파트 하나에 속하므로 노드에 다는 것이 정확하다.

### 루트 별칭은 루트 요소의 로컬명이다

| 파트 | 루트 요소 | 경로 |
|---|---|---|
| `word/document.xml` | `w:document` | `document/body[1]/p[3]/r[1]/t[1]` |
| `ppt/slides/slide1.xml` | `p:sld` | `sld/cSld[1]/spTree[1]/sp[2]/…` |

규칙 하나: **루트 요소는 로컬명 그대로, 인덱스 없음**. 파트당 루트는 XML 법칙상 하나뿐이라 셀 형제가 없다. 그 아래부터는 기존 규칙("같은 부모 아래 같은 로컬명 1-base, 항상 표기")이 그대로다.

**이것은 기존 docx 경로를 바꾼다** — `word/body[1]/p[1]` → `document/body[1]/p[1]`. 선행 슬라이스의 `word` 는 루트 요소가 아니라 디렉토리 이름과 우연히 같은 문자열이었고, 읽는 사람이 파트 경로로 오해하게 만든다. 저장소 공개 이틀차이고 외부 소비자가 없는 지금이 바꾸는 비용이 가장 싼 시점이다. 포맷마다 별칭 규칙이 갈리는 것도 xlsx 가 들어오면 세 갈래가 된다.

### 논리 참조는 파트 층에만 있다

패치는 파트를 물리·논리 어느 쪽으로도 가리킬 수 있다.

```json
{"op":"setText", "part":"ppt/slides/slide1.xml", "path":"sld/cSld[1]/…", "text":"…"}
{"op":"setText", "part":"pptx/slide[1]",         "path":"sld/cSld[1]/…", "text":"…"}
```

덤프는 양쪽을 다 준다. 노드는 **파트로 묶여** 나간다.

```json
{
  "doc": {
    "format": "pptx",
    "hash": "sha256:…",
    "parts": ["[Content_Types].xml", "ppt/presentation.xml", "…"],
    "scanned": ["ppt/slides/slide1.xml", "ppt/slides/slide2.xml"]
  },
  "scannedParts": [
    { "part": "ppt/slides/slide1.xml", "ref": "pptx/slide[1]", "root": "sld",
      "nodes": [ { "path": "sld", "type": "sld", "span": {…}, "inner": {…} }, … ] }
  ]
}
```

`doc.parts` 는 컨테이너의 전 엔트리(기존과 같음), `doc.scanned` 는 그중 파싱한 것이다. 기존 `doc.scannedPart`(단수 문자열)를 `doc.scanned`(배열)가 대체하고 최상위 `nodes` 배열은 `scannedParts` 로 대체된다.

Go 쪽 이름도 바뀐다. §2 의 `const ScannedPart = "word/document.xml"` 은 없어지고, `scannedParts` 를 이루는 원소 타입이 `dump.ScannedPart` 가 된다 — 이름은 상수 시절과 같지만, 상수와 타입은 같은 패키지에서 식별자를 공유할 수 없다(Go 컴파일 에러 "redeclared in this block"). 그래서 구현 도중에는 상수가 남아 있는 동안 타입을 `dump.Part` 라는 임시 이름으로 뒀고, 상수를 참조하던 `patch`·`tmpl` 이 파트 인식으로 바뀌어 상수가 필요 없어진 뒤에야 상수를 지우고 타입이 `ScannedPart` 이름을 이어받았다. 끝난 상태는 하나뿐이다 — **상수는 없고, 타입 `dump.ScannedPart` 만 있다.**

**노드 JSON 에는 `part` 를 싣지 않는다** — 묶음 머리에 이미 있어 노드마다 반복하면 덤프가 붇는다. Go 의 `Node.Part` 는 채워지며, 파트 경계를 넘어 노드를 다룰 때(패치 검증, 템플릿 비교) 쓴다.

**노드 수준 의미론은 넣지 않는다.** 상위 설계가 약속한 `pptx/slide[3]/shape[title]/text` 는 어느 `sp` 가 title 플레이스홀더인지 알아야 하고(`<p:ph type="title"/>`), 그것은 포맷 의미론이 코어로 새어드는 지점이다. 구조 경로로 먼저 닿게 하고 편의 선택자는 나중에 얹는다.

논리 이름이 없는 파트(`ppt/theme/theme1.xml` 등)는 `ref` 가 비고 물리 경로로만 닿는다.

## 4. 파트 지도 — 포맷 지식의 유일한 거처

```go
// internal/parts
type Part struct {
    Name string  // "ppt/slides/slide1.xml"  물리
    Ref  string  // "pptx/slide[1]"          논리, 없으면 ""
    Root string  // 루트 별칭 ("document" / "sld")
}
func Plan(p *opc.Package) (format string, parts []Part, err error)
```

`Plan` 은 `[Content_Types].xml` 로 본문 파트를 고르고, pptx 면 `ppt/presentation.xml` + `ppt/_rels/presentation.xml.rels` 로 순서를 매긴다.

| ContentType | 스캔 | `Ref` |
|---|---|---|
| `…wordprocessingml.document.main+xml` | ✅ | `docx/document` |
| `…presentationml.slide+xml` | ✅ | `pptx/slide[N]` |
| 그 외 (theme·master·layout·styles·rels) | ❌ | — |

**본문 파트만 스캔한다.** 마스터·레이아웃까지 넣으면 빈 3 장 덱도 노드가 수백 개다(§11 실측). `--part` 는 **그 본문 파트 집합을 좁히는** 선택자다 — 계획에 없는 파트를 명시해도 스캔하지 않고 `part_not_scannable` 로 거절한다. 계획 밖 파트를 명시로 끌어들이는 것은 이 슬라이스의 범위가 아니다(§12).

**슬라이드 순서는 `presentation.xml` 이 정한다.** 파일명이 아니다. §11 의 실측에서는 PowerPoint 가 매 저장마다 정규화해 두 경우 모두 파일명과 순서가 일치했으나, 그렇다고 파일명을 믿을 이유는 없다 — 논리 참조를 제공하려면 어차피 그 파일을 읽어야 하고, 파일명 순서를 가정하면 어긋났을 때 알아낼 방법이 없다. 생산자를 하나만 관측했다는 한계도 선행 슬라이스와 같다.

`Plan` 의 출력 순서는 결정론적이어야 한다 (I3). pptx 는 `sldIdLst` 순서, docx 는 본문 파트 하나뿐이다.

`Plan` 의 **모든** 실패는 한 부류다 — 알려진 본문 파트가 없는 것, `[Content_Types].xml` 이 없거나 안 읽히는 것, `presentation.xml` 이 파싱되지 않는 것, `sldId` 의 rId 가 rels 에 없는 것, 슬라이드가 아닌 ContentType 을 가리키는 것 전부. 셋 다 `parts.ErrUnsupportedFormat` 을 `%w` 로 감싸고, CLI 는 **stdout JSON + 종료 코드 1**(`unsupported_format`)로 낸다 — `unsupported_container` 와 같은 취급이다. 입력 파일의 성질이지 도구의 고장이 아니다. 부류가 하나여야 `dump`·`apply`·`tmpl` 이 같은 파일에 같은 코드를 낸다.

**`Plan` 은 신뢰 경계다.** `presentation.xml`·rels·`[Content_Types].xml` 은 모두 입력 파일이 정하므로, "슬라이드로 선언됐다"는 그 파트가 실제로 있다는 근거도 한 번만 나온다는 근거도 못 된다. 계획은 `sldId` 하나당 항목 하나를 만들기 때문에, 검사 없이 통과시키면 이 함수가 입력 XML 을 작업량 배수로 바꾼다 — 같은 슬라이드를 N 번 가리키는 작은 `presentation.xml` 하나가 수백 MB 짜리 덤프가 된다(같은 노드 슬라이스를 N 번 직렬화). 그래서 `orderSlides` 는 **이름 중복**과 **컨테이너에 없는 파트**를 거절한다. 둘 다 거절이므로 폴백 금지 규칙과 맞는다.

### 조회 표면

파트가 여럿이 되면서 조회도 파트를 받는다.

```go
// internal/xmlscan
type Tree struct { Src []byte; Nodes []Node }   // 파트 하나의 스캔 결과
func Scan(src []byte, rootAlias string) (*Tree, error)
func (t *Tree) Lookup(path string) (Node, bool)  // 파트 안에서

// internal/parts  — 여러 파트를 묶은 것
type Document struct { … }
func (d *Document) Tree(part string) (*Tree, bool)
func (d *Document) Lookup(part, path string) (Node, bool)
func (d *Document) Resolve(ref string) (part string, ok bool)  // 논리 → 물리
```

`Tree` 는 파트 하나에 대한 기존 타입 그대로다. 파트를 가로지르는 것은 `Document` 가 맡는다.

## 5. 아키텍처 변경

```
internal/opc/      변경 없음 — 이미 포맷 무관이고 pptx 를 바이트 정확히 다룬다 (§11)
internal/parts/    신규 — 파트 지도. 포맷을 아는 유일한 곳
internal/xmlscan/  wml 에서 개명. Scan(src, rootAlias) 로 루트 별칭 주입
internal/dump/     ScannedPart 상수 제거. 파트별 노드 집합
internal/patch/    파트 인식 해석·겹침·적용
internal/tmpl/     파트 집합 정렬 후 파트별 비교
cmd/panto/         --part 선택자
```

`internal/wml` 의 개명 근거: 스캐너를 읽어보면 Word 특유의 것은 **루트 경로를 `"word"` 로 고정한 한 줄뿐**이고 나머지는 범용 XML 스캐너다. pptx 용 스캐너를 복제할 이유가 없다.

## 6. 데이터 흐름

### dump

```
opc.Open → parts.Plan
  → (선택자가 있으면 필터)
  → 파트별 Part() + xmlscan.Scan(src, part.Root)
  → 파트별 노드 집합을 담은 JSON
```

### apply

```
1. hash 대조                                   불일치 → 거부
2. parts.Plan
3. op 의 part 해석 (물리 or 논리) → 물리 파트
4. **필요한 파트만** Part() + Scan             ← 지연
5. 경로 해석 → (part, span)                    하나라도 실패 → 아무것도 적용 안 함
6. 겹침 검사 — 파트별로 분리                    다른 파트끼리는 겹칠 수 없다
7. 파트별 내림차순 스플라이스 → 버퍼
8. **모든 버퍼**를 재스캔                       하나라도 깨지면 Replace 없음
9. 파트별 Replace
```

**8 번이 원자성의 핵심이다.** 파트 A 는 성공하고 B 가 깨졌는데 A 만 `Replace` 하면 문서가 반쯤 바뀐다. 모든 버퍼를 검증한 뒤 한꺼번에 쓴다. 검증과 적용이 이미 갈라져 있어 구조는 그대로고 대상만 하나에서 여럿이 된다.

**4 번의 지연이 성능의 전부다.** 50 장 덱에서 3 장만 고치면 3 장만 압축 해제한다. `opc.Part()` 가 이미 파트별 캐시라 자연히 따라오지만, `apply` 가 계획의 모든 파트를 미리 스캔하지 않도록 주의한다. `dump` 는 반대로 기본 전부 스캔한다.

빈 패치 가드는 그대로다 — op 이 없으면 어떤 파트도 건드리지 않고 반환한다.

## 7. 불변식

| | 선행 슬라이스 | 이번 |
|---|---|---|
| **I1 항등** | 빈 패치 → 파일 전체 바이트 동일 | 그대로 |
| **I2 국소성** | 안 건드린 zip 엔트리 = 압축 데이터까지 동일 | **더 강해진다** — 안 건드린 *슬라이드*도 압축 데이터가 그대로다 |
| **I3 결정성** | dump 두 번 → 동일 바이트 | 그대로. `Plan` 순서가 결정론적이어야 성립 |
| **I4a/I4b** | 한 파트 비교 | 파트 집합을 먼저 맞추고 파트별로 비교 |

hash 잠금은 파일 전체 sha256 이라 어느 파트가 바뀌었든 잡는다. 변경 없음.

## 8. 에러

파트 해석이 새 실패 지점이다.

| Reason | 뜻 | 코드 |
|---|---|---|
| `part_not_found` | 물리 파트가 문서에 없다 | 1 |
| `ref_not_found` | 논리 참조가 안 풀린다 (`pptx/slide[99]`) | 1 |
| `part_not_scannable` | 스캔 대상이 아닌 파트를 경로로 가리켰다 | 1 |
| `unsupported_format` | `Plan` 이 실패했다 — 알려진 본문 파트 없음, Content_Types·presentation.xml 파싱 실패, sldId 의 rId 미해석, 중복·부재 슬라이드 | 1 |

기존 `path_not_found` 는 "파트는 찾았는데 그 안에 경로가 없다" 로 좁혀진다. **에러가 파트와 경로 중 어디서 틀렸는지 구분해서 말한다** — 에이전트가 재시도할 때 필요한 정보다.

**부류→(reason, 코드) 매핑은 CLI 에 한 곳만 둔다** (`cmd/panto/main.go` 의 `classify`). 명령마다 따로 매핑하면 같은 파일이 `dump` 에서는 코드 1, `tmpl` 에서는 코드 2 로 나간다. 마찬가지로 못 푼 선택자의 세 갈래 판정은 `parts.Document.Reject` 한 곳에만 둔다.

**거절 문구(`Error.Detail`)는 포맷 특정 요소 이름을 대지 않는다.** 텍스트 요소는 docx 가 `w:t`, pptx 가 `a:t` 다 — 문구에 한쪽을 박아넣으면 다른 포맷을 다룰 때마다 거짓을 말한다. 규칙을 말하고, 구체 예가 필요하면 손에 든 노드의 `Type`(로컬명)을 쓴다.

선행 슬라이스의 15 개 Reason 은 그대로 유지된다.

## 9. CLI 표면

```
panto dump  <in.docx|in.pptx> [--part <선택자>]
panto apply <in> -p patch.json -o out
panto tmpl extract <a> <b> [...] -o tmpl --schema schema.json
panto tmpl fill    <tmpl> --schema schema.json -d data.json -o out
```

`--part` 선택자는 물리 경로 glob 또는 논리 참조를 받으며, **`Plan` 이 고른 본문 파트 안에서만** 고른다.

```
--part 'ppt/slides/*'        물리 경로 glob
--part 'pptx/slide[3]'       논리 참조 — 정확 일치
```

**판정 순서**: 먼저 논리 참조로 정확 일치를 시도하고, 없으면 물리 경로 glob 으로 본다. `[` 를 포함하는 논리 참조가 glob 의 문자 클래스로 오독되지 않도록 이 순서가 필요하다.

`--part` 는 여러 번 줄 수 있고 합집합이다. 어느 선택자도 파트를 하나도 못 고르면 **거부한다** — 조용히 빈 덤프를 내면 사용자가 오타를 눈치채지 못한다. 거절 사유는 `apply` 의 `op.part` 해석과 같은 세 갈래(`part_not_found` / `ref_not_found` / `part_not_scannable`)이며, 판정은 `parts.Document.Reject` 한 곳에만 있다 — 같은 선택자에 두 명령이 다른 답을 내면 에이전트가 어느 쪽을 믿어야 할지 알 수 없다. 계획에 없는 파트(`ppt/theme/theme1.xml`, `ppt/slideMasters/*`)를 명시하면 `part_not_scannable` 이다.

인자가 없으면 `Plan` 이 고른 본문 파트를 전부 덤프한다. docx 는 지금과 동작이 같다.

## 10. 테스트

기존 불변식 테스트가 회귀 테스트가 된다. docx 경로 문자열이 바뀌므로 테스트도 갱신되지만 판정은 그대로고, **실제 Word 문서 2 벌이 리팩터가 아무것도 깨지 않았음을 증명한다.**

| 테스트 | 검증 |
|---|---|
| `TestPlanOrdersSlidesByPresentation` | `sldIdLst` 순서로 `Ref` 가 매겨진다 |
| `TestPlanIsDeterministic` | I3 — `Plan` 을 두 번 호출하면 같은 순서 |
| `TestApplyAcrossParts` | 두 슬라이드를 한 패치로 수정 → 둘 다 반영, 나머지 압축 데이터 동일 |
| `TestApplyAtomicAcrossParts` | 슬라이드 A 유효 + B 무효 → 아무것도 안 바뀜 |
| `TestOverlapIsPerPart` | 다른 파트의 같은 오프셋 구간은 겹침이 아니다 |
| `TestLazyPartLoading` | 1 장만 고칠 때 나머지 파트가 압축 해제되지 않는다 |
| `TestPptxIdentityReal` | I1 — 실제 pptx 빈 패치 → 파일 바이트 동일 |
| `TestPptxTemplateReversalReal` | I4a — pptx 같은 양식 2 벌 |

`TestLazyPartLoading` 이 성능 주장을 테스트로 고정하는 자리다. 없으면 "지연 로딩한다" 가 주석으로만 남는다.

### 픽스처

| 파일 | 용도 | 상태 |
|---|---|---|
| `testdata/real/form-a.docx`, `form-b.docx` | 기존 docx 불변식 (회귀) | 있음 |
| `testdata/real/deck-a.pptx` | pptx I1·I2, 다중 파트 | **생성 완료** (§11) |
| `testdata/real/deck-b.pptx` | pptx I4a — `deck-a` 와 같은 양식 | 구현 시 생성 |

pptx 픽스처는 PowerPoint 16.x(macOS)를 AppleScript 로 구동해 만든다. 샌드박스 때문에 컨테이너 안에 저장시키고 복사해 온다 — 구체 절차는 프로젝트 메모리에 있다.

네 픽스처 모두 `docProps/core.xml` 을 다시 썼다 — Word·PowerPoint 가 이 파트에 찍는 macOS 계정 실명(`dc:creator`·`cp:lastModifiedBy`)을 지우기 위해서다. 재작성은 이 프로젝트의 `internal/opc.Open` → `Part`/`Replace`/`Bytes` 로 했다 — 그래야 나머지 모든 zip 엔트리가 원본 생산자 바이트 그대로 남는다. Python `zipfile`이나 `archive/zip`으로 컨테이너를 통째로 다시 만들면 그 자체가 다른 라이터의 산출물이 되어 I1·I2 가 시험하는 "실제 오피스 제품군이 낸 바이트"라는 전제가 허물어진다. 자세한 경위는 `testdata/real/README.md` 참조.

## 11. 실측 기록

pptx 픽스처를 만들며 확인한 것. 설계의 근거이므로 남긴다.

**컨테이너 게이트가 PowerPoint 산출물을 통과시킨다.** `panto dump deck-a.pptx` 의 실패는 `파트 없음: word/document.xml` 뿐이었다 — `internal/opc` 는 pptx 를 바이트 정확히 받아들였다. 자체 zip 라이터가 두 번째 포맷에서 그대로 동작한다.

**PowerPoint 도 growth hint 를 쓴다** — 41 개 엔트리 중 5 개의 로컬 헤더에 `0xA220`. 선행 슬라이스에서 이미 해결된 문제다.

**슬라이드 순서와 파일명** — 3 장 덱을 만들어 저장 전에 한 번, 저장 후에 한 번 순서를 바꿔 재저장했다. 두 경우 모두 `slideN.xml` 이 N 번 슬라이드였다. PowerPoint 가 매 저장마다 파트를 순서대로 다시 번호 매긴다. **관측은 PowerPoint 16.x 하나뿐이며, 다른 생산자(python-pptx·Google Slides·Keynote 내보내기)가 같은 정규화를 한다는 근거는 없다.**

**규모** — 거의 빈 슬라이드가 요소 약 52 개, `slideMaster1.xml` 하나가 383 개, 전체 XML 파트 합계 95KB(3 장 덱). 50 장 실무 덱이면 본문만 수천 노드다. `--part` 없이는 에이전트의 컨텍스트가 덤프로 찬다.

**PowerPoint 는 도형·슬라이드마다 새 생성 ID 를 찍는다.** `TestPptxTemplateReversalReal` 을 처음 붙였을 때 `tmpl.Extract` 가 `nontext_diff` 로 거절했다 — 같은 양식의 `deck-a`/`deck-b` 인데도 실패한 것이다. 원인은 `internal/tmpl` 의 휘발성 속성 집합(`VolatileAttrs`: `paraId`·`textId`·`w:rsid*`)이 Word 의 스탬프만 알고 있었던 것이었다. PowerPoint 는 도형을 새로 만들 때마다 `a16:creationId`(속성 이름 `id`)에 새 GUID 를, 슬라이드를 새로 만들 때마다 `p14:creationId`(속성 이름 `val`)에 새 값을 찍는다 — 내용과 무관하게 매 생성마다 달라지므로 같은 양식의 두 덱이라도 이 두 속성값은 항상 다르고, `Extract` 는 키를 만들기도 전에 `nontext_diff` 로 거절했다.

고친 방식은 속성 스코프가 아니라 **요소 스코프**(`VolatileElements`, `internal/tmpl/schema.go`)다. 원인 속성의 이름이 하필 `id`·`val` 인데, 이 둘은 OOXML 전역에서 진짜 내용(색상 값 등)도 나르는 흔한 이름이라 속성 이름 단위로 전역으로 빼면 비교가 눈을 감는다 — 슬라이드 한 장만도 `id=` 속성 5 개 중 3 개가 진짜 도형 식별자이고, 슬라이드 마스터 하나에는 색상·크기·테마 값을 나르는 `val=` 속성이 47 개다. `creationId` 라는 요소 이름 자체로 저격하면 그 안의 속성만 빠지고 다른 곳의 `id`·`val` 은 그대로 비교 대상에 남는다.

## 12. 범위 밖 · 미해결

- **xlsx** — 시트가 파트로 쪼개지는 것은 같으나, 셀 텍스트가 시트에 없고 `xl/sharedStrings.xml` 에 모여 인덱스로 참조된다. 텍스트 하나를 바꾸려면 두 파트를 함께 봐야 하고, 그것은 이 슬라이스의 "노드는 파트 하나에 속한다" 전제를 건드린다. 별도 슬라이스
- **노드 수준 논리 선택자** — `shape[title]` / `shape[@name='로고']` (§3)
- **본문 파트 밖 스캔** — `--part` 로 마스터·레이아웃·테마를 명시해도 스캔하지 않는다(`part_not_scannable`). `Select` 는 계획을 좁히기만 하므로, 계획 밖 파트를 끌어들이려면 계획 자체가 선택자를 받아야 한다 — `Plan` 의 책임 범위가 달라지는 변경이라 별도 슬라이스
- **슬라이드 추가·삭제·재정렬** — `presentation.xml` 과 rels 를 함께 고쳐야 하고, 이는 제자리 변경이 아니다. 선행 슬라이스가 `insert`/`delete` 를 뺀 것과 같은 이유
- **문자 참조 인코딩 왕복** — 선행 슬라이스의 한계가 그대로 남는다. pptx 픽스처로도 아직 안 건드려졌다
- **다른 pptx 생산자** — 파일명·순서 정규화와 컨테이너 재현 가능 여부 모두 PowerPoint 16.x 만 관측했다
