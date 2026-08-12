# 명시적 삭제 — 연산과 필드의 계약

**날짜**: 2026-08-12
**상태**: 승인됨
**선행**: [tmpl 정렬](2026-08-11-tmpl-align-design.md) (PR #6)
**후속**: 선택 블록 — 이 슬라이스의 `delete` 를 소비한다

---

## §1 문제 — 증상 셋, 뿌리 하나

`panto apply` 는 오늘 **패치 하나로 내용을 지우면서 `ok:true` 를 반환한다.** 세 경로 모두 실측했다 (`testdata/real/form-a.docx`, 문단 6개).

**증상 1 — `replaceRaw` 에 `xml` 을 빠뜨리면 노드가 사라진다**

```
$ echo '{"ops":[{"op":"replaceRaw","path":"document/body[1]/p[6]"}]}' > p.json
$ panto apply form-a.docx -p p.json -o out.docx
{"ok":true}
$ echo $?
0
→ 문단 6개 → 5개
```

**증상 2 — `xml:""` 도 같다.** 위와 바이트 결과가 동일하다.

**증상 3 — `setText` 에 `text` 대신 `xml` 을 주면 텍스트가 지워진다**

```
$ echo '{"ops":[{"op":"setText","path":"document/body[1]/p[1]/r[1]/t[1]","xml":"<w:t>바뀜</w:t>"}]}' > p.json
$ panto apply form-a.docx -p p.json -o out.docx
{"ok":true}
→ "청구서" → (텍스트 없음)
```

증상 3 은 **PR #3 이 고쳤다고 믿었던 실패의 재발**이다. 그때 `decodeStrict` 의 `DisallowUnknownFields` 로 *모르는* 키(`"value"` 오타)는 막았지만, `xml` 은 `patch.Op` 가 아는 키라 검사를 그대로 통과한다. 문을 하나 닫고 옆 문을 열어둔 것이다.

뿌리는 하나다: **`Op` 은 합집합 타입인데 필드와 연산이 맞는지 아무도 보지 않는다.** `Op`·`Part`·`Path` 는 공통, `Text` 는 `setText` 전용, `XML` 은 `replaceRaw` 전용이라는 계약이 주석에만 있고 코드에 없다.

이 결함 종류는 이 저장소가 반복해서 처벌해 온 것이다 — 도구가 실패를 성공이라 말하는 것. 에이전트는 `ok:true` 를 보고 다음 단계로 넘어가고, 잃은 내용은 아무도 세지 않는다.

## §2 무엇을 만드나

### 2.1 `delete` 연산 신설

```json
{"op": "delete", "part": "word/document.xml", "path": "document/body[1]/p[6]"}
```

노드의 `Span` 전체를 폭 0 으로 치환한다. **앞뒤 공백·개행은 건드리지 않는다** — 인접 바이트를 먹으면 "어디까지 지웠나"가 흐려지고 I2(국소성) 논증이 약해진다. 들여쓴 XML 이면 빈 줄이 남지만 well-formed 이고 의미도 같다.

기존 기계를 그대로 쓴다: `splice{span: n.Span, repl: nil}`. 겹침 검사·내림차순 적용·결과 재스캔 모두 변경 없이 적용된다.

### 2.2 `Text`·`XML` 을 `*string` 으로

이것이 핵심이다. `nil`(필드 없음)과 `""`(의도적으로 빈 값)를 구분해야 "빠뜨림"을 잡을 수 있다.

```go
Text *string `json:"text,omitempty"` // setText
XML  *string `json:"xml,omitempty"`  // replaceRaw
```

| 입력 | 판정 | 근거 |
|---|---|---|
| `{"op":"setText","path":…}` | **거절** `missing_text` | 오늘은 텍스트를 지운다 |
| `{"op":"setText","path":…,"text":""}` | 허용 | 양식 필드 비우기는 정당한 요구다 |
| `{"op":"setText",…,"xml":…}` | **거절** `unused_field` | 증상 3 |
| `{"op":"replaceRaw","path":…}` | **거절** `missing_xml` | 증상 1 |
| `{"op":"replaceRaw",…,"xml":""}` | **거절** `empty_xml` | 증상 2 |
| `{"op":"delete",…,"text":…}` | **거절** `unused_field` | — |

`delete` 없이 §2.2 만 하면 삭제할 방법 자체가 사라진다. 둘은 같은 슬라이스여야 한다.

## §3 거절 이유

`reason` 은 이 저장소에서 에이전트의 다음 행동을 가르는 값이다 (docx 설계 §9). 처방이 다르면 이유도 나눈다.

| reason | 언제 | 처방 |
|---|---|---|
| `missing_text` | `setText` 에 `text` 가 없다 | 값을 써라 |
| `missing_xml` | `replaceRaw` 에 `xml` 이 없다 | 내용을 써라 |
| `empty_xml` | `replaceRaw` 의 `xml` 이 `""` | **지우려면 `delete` 를 써라** |
| `unused_field` | 이 연산이 안 쓰는 필드가 왔다 | 필드를 빼거나 연산을 고쳐라 |
| `delete_root` | 루트 노드를 지우려 한다 | — |

`missing_xml` 과 `empty_xml` 을 합치지 않는 이유: 전자는 "내용을 빠뜨렸다", 후자는 "지우려 했다"로 **의도가 다르고 처방이 다르다.**

`delete_root` 를 따로 두는 이유: 안 두면 결과 재스캔이 `invalid_xml` 로 잡는데, 그 이유는 "네가 준 XML 이 나쁘다"는 뜻이다. `delete` 에는 사용자가 준 XML 이 없으니 **틀린 이유**를 대게 된다.

### 3.1 검사 순서 — 한 연산은 이유 하나만 낸다

기존 코드는 op 마다 이유를 하나 내고 `continue` 한다. 그 골격을 유지하되 순서를 고정한다:

1. **필드 정합** — 안 쓰는 필드(`unused_field`) → 빠뜨린 필드(`missing_text`/`missing_xml`) → 빈 값(`empty_xml`)
2. **경로 조회** — `path_not_found`
3. **연산별 검사** — `type_mismatch`·`self_closing_target`·`whitespace_needs_preserve`·`delete_root`

`{"op":"setText","xml":…}` 처럼 두 결함이 겹치는 입력은 **`unused_field` 를 낸다** — 사용자가 실제로 한 실수는 "필드를 잘못 골랐다"이지 "값을 빠뜨렸다"가 아니다. 순서가 곧 진단의 품질이다.

필드 정합을 경로 조회보다 먼저 두는 이유: 필드가 틀렸다는 건 경로가 존재하든 말든 사실이고, 경로까지 틀린 패치에서 `path_not_found` 만 보여주면 사용자가 경로를 고친 뒤 두 번째 오류를 만나게 된다.

## §4 불변식

| | |
|---|---|
| **P1 (명시성)** | 삭제는 `delete` 로만 일어난다. 다른 어떤 연산·필드 조합으로도 노드가 사라지지 않는다 |
| **P2 (필드 정합)** | 연산이 안 쓰는 필드도, 빠뜨린 필드도 거절한다. 단 `setText` 의 `""` 는 정당한 값이다 |
| **P3 (전부 아니면 전무)** | 기존 계약 유지 — 거절 시 출력 파일이 생기지 않는다 |
| **I2 (국소성)** | 삭제 후에도 손대지 않은 엔트리는 압축 바이트까지 동일하다 |

P1 은 **회귀 테스트로만 지켜진다**. §1 의 세 명령을 그대로 테스트에 넣어, 각각이 거절되고 출력 파일이 생기지 않는지 본다. 오늘 초록인 테스트를 그대로 두면 이 슬라이스가 무엇을 고쳤는지 아무도 증명하지 못한다.

## §5 한계 — 명시할 것

**`delete` 는 파트 안의 노드만 지운다.** 지워진 노드가 참조하던 관계(`.rels`)와 미디어 파트는 남는다 — 문서는 열리지만 안 쓰이는 바이트가 남는다. 이것을 정리하려면 관계 그래프를 따라가야 하는데, 그건 "노드는 파트 하나에 속한다"는 전제를 넘는 작업이다.

**파트 자체를 지우는 것은 범위 밖이다.** 슬라이드 삭제는 `presentation.xml` 과 그 rels 를 함께 고쳐야 해서 제자리 변경이 아니다 — README 가 그은 경계 그대로다.

**스키마 검증은 하지 않는다.** `<w:body>` 를 지우면 결과는 well-formed 이지만 Word 가 열지 못할 수 있다. panto 는 바이트 하네스지 스키마 검증기가 아니다. 재스캔이 통과하면 통과시킨다.

## §6 영향

| 파일 | 무엇 |
|---|---|
| `internal/patch/patch.go` | `Op.Text`·`Op.XML` 을 `*string` 으로, 계약을 주석에 명시 |
| `internal/patch/apply.go` | 필드 정합 검사 + `delete` 분기 + `delete_root` 사전 거절 |
| `internal/patch/apply_test.go` | `Op` 리터럴 27곳 갱신 + 새 테스트 |
| `internal/tmpl/extract.go:159` | `Text: &placeholder` |
| `internal/tmpl/fill.go:114` | `Text: &v` |
| `cmd/panto/main_test.go` | CLI 수준 거절 (exit 1, 출력 파일 미생성) |
| `README.md` | 연산 목록, "다음 작업" 4번의 insert/delete 문구 정정 |

**7 파일 — HARD-GATE 상 간략 설계 등급** (영향 분석 + 태스크 분해).

## §7 왜 이 순서인가

원래 README 4번은 "선택적 블록에는 `patch` 에 insert/delete 연산이 필요하다"였다. 실측해 보니 **절반이 틀렸다**:

- **삭제는 이미 된다** — `replaceRaw` + 빈 `xml`. 다만 그게 §1 의 결함이다
- **삽입도 표현은 된다** — 앵커의 원본 바이트(239 B)를 복제해 `replaceRaw` 하면 문단이 6→7 이 된다. 호출자가 원본 바이트를 들고 있어야 할 뿐이다

그래서 필요한 건 새 능력이 아니라 **계약**이다. 이 슬라이스가 그 계약을 세우고, 다음 슬라이스(선택 블록)가 그 위에 선다 — 블록을 끄는 것은 `delete` 를 발행하는 일이다.

선택 블록 슬라이스에서 이미 정한 것 (여기 기록해 두고 그 스펙에서 전개한다):

- **표현 범위**: 삭제만. base 가 상위집합이어야 하고, base 에 없는 블록은 계속 `unrepresented` 로 거절한다
- **블록 단위**: 같은 부재 집합을 가진 연속 형제를 하나로 (런 단위). `align.Siblings` 가 이미 런 경계를 알고 있고, `Match` 가 그걸 개별 노드로 펼쳐 버리고 있다
- **누락 시**: 거절 (`missing_block`). 키의 `missing_key` 와 같은 규칙
- **블록 안의 키**: 있을 수 없다. 키는 "모든 문서에서 짝지어진 노드", 블록은 "어떤 문서에서 짝이 없는 서브트리"라 정의상 배타적이다 — 불변식으로 잠근다
