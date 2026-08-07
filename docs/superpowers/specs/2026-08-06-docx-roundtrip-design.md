# docx 왕복 무손실 코어 + 자동 템플릿 역추출 (설계)

> 상위 설계: [`docs/2026-08-06-pantograph-design.md`](../../2026-08-06-pantograph-design.md)
> 이 문서는 그 설계의 **첫 수직 슬라이스**를 구현 가능한 수준으로 확정한다.

## 1. 이번 슬라이스의 범위

| | 포함 | 근거 |
|---|---|---|
| docx 왕복 무손실 코어 (dump / apply) | ✅ | 상위 설계의 "docx 하나로 수직 슬라이스" |
| 자동 템플릿 역추출 + 채우기 | ✅ | 상위 설계가 기각한 "템플릿 채우기"의 기각 사유(사람이 템플릿을 만들어야 함)를 지운다 |
| 레시피 학습 루프 | ❌ | 볼트·ledger 인프라 선행. 단 덤프가 그 입력이 되는 형태로 설계한다 |
| 렌더 · 픽셀 비교 | ❌ | 합격 임계가 없어 판정할 대상이 없다 |
| xlsx · pptx | ❌ | 상위 설계의 포맷 확장 순서 |
| `setProps` 패치 연산 | ❌ | 이번 두 목표에 불필요. §7 참조 |

## 2. 왜 이 접근인가 — 실측 근거

### 2.1 `encoding/xml`은 OOXML을 왕복시키지 못한다

토큰을 읽어 **그대로 다시 쓰기만 해도** 깨진다 (Go 1.26.5, `xml.Decoder` → `xml.Encoder.EncodeToken` 무가공 전달):

| 원본 | 재직렬화 후 |
|---|---|
| `<w:document xmlns:w="…">` | `<document xmlns="…" xmlns:_xmlns="xmlns" _xmlns:w="…">` |
| `mc:Ignorable="w14"` | `_:Ignorable="w14"` |
| `w14:paraId` | `wordml:paraId` (접두사를 네임스페이스 URI 끝단어로 새로 지어냄) |
| `<w:b/>` | `<b></b>` |

`_xmlns:w`는 유효한 XML도 아니다. 따라서 **재직렬화 경로를 전면 배제한다.**

같은 디코더의 `InputOffset()`으로는 노드의 원문 바이트 범위를 정확히 얻을 수 있음을 같은 실측에서 확인했다.

### 2.2 zip 엔트리 raw 통과는 파일 전체 바이트 동일을 낸다

`zip.File.OpenRaw()` → `zip.Writer.CreateRaw()`로 전 엔트리를 복사하면 원본과 sha256이 일치한다. DEFLATE 엔트리와 STORE 엔트리가 섞여 있어도 유지된다.

**단, 이 실측은 Python `zipfile`이 쓴 zip에 대한 것이다.** Word가 쓴 실제 docx는 ZIP64·data descriptor·extra field 구성이 다를 수 있어 구현 1단계에서 재검증한다 (§11).

**보존되는 것은 엔트리의 *페이로드*지 *헤더*가 아니다.** raw 통과 뒤에도 `Package.Write`는 로컬·중앙 헤더를 `zip.FileHeader`로부터 다시 찍어내는데, `archive/zip`이 zip 헤더의 모든 것을 표현하지는 못한다. Info-ZIP이 쓴 zip으로 확인된 소실 두 가지:

| 소실 | 증상 |
|---|---|
| 중앙 디렉토리의 internal file attributes (레코드 +36) | `zip.FileHeader`에 필드가 없어 writer가 항상 0을 쓴다. Info-ZIP은 텍스트 엔트리에 bit 0을 세운다 → 길이는 같고 1바이트가 달라진다 |
| 로컬 헤더의 extra field | `zip.Reader`는 **중앙** 레코드의 것만 채우고 writer는 그 사본을 양쪽에 찍는다. Info-ZIP의 `UT` 타임스탬프 extra는 로컬 쪽이 더 길어서 **파일 길이 자체**가 달라진다 |

게이트가 실제로 거절하는 범위를 zip 생성기 4종으로 측정했다:

| 생성기 | 결과 | 관측 |
|---|---|---|
| Info-ZIP `zip -X` | 거절 | 첫 차이 offset 146 — 중앙 디렉토리 internal file attributes |
| Info-ZIP `zip` | 거절 | 첫 차이 offset 28, 349 → 341 바이트 — 로컬 헤더 extra field |
| macOS `ditto -c -k` | 거절 | 첫 차이 offset 28, 955 → 935 바이트 |
| Python `zipfile` | 통과 | |
| Python `zipfile` + data descriptor | 통과 | |

거절 범위는 "이색적인 예외"가 아니다 — Unix·macOS에서 가장 흔한 zip 생성기 둘(Info-ZIP, macOS 자체 압축 도구)이 거절당한다. data descriptor는 거절 원인이 아니다 (자연스러운 추정이지만 틀렸다) — 실제 원인은 위 표의 두 소실, internal file attributes 비트와 로컬/중앙 extra field 불일치다. Word 자신의 writer가 게이트를 통과하는지는 검증되지 않았다 — 실제 Word가 만든 .docx가 이 프로젝트에 한 번도 제공되지 않았다.

이 때문에 I1을 "재조립 결과가 원본과 같다"에 기대면 안 된다. 대응은 두 겹이다:

1. **빈 패치는 재조립하지 않는다.** 고친 파트가 하나도 없으면 `Write`가 원본 바이트를 그대로 흘려보낸다 — 항등 경로에서 헤더 재현 문제를 통째로 제거한다
2. **열 때 재현 가능성을 검사한다.** `opc.Open`이 읽은 것을 즉시 되쓴 뒤 원본과 비교해, 다르면 `unsupported_container`로 **거절한다** (§9). 파트를 고치는 경로는 재조립을 피할 수 없으므로, 이 게이트가 "열린 파일은 재조립해도 안전하다"를 보장해 I2를 받쳐준다

거절되는 실제 파일이 있을 수 있다. 그것이 의도다 — 폴백으로 근사하면 "안 건드린 것은 바이트 동일"이 조용히 거짓이 된다. 검증되지 않은 가정은 보증이 아니다.

`TestIdentityGenerated`가 이 문제를 잡을 수 없었던 이유도 여기 있다: 픽스처 생성기가 `archive/zip` 자신이라, 헤더 축에서는 "Go의 zip writer가 Go의 zip writer가 쓴 것을 재현하는가"를 묻는 동어반복에 가깝다. `TestIdentityReal`이 필요한 이유다.

### 2.3 결론 — 바이트 오프셋 스플라이싱

파싱은 **주소를 만들기 위해서만** 한다. 각 노드의 `(offset, length)`를 기록하고, 패치는 그 바이트 구간을 갈아끼우는 문자열 연산이다. XML 트리를 다시 직렬화하는 일은 한 번도 없다.

무손실이 *증명*이 아니라 *구조*로 보장된다 — 안 건드린 바이트는 애초에 지나간 적이 없다.

대가: 잘라 붙인 결과의 XML 유효성을 파서가 막아주지 않는다. 적용 후 재스캔으로 대신한다 (§6).

### 2.4 기각한 대안

| 대안 | 기각 사유 |
|---|---|
| `encoding/xml` 재직렬화 | §2.1이 기각. 커스텀 마샬러로 우회 가능하나 그것은 XML 라이터를 새로 쓰는 일이고, 무손실을 매번 증명해야 한다 |
| 전 파트 IR 파싱 + canonical 재직렬화 | 상위 설계가 "객체 모델 라이브러리"를 기각한 실패 양상 그 자체. 정규화 규칙이 소실을 가린다 |

## 3. 성공 기준 — 불변식

전부 기계가 판정한다. 사람이 문서를 열어볼 필요가 없다.

| | 불변식 | 판정 |
|---|---|---|
| **I1 항등** | `apply(D, 빈 패치)` == `D` | 파일 전체 바이트 동일 |
| **I2 국소성** | `apply(D, P)`에서 P가 안 건드린 zip 엔트리 | 압축 데이터까지 바이트 동일. 건드린 파트는 압축 해제 내용이 기대와 바이트 동일 |
| **I3 결정성** | `dump(D)`를 두 번 | 동일 바이트의 JSON |
| **I4a 템플릿 가역성 (베이스)** | `fill(extract({D₁..Dₙ}), values(D₁))` == `D₁` | 파트별 압축 해제 내용 바이트 동일 |
| **I4b 템플릿 가역성 (그 외)** | i ≥ 2에 대해 `fill(…, values(Dᵢ))` vs `Dᵢ` | `word/document.xml`의 텍스트 노드 전체 일치 |

**I4a가 이 설계의 심장이다.** 역추출이 텍스트 외의 무엇이라도 건드렸다면 원래 값으로 되채웠을 때 원본이 안 나온다. 시각 비교·사람 눈·임계값 없이 템플릿 기능의 정확성이 증명된다.

**I2에서 압축 데이터와 압축 해제 내용을 가른 이유**: Word의 deflate 구현을 Go가 바이트 단위로 재현한다는 보장이 없다. 안 건드린 것은 재압축을 아예 안 하므로 압축 데이터까지 동일하고, 건드린 것은 재압축하므로 내용 수준에서만 주장한다.

**I4를 a/b로 가른 이유**: Word는 문단마다 `w14:paraId`를, 저장마다 `w:rsid*`를 붙이고 `docProps/core.xml`에 수정 시각을 쓴다. 같은 양식의 두 문서는 본문 텍스트가 같아도 이 값들이 다르다. 템플릿은 한쪽을 베이스로 삼을 수밖에 없으므로 다른 쪽을 바이트 단위로 되살릴 수 없다. **주장할 수 없는 것을 주장하지 않는다.**

## 4. 산출 표면

```
panto dump  <in.docx>                                     → stdout 덤프 JSON
panto apply <in.docx> -p patch.json -o out.docx
panto tmpl extract <a.docx> <b.docx> [...] -o tmpl.docx --schema schema.json
panto tmpl fill    <tmpl.docx> --schema schema.json -d data.json -o out.docx
```

`tmpl fill`은 편의 기능이 아니다. **없으면 템플릿이 맞는지 검증할 방법이 없다** (I4).

## 5. 아키텍처

```
cmd/panto/        CLI 진입 — 플래그, stdout JSON, 종료 코드
internal/opc/     OPC 컨테이너 — zip 엔트리 raw 보관·재작성
internal/wml/     WordprocessingML 스캐너 — 경로 부여 + 바이트 범위
internal/dump/    덤프 JSON 스키마
internal/patch/   패치 파싱·검증·스플라이스 적용 (트랜잭션)
internal/tmpl/    N벌 정렬 → 가변부 판별 → 템플릿·스키마, fill
```

| 패키지 | 하는 일 | 의존 |
|---|---|---|
| `opc` | `Open(path)`가 엔트리 순서·헤더를 그대로 보관. `Part(name)`은 압축 해제(1회 캐시), `Replace(name, bytes)`, `Write(w)`는 수정된 파트만 재압축하고 나머지는 `CreateRaw` | `archive/zip` |
| `wml` | `Scan(bytes) → Tree`. 노드마다 경로와 바이트 범위. **재직렬화 함수를 두지 않는다** | `encoding/xml` (읽기 전용) |
| `dump` | Tree + 컨테이너 메타 → JSON | `wml`, `opc` |
| `patch` | 패치 JSON → `[]Splice` 검증·적용 | `wml`, `opc` |
| `tmpl` | Tree N개 정렬 → 가변부 → 템플릿·스키마, fill | `wml`, `patch`, `opc` |

**`wml`에 재직렬화 함수를 두지 않는 것이 설계의 강제 장치다.** 있으면 언젠가 누가 쓴다. 없으면 스플라이싱 외에 길이 없다.

모든 것이 이 타입 하나 위에 선다:

```go
type Span struct{ Start, End int }  // word/document.xml 내 바이트 [Start, End)
```

## 6. 데이터 흐름

### dump

```
opc.Open → 엔트리 목록(순서·헤더 보존)
  → word/document.xml 만 압축 해제        ← 유일하게 파싱하는 파트
  → wml.Scan → []Node{Path, Type, Span, Attrs, Text}
  → JSON
```

**경로 부여 규칙** — I3(결정성)의 근거:

- 인덱스는 *같은 부모 아래 같은 로컬명* 기준 1-base: `word/body[1]/p[3]/r[1]/t[1]`
- 인덱스를 **항상** 붙인다. 형제가 하나일 때 생략하는 규칙은 문서를 끝까지 읽어야
  결정되므로 단일 패스 스캔이 불가능하고, 형제가 하나 늘면 기존 경로가 바뀌어
  경로 안정성도 깨진다
- 순수 함수. 난수·시각 없음
- **속성은 정렬된 슬라이스로 낸다.** Go 맵 순회 순서가 랜덤이라 `map[string]string`을 그대로 JSON으로 내면 I3가 깨진다. 타입 수준에서 막는다

**덤프 JSON**:

```json
{
  "doc": {
    "format": "docx",
    "hash": "sha256:10c770bf…",
    "parts": ["[Content_Types].xml", "_rels/.rels", "word/document.xml", "…"],
    "scannedPart": "word/document.xml"
  },
  "nodes": [
    { "path": "word/body[1]/p[1]", "type": "p",
      "span":  {"start": 301, "end": 383},
      "inner": {"start": 328, "end": 377},
      "attrs": [
        {"name":"paraId", "ns":"http://schemas.microsoft.com/office/word/2010/wordml",
         "value":"00000001"}
      ] },
    { "path": "word/body[1]/p[1]/r[1]/t[1]", "type": "t",
      "span":  {"start": 333, "end": 371},
      "inner": {"start": 359, "end": 365},
      "attrs": [
        {"name":"space", "ns":"http://www.w3.org/XML/1998/namespace", "value":"preserve"}
      ],
      "text": "제목" }
  ]
}
```

`doc.hash`는 **입력 파일 전체 바이트의 sha256**이다 (압축 해제 전, zip 컨테이너 그대로). 패치의 `hash`가 이 값과 대조된다 — 낙관적 잠금이 `word/document.xml`뿐 아니라 컨테이너 전체의 변경을 잡는다.

`span`은 `scannedPart`의 압축 해제된 바이트 기준 `[start, end)`이며 `{"start":…, "end":…}` 객체다. `inner`는 시작·종료 태그를 뺀 안쪽 구간으로, `setText`가 갈아끼우는 것이 바로 이 구간이다 (자기닫힘 요소는 폭 0). 다른 파트는 스캔하지 않으므로 노드가 없다.

**속성은 접두사가 아니라 `{name, ns}` 쌍으로 낸다.** 접두사(`w14:`, `xml:`)는 문서마다 다르게 선언될 수 있어 안정적인 식별자가 아니다 — `ns`는 네임스페이스 URI이고, 네임스페이스가 없으면 생략된다. 네임스페이스 **선언** 자체(`xmlns:w=…`)도 `{"name":"w","ns":"xmlns"}` 형태의 속성으로 나온다는 점에 주의한다 — `encoding/xml`이 그것을 일반 속성으로 돌려주기 때문이다.

`text`는 그 요소가 **직접** 품은 문자 데이터다. 자손의 텍스트는 포함하지 않으므로, 들여쓰기가 없는 `document.xml`에서 비단말 요소의 `text`는 비어 있다.

### apply

```
1. 스캔 → 경로→Span 맵                   (dump과 같은 코드)
2. hash 대조                             불일치 → 거부
3. 모든 op 경로 해석 + 경로 중복 검사      하나라도 실패 → 아무것도 적용하지 않고 실패 목록
4. op → Splice 변환
5. Span 겹침 검사                         겹치면 거부
6. offset 내림차순으로 지역 버퍼에 적용     ← 앞에서부터 하면 뒤 Span이 밀린다
7. 결과를 다시 wml.Scan                   파싱 실패 → invalid_xml 거절 (출력 파일 미생성)
8. opc.Replace → Write
```

7번이 접근법의 대가를 치르는 지점이다 (§2.3).

**7번에서 되돌릴 것은 없다.** 스플라이스는 6번의 지역 버퍼에만 쌓이고 `opc.Replace`는 8번에서 처음 호출된다 — 트랜잭션을 연 적이 없으니 닫을 것도 없다. 실패는 열지 않은 것으로 끝난다. 롤백 기구가 있는 것처럼 쓰면 나중에 누가 없는 트랜잭션을 보정하는 코드를 더한다.

**op이 하나도 없는 패치(빈 패치)는 4번 이후 스플라이스가 없으므로 패키지를 건드리지 않고 그대로 반환한다.** 내용이 같아도 파트를 무조건 다시 쓰면 dirty 로 표시돼 `Package.Write`가 재압축을 하고, 재압축 결과가 원본과 바이트 단위로 같다는 보장이 없어 I1이 깨진다. 그 위에 `Package.Write`가 "고친 파트가 없으면 원본 바이트를 그대로 쓴다"를 더해 I1을 컨테이너 층에서도 구조로 못박는다 (§2.2).

## 7. 패치 계약

```json
{
  "hash": "sha256:…",
  "ops": [
    {"op":"setText",    "path":"word/body[1]/p[1]/r[1]/t[1]", "text":"새 제목"},
    {"op":"replaceRaw", "path":"word/body[1]/tbl[1]",         "xml":"<w:tbl>…</w:tbl>"}
  ]
}
```

`replaceRaw`가 **기본 연산**이다 — Span을 통째로 갈아끼운다. 상위 설계의 요구 #6(원시 XML 대체)이 특수 기능이 아니라 엔진의 바닥이 된다. `setText`는 그 위에 얹힌 편의 계층으로, 대상 `w:t`의 텍스트 노드 Span만 교체한다.

`setProps`를 뺀 이유: 이번 두 목표는 `setText`만으로 달성되고, 표현력은 `replaceRaw`가 전부 커버한다. `setProps`는 `w:pPr`가 없는 문단에 속성을 넣을 때 요소 삽입이 필요해 살이 붙는데, 그 살은 보정 루프 슬라이스에서 값을 한다.

**`setText` 거절 규칙**: 대상 `w:t`에 `xml:space="preserve"`가 없는데 새 텍스트의 앞뒤에 공백이 있으면 거부하고 `replaceRaw`를 안내한다. 조용히 속성을 붙이면 원본에 없던 바이트가 생겨 I4a가 깨진다. **폴백하지 않고 거절한다.**

**`setText` 거절 규칙 (둘째)**: 대상 `w:t`가 자기닫힘(`<w:t/>`)이면 거부하고 `replaceRaw`를 안내한다. 자기닫힘 요소는 시작·종료 태그가 하나로 합쳐져 있어 '안쪽'이 없다 — 스캐너는 이 경우 안쪽 Span을 요소 바로 뒤의 폭 0 지점으로 보고하므로, 거기에 스플라이스하면 텍스트가 `w:t` 밖(형제 위치)에 들어가 well-formed 하지만 의미가 깨진 XML이 나온다. 적용 후 재스캔 검증은 well-formed 여부만 보므로 이 오류를 잡지 못한다.

대안으로 `<w:t/>`를 `<w:t>text</w:t>`로 다시 써서 안쪽을 만들어주는 방법을 검토했으나 기각했다 — I4a를 깨기 때문이다. 템플릿을 빈 값으로 되채우면 원본은 `<w:t/>`인데 결과는 `<w:t></w:t>`가 되어 바이트가 갈린다.

## 8. 템플릿 역추출

```
입력 D₁..Dₙ (n ≥ 2), D₁이 베이스
  1. 각각 dump → Tree
  2. 구조 정렬: 경로 집합이 완전히 일치해야 함
       불일치 → 실패 + 최초로 갈라진 경로 보고
  3. 경로별 비교
       w:t 텍스트      : 전부 같으면 고정부 / 하나라도 다르면 가변부
       그 외 원문 바이트 : 휘발성 속성(w14:paraId, w14:textId, w:rsid*) 제외 후 비교
                         그래도 다르면 실패 — "같은 양식"이 아니라는 뜻
  4. 가변부 → 키: 경로 DFS 순으로 k1, k2, … (결정론적)
  5. 산출
       tmpl.docx   D₁에 setText 패치를 적용해 가변부를 {{kN}}으로
       schema.json 키·경로·관측 샘플
```

```json
{ "base": "invoice-2024-01.docx", "hash": "sha256:…",
  "keys": [
    {"key":"k1", "path":"word/body[1]/p[2]/r[1]/t[1]", "samples":["홍길동","김철수"]},
    {"key":"k2", "path":"word/body[1]/tbl[1]/tr[2]/tc[3]/p[1]/r[1]/t[1]", "samples":["1,200,000","880,000"]}
  ]}
```

**2번의 "완전 일치"가 v1의 절단면이다.** 문단 수가 다르면 실패한다. LCS 기반 유연 정렬은 그 자체로 별도 주제고, 지금 넣으면 이 슬라이스가 정렬 알고리즘 프로젝트가 된다.

**3번에서 텍스트 외의 차이를 만나면 실패시킨다.** D₁의 것을 채택하고 넘어가면 조용한 손실이 생기고 I4a가 무의미해진다. 휘발성 속성만 명시적 예외로 둔다.

구현상 비교 대상은 **요소 자신의 마크업**(`Type` + 휘발성 제외 `Attrs`)이다. 비단말 노드의 원문 바이트는 자손의 가변 텍스트를 포함하므로 그대로 비교하면 가변부가 있는 모든 조상이 거짓 불일치를 낸다.

**비교 범위는 `word/document.xml`뿐이다.** `styles.xml`·`theme1.xml`·`numbering.xml` 등 다른 파트는 스캔하지 않으므로 비교하지도 않고, 템플릿은 D₁의 것을 그대로 쓴다. 이것이 I4b를 텍스트 수준으로 낮춰 잡은 두 번째 이유다 — D₂의 스타일이 D₁과 다르면 채운 결과가 D₂와 시각적으로 다를 수 있고, 이 설계는 그것을 잡지 못한다. **잡지 못한다는 사실을 숨기지 않는다.** 다중 파트 비교는 별도 슬라이스(§13).

**5번은 자기 자신의 patch 엔진을 쓴다.** 템플릿 전용 코드 경로를 만들지 않는다 — `tmpl extract`는 setText 패치를 만드는 기계이고, `tmpl fill`은 schema + data → 패치 → `apply`다. 새 엔진이 없다는 것은 I1~I3가 템플릿 층까지 자동으로 덮는다는 뜻이다.

키 이름을 `k1, k2…`로 두는 이유: 근처 텍스트 기반 이름 추정은 휴리스틱이라 결정성이 흔들린다. `schema.json`에 경로와 샘플이 있으므로 사람이 나중에 rename 할 수 있다.

## 9. 에러 처리

모든 실패는 stdout JSON + 비-zero 종료 코드. **부분 적용은 없다.**

```json
{"ok": false, "errors": [
  {"path":"word/body[1]/p[99]", "reason":"path_not_found", "detail":"body에 w:p는 12개"}
]}
```

| 코드 | 뜻 |
|---|---|
| 0 | 성공 |
| 1 | 입력 오류 — 경로 미해석 / hash 불일치 / Span 겹침 / setText 공백 거절 / 구조 불일치 / 적용 후 재스캔 실패 / 재현 불가 컨테이너 |
| 2 | 내부 오류 — 파일 손상 / I/O |

**적용 후 재스캔 실패가 코드 1인 이유**: 결함은 전적으로 호출자가 준 `replaceRaw` XML에 있다. 종료 코드로 재시도 여부를 가르는 에이전트에게 코드 2는 "도구가 고장났으니 포기"를 뜻하므로, 호출자가 고칠 수 있는 실패를 거기로 보내면 안 된다. 이 실패도 다른 거절과 똑같이 stdout JSON + 경로를 단다 (`invalid_xml`).

구현이 내는 `Reason` 전체 목록:

| Reason | 뜻 | 코드 |
|---|---|---|
| `hash_mismatch` | 낙관적 잠금 실패 — 덤프 이후 문서가 바뀌었다 | 1 |
| `path_not_found` | 경로가 문서에 없다 | 1 |
| `unknown_op` | 알 수 없는 연산 (setText \| replaceRaw) | 1 |
| `duplicate_path` | 같은 경로를 가리키는 op 이 둘 이상이다 — 적용 순서가 정의되지 않는다 | 1 |
| `overlap` | 두 패치의 바이트 구간이 겹친다 | 1 |
| `type_mismatch` | setText 대상이 `w:t` 가 아니다 | 1 |
| `whitespace_needs_preserve` | 앞뒤 공백을 넣으려는데 `xml:space="preserve"` 가 없다 | 1 |
| `self_closing_target` | setText 대상이 자기닫힘 요소라 텍스트 자리가 없다 | 1 |
| `invalid_xml` | 적용 결과가 well-formed XML 이 아니다. 원인이 된 op 의 경로를 단다 | 1 |
| `unsupported_container` | zip 컨테이너를 바이트 동일하게 재조립할 수 없다 (§2.2) | 1 |
| `too_few_documents` | 템플릿 역추출에 문서가 2벌 미만 | 1 |
| `structure_mismatch` | 문서 간 경로 집합이 다르다 | 1 |
| `nontext_diff` | 텍스트 외의 마크업이 문서마다 다르다 | 1 |
| `missing_key` | 데이터에 스키마의 키가 없다 | 1 |
| `template_drift` | 템플릿의 해당 경로에 기대한 자리표시자가 없다 | 1 |

`duplicate_path`를 `overlap`과 따로 두는 이유: 겹침 검사는 구간 비교(`start < 앞의 end`)라 폭 0 구간(빈 `<w:t></w:t>`의 안쪽)을 잡지 못한다. 같은 경로 자체를 검증 단계에서 거절하면 원인이 정확히 드러나고, 정렬이 안정 정렬이 아니어서 생기는 순서 비결정성(I3)도 함께 닫힌다.

`panto tmpl fill`은 키가 0개인 스키마를 입력 오류로 거절한다 — 빈 `keys` 배열을 그대로 두면 `missing_key`/`template_drift` 검사를 하나도 거치지 않고 `{{key}}` 자리표시자가 그대로 남은 템플릿을 "ok": true 로 내보내게 된다.

에러는 항상 **경로**를 단다. 상위 설계의 "시스템은 차이를 경로로 가리키는 것까지"라는 역할 분담이 에러 메시지에도 적용된다.

## 10. 테스트

RED 먼저. 불변식을 테스트로 고정한다.

| 테스트 | 검증 |
|---|---|
| `TestIdentity` | I1 — 실제 Word docx + 빈 패치 → 파일 전체 바이트 동일 |
| `TestDeterminism` | I3 — dump 두 번 → 동일 바이트 |
| `TestLocality` | I2 — setText 1건 → 다른 엔트리 압축 데이터 동일 |
| `TestAtomicity` | 유효+무효 경로 혼합 패치 → 출력 파일 미생성 + 무효 경로만 에러 목록에 |
| `TestOverlapRejected` | Span 겹침 거절 |
| `TestHashMismatch` | 낙관적 잠금 거절 |
| `TestWhitespaceRejected` | setText 공백 거절 |
| `TestTemplateReversal` | I4a — 2벌 → extract → fill(values(D₁)) → D₁과 바이트 동일. 생성 픽스처(`…Base`)와 실제 문서(`…Real`) 둘 다 |
| `TestTemplateTextEquality` | I4b — fill(values(D₂)) → D₂와 텍스트 노드 일치 |
| `TestStructureMismatch` | 구조 다른 2벌 → 갈라진 경로를 정확히 지목 |

### 픽스처

| 층 | 조달 | 상태 |
|---|---|---|
| 최소 docx | `internal/testutil/gen.go`가 코드로 생성 (결정론적, 생성기를 버전 관리) | 문제없음 |
| 실제 Word 문서 1개 | 사용자 제공 → `testdata/real/`, git 커밋 | **미확보** |
| 같은 양식 2벌 이상 | 사용자 제공 → `testdata/real/`, git 커밋 | **미확보** |

실제 문서가 없으면 I1·I2·I4 테스트는 **skip이 아니라 FAIL**로 둔다. 상위 설계가 "LibreOffice 없으면 없다고 보고하고 조용히 통과시키지 않는다"고 한 것과 같은 자세다.

머신 탐색 결과 사용 가능한 `.docx`가 없었다 (발견된 5개는 레거시 바이너리 `.doc`/CFB 포맷으로 범위 밖이며, 실제 고객사 문서라 픽스처로 쓰려면 별도 판단이 필요하다).

현재까지 어떤 불변식도 실제 Word가 만든 `.docx`로 검증되지 않았다. `TestIdentityReal`(I1) · `TestLocalityReal`(I2) · `TestTemplateReversalReal`(I4a) 셋 다 픽스처가 없어 설계대로 FAIL한다. I4a 증명은 지금 프로젝트 자체 픽스처 생성기(`internal/testutil/gen.go`)가 만든 문서에 대해서만 돌며, §13의 문자 참조 왕복 한계(`&amp;` vs `&#38;`)가 드러날 자리도 바로 거기다 — §2.2가 지적한 ZIP64 · data descriptor · extra field 위험과 함께 아직 검증되지 않은 채로 남아 있다.

## 11. 개발 순서

```
1. opc + I1              ← 실제 Word 파일로 즉시 검증
2. wml.Scan + I3
3. patch(replaceRaw) + I2 + 원자성
4. patch(setText) + 거절 3종
5. tmpl extract/fill + I4a/I4b
```

**1번을 맨 앞에 둔다.** 실제 Word docx에서 `CreateRaw` 왕복이 깨지면(ZIP64·data descriptor·extra field) 접근법의 토대가 무너지므로, 코드를 더 쌓기 전에 알아야 한다. Python이 쓴 zip에서 통과한 것은 증거가 아니다 (§2.2).

## 12. 상위 설계 문서에 주는 피드백

이번 슬라이스가 확정한 것 중 상위 설계 문서를 구체화하는 항목:

| 상위 설계의 서술 | 이 슬라이스의 확정 |
|---|---|
| 덤프 스키마의 `raw` 필드가 왕복 무손실의 열쇠 | `raw`는 **문자열 사본이 아니라 바이트 범위 `Span`**이다. 사본을 들면 원본과 갈라질 수 있다 |
| 요구 #6 "원시 XML 대체" | 부가 기능이 아니라 **엔진의 기본 연산**. `setText`가 그 위에 얹힌다 |
| 구현 언어 Go | §2.1이 근거를 하나 더한다 — Go의 `encoding/xml`은 못 쓰지만, 애초에 안 쓰는 설계라 무관하다 |

## 13. 범위 밖 · 미해결

- **구조가 다른 문서 간 템플릿 추출** — LCS 기반 유연 정렬. 별도 슬라이스
- **`word/document.xml` 외 파트의 비교** — 문서 간 `styles.xml`·`numbering.xml` 차이는 이번 슬라이스가 보지 못한다 (§8)
- **키 이름 자동 추정** — 근처 텍스트 기반. 결정성과 상충하므로 보류
- **`setProps`** — 보정 루프 슬라이스에서
- **`w14:paraId` 재생성 규칙** — Word가 어떤 조건에서 재생성하는지 미확인. I4b를 텍스트 수준으로 낮춘 것이 이 불확실성에 대한 대응이다
- **ZIP64 · data descriptor** — 실제 Word 파일에서의 거동 미확인 (§11의 1단계가 확인한다)
- **문자 참조 인코딩 왕복** — 템플릿 채우기는 텍스트를 디코딩한 뒤 `&`, `<`, `>`만 다시 이스케이프해 재인코딩한다. 원본이 같은 문자를 다른(그러나 동등한) 인코딩 — 숫자 문자 참조 `&#38;`, 또는 `&quot;`/`&apos;` — 으로 썼다면 왕복 결과가 원본과 바이트 단위로 달라지고 I4a가 정당하게 실패한다. 현재 테스트는 한글·숫자·구두점만 쓰므로 이 경로를 건드리지 않는다. 실제 Word 문서는 `&amp;`를 흔히 쓰므로, 실제 픽스처가 들어오면 가장 먼저 확인할 지점이다
