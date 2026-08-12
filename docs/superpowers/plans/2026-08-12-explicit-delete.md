# 명시적 삭제 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 삭제를 명시적 연산으로 만들고, 패치 하나가 `ok:true` 를 내면서 내용을 지우는 세 경로를 닫는다.

**Architecture:** `patch.Op` 은 합집합 타입인데(Text 는 setText 전용, XML 은 replaceRaw 전용) 그 계약이 코드에 없다. `delete` 연산을 먼저 만들어 삭제할 정당한 길을 낸 뒤, `Text`·`XML` 을 `*string` 으로 바꿔 "필드 없음"과 "빈 값"을 구분하고, 연산과 필드가 맞는지 검사한다. 스플라이스 기계 자체는 손대지 않는다 — `delete` 는 `splice{span: n.Span, repl: nil}` 일 뿐이다.

**Tech Stack:** Go 1.26.5, 표준 라이브러리만

## Global Constraints

- **외부 의존성 0** — `go.mod` 에 `require` 블록이 없다. 새 의존성을 추가하지 않는다
- **재직렬화 금지** — `encoding/xml` 로 다시 쓰지 않는다. 원본 바이트를 잘라 붙이는 것만 한다
- **전부 아니면 전무 (P3)** — 거절이 하나라도 있으면 패키지는 손대지 않은 상태로 남고, CLI 는 출력 파일을 만들지 않는다
- **거절 문구는 형식 특정 요소명을 대지 않는다** — 같은 텍스트 요소가 docx 에서는 `w:t`, pptx 에서는 `a:t` 다. 구체 예가 필요하면 손에 든 노드의 `Type` 을 쓴다
- **reason 은 처방이 다르면 나눈다** — `reason` 은 에이전트의 다음 행동을 가르는 값이다
- **주석·오류 문구는 한국어**, 커밋 메시지는 conventional 접두사(영어) + 한국어 본문
- **테스트는 RED 를 먼저 본다** — 통과하는 걸 확인하기 전에 실패하는 걸 확인한다
- 검증 명령: `go test ./... -count=1`, `gofmt -l .`, `go vet ./...` — 셋 다 무음/통과여야 커밋

---

## File Structure

| 파일 | 책임 | 이 계획에서 |
|---|---|---|
| `internal/patch/patch.go` | 패치의 자료형과 JSON 계약 | `Op.Text`·`Op.XML` 을 `*string` 로, `Str` 헬퍼 추가 |
| `internal/patch/apply.go` | 검증 → 스플라이스 → 재스캔 | `checkFields` 추가, `delete` 분기, `delete_root` |
| `internal/patch/apply_test.go` | patch 계층 단위 시험 | Op 리터럴 갱신 + 새 테스트 |
| `internal/tmpl/extract.go` | 템플릿 역추출 | `Text:` 한 곳 갱신 |
| `internal/tmpl/fill.go` | 템플릿 되채우기 | `Text:` 한 곳 갱신 |
| `cmd/panto/main_test.go` | CLI 계층 시험 (exit 코드, 출력 파일) | 설계 §1 세 명령의 회귀 시험 |
| `README.md` | 사용자 문서 | 연산 목록, "다음 작업" 4번 문구 정정 |

---

## Task 1: `delete` 연산

`delete` 를 먼저 만든다. 이것 없이 §2.2(필드 정합)만 하면 삭제할 방법 자체가 사라진다.

**Files:**
- Modify: `internal/patch/apply.go` — `spliceOne` 의 `switch op.Op` (현재 173~226행)
- Modify: `internal/patch/patch.go:7` — `Op` 의 주석
- Test: `internal/patch/apply_test.go`

**Interfaces:**
- Consumes: `xmlscan.Node.Span`, `parts.Part.Root` (둘 다 `spliceOne` 안에서 이미 손에 있다)
- Produces: `{"op":"delete","part":…,"path":…}` — Task 3 의 README 와 다음 슬라이스(선택 블록)가 이 연산을 쓴다

- [ ] **Step 1: 실패하는 테스트 셋을 쓴다**

`internal/patch/apply_test.go` 끝에 붙인다.

```go
// TestDeleteRemovesNode 는 delete 가 노드를 통째로 지우고 형제는 남기는지 본다.
// 지운 뒤에는 경로 번호가 밀리므로 경로가 아니라 텍스트로 확인한다.
func TestDeleteRemovesNode(t *testing.T) {
	src := testutil.MinimalDocx([]string{"첫째", "둘째", "셋째"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "delete", Part: "word/document.xml", Path: "document/body[1]/p[2]"}},
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
	if bytes.Contains(content, []byte("둘째")) {
		t.Fatalf("지워지지 않았다: %s", content)
	}
	if !bytes.Contains(content, []byte("첫째")) || !bytes.Contains(content, []byte("셋째")) {
		t.Fatalf("형제가 함께 사라졌다: %s", content)
	}
	// 요소 껍데기까지 지워야 한다 — 텍스트만 비우면 빈 문단이 남는다.
	if bytes.Contains(content, []byte(`w14:paraId="00000002"`)) {
		t.Fatalf("요소가 남았다: %s", content)
	}
}

// TestDeleteRootRejected 는 루트 삭제를 사전에 거절하는지 본다.
//
// 막지 않아도 결과 재스캔이 잡지만, 그 이유는 invalid_xml("네가 준 XML 이
// 나쁘다")이다. delete 에는 사용자가 준 XML 이 없으니 틀린 이유를 대게 된다.
func TestDeleteRootRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "delete", Part: "word/document.xml", Path: "document"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "delete_root" {
		t.Fatalf("루트 삭제가 delete_root 로 거절되지 않았다: %+v", errs)
	}
}

// TestDeleteLocalityUntouchedEntriesAreByteIdentical 는 I2 국소성이 삭제에도
// 성립하는지 본다 — 손대지 않은 엔트리는 압축 데이터까지 같아야 한다.
func TestDeleteLocalityUntouchedEntriesAreByteIdentical(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p := open(t, src)

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "delete", Part: "word/document.xml", Path: "document/body[1]/p[2]"}},
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
				t.Fatal("삭제한 파트인데 압축 데이터가 그대로다")
			}
			continue
		}
		if !bytes.Equal(wantRaw, gotRaw) {
			t.Fatalf("안 건드린 엔트리의 압축 데이터가 달라졌다: %s", name)
		}
	}
}

// TestDeleteOverlappingSetTextRejected 는 지우는 서브트리 안을 동시에 고치는
// 패치를 겹침으로 거절하는지 본다. 순서에 따라 결과가 달라지므로 (I3) 둘 다
// 적용해선 안 된다.
func TestDeleteOverlappingSetTextRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목", "본문"}))

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{
			{Op: "delete", Part: "word/document.xml", Path: "document/body[1]/p[2]"},
			{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[2]/r[1]/t[1]", Text: "바뀜"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "overlap" {
		t.Fatalf("겹치는 delete+setText 가 거절되지 않았다: %+v", errs)
	}
}
```

- [ ] **Step 2: 실패를 확인한다**

```bash
go test ./internal/patch/ -run 'TestDelete' -count=1
```

기대: 넷 다 FAIL. `TestDeleteRemovesNode` 는 `unknown_op` 에러로, `TestDeleteRootRejected` 는 `unknown_op` 로(delete_root 가 아니라), 나머지 둘도 `unknown_op` 로 실패한다.

- [ ] **Step 3: `delete` 분기를 넣는다**

`internal/patch/apply.go` 의 `spliceOne` 안 `switch op.Op` 에 `case` 를 더한다. `case "replaceRaw":` 바로 뒤에 넣는다:

```go
		case "delete":
			// 루트를 지우면 파트가 XML 선언만 남은 파일이 된다. 재스캔이 잡긴
			// 하지만 그 이유(invalid_xml)는 "네가 준 XML 이 나쁘다"는 뜻이라
			// 사용자가 준 XML 이 없는 delete 에는 틀린 진단이다.
			if op.Path == part.Root {
				errs = append(errs, Error{
					Path:   op.Path,
					Reason: "delete_root",
					Detail: fmt.Sprintf("루트 노드 %s 는 지울 수 없다 — 파트가 빈 파일이 된다", part.Root),
				})
				continue
			}
			// 요소 전체를 폭 0 으로 치환한다. 앞뒤 공백·개행은 건드리지 않는다 —
			// 인접 바이트를 먹으면 어디까지 지웠는지가 흐려지고 I2 논증이 약해진다.
			splices = append(splices, splice{span: n.Span, repl: nil, path: op.Path})
```

`unknown_op` 의 `Detail` 도 새 연산을 포함하도록 고친다 (현재 224행):

```go
				Detail: fmt.Sprintf("알 수 없는 연산: %s (setText | replaceRaw | delete)", op.Op),
```

`internal/patch/patch.go:7` 의 주석도 고친다:

```go
// Op 는 패치 연산 하나다. 연산은 setText·replaceRaw·delete 셋이다.
```

- [ ] **Step 4: 통과를 확인한다**

```bash
go test ./internal/patch/ -run 'TestDelete' -count=1
go test ./... -count=1
gofmt -l . && go vet ./...
```

기대: 전부 통과, `gofmt`·`vet` 무음.

- [ ] **Step 5: 커밋**

```bash
git add internal/patch/apply.go internal/patch/patch.go internal/patch/apply_test.go
git commit -m "feat: delete 연산 — 노드를 명시적으로 지운다

루트 삭제는 사전에 거절한다. 막지 않아도 재스캔이 잡지만 그 이유가
invalid_xml 이라, 사용자가 준 XML 이 없는 delete 에는 틀린 진단이 된다."
```

---

## Task 2: `Text`·`XML` 을 `*string` 으로 — 빠뜨린 필드를 잡는다

설계 §1 의 증상 1·2 를 닫는다.

**Files:**
- Modify: `internal/patch/patch.go:10-16` — `Op` 구조체, `Str` 헬퍼 추가
- Modify: `internal/patch/apply.go` — `checkFields` 추가, `*op.Text`·`*op.XML` 역참조
- Modify: `internal/tmpl/extract.go:159-160`
- Modify: `internal/tmpl/fill.go:114`
- Test: `internal/patch/apply_test.go`, `cmd/panto/main_test.go`

**Interfaces:**
- Produces: `patch.Str(v string) *string` — Task 3 과 다른 패키지가 `Op` 를 코드로 조립할 때 쓴다
- Produces: `checkFields(op Op) *Error` — Task 3 이 여기에 `unused_field` 를 더한다

- [ ] **Step 1: 실패하는 테스트를 쓴다**

**이 단계의 테스트는 `Text: "…"` / `XML: ""` 처럼 오늘의 값 문법으로 쓴다.** `patch.Str` 을 쓰면 타입을 바꾸기 전에는 컴파일이 안 되고, 컴파일이 막히면 같은 패키지의 다른 RED 도 관측할 수 없다. Step 4 의 기계적 변환이 이것들도 함께 포인터로 바꾼다.

`internal/patch/apply_test.go` 끝에 붙인다.

```go
// TestSetTextWithoutTextRejected 는 text 를 빠뜨린 setText 가 거절되는지 본다.
// 포인터로 바꾸기 전에는 nil 과 "" 가 같은 제로값이라, 빠뜨린 패치가 조용히
// 텍스트를 지우고 ok:true 를 냈다 (설계 §1 증상 3 의 다른 얼굴).
func TestSetTextWithoutTextRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"지켜져야 할 텍스트"}))

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "setText", Part: "word/document.xml", Path: "document/body[1]/p[1]/r[1]/t[1]"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "missing_text" {
		t.Fatalf("text 없는 setText 가 거절되지 않았다: %+v", errs)
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("지켜져야 할 텍스트")) {
		t.Fatalf("거절됐는데 텍스트가 사라졌다: %s", content)
	}
}

// TestSetTextWithEmptyStringAccepted 는 빈 문자열이 정당한 값으로 남는지 본다.
// 양식 필드를 비우는 것은 정당한 요구이고, 이걸 막으면 빠뜨림을 잡는 대가로
// 정상 기능을 잃는다.
//
// **이 테스트에는 RED 가 없다** — 오늘도 통과하고 고친 뒤에도 통과해야 한다.
// 과잉 교정을 막는 경계 시험이다: "빈 값 금지"로 문제를 풀려는 구현은 여기서
// 걸린다. 그것이 이 테스트가 지키는 유일한 것이고, 그래서 여기 있다.
func TestSetTextWithEmptyStringAccepted(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"지워질 텍스트"}))

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{{Op: "setText", Part: "word/document.xml",
			Path: "document/body[1]/p[1]/r[1]/t[1]", Text: ""}},
	})
	if err != nil || len(errs) != 0 {
		t.Fatalf("빈 문자열이 거절됐다: err=%v errs=%+v", err, errs)
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if bytes.Contains(content, []byte("지워질 텍스트")) {
		t.Fatalf("빈 문자열이 적용되지 않았다: %s", content)
	}
	// 요소는 남아야 한다 — 텍스트를 비우는 것이지 노드를 지우는 게 아니다.
	if !bytes.Contains(content, []byte(`w14:paraId="00000001"`)) {
		t.Fatalf("요소까지 사라졌다: %s", content)
	}
}

// TestReplaceRawWithoutXMLRejected 는 xml 을 빠뜨린 replaceRaw 가 거절되는지
// 본다. 설계 §1 증상 1 — 오늘은 노드가 통째로 사라지고 ok:true 가 난다.
func TestReplaceRawWithoutXMLRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목", "지켜져야 할 문단"}))

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops:  []patch.Op{{Op: "replaceRaw", Part: "word/document.xml", Path: "document/body[1]/p[2]"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "missing_xml" {
		t.Fatalf("xml 없는 replaceRaw 가 거절되지 않았다: %+v", errs)
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("지켜져야 할 문단")) {
		t.Fatalf("거절됐는데 문단이 사라졌다: %s", content)
	}
}

// TestReplaceRawWithEmptyXMLRejected 는 xml 이 빈 문자열인 replaceRaw 가
// 거절되고, 안내가 delete 를 가리키는지 본다. 설계 §1 증상 2.
//
// missing_xml 과 이유를 나누는 까닭: 전자는 "내용을 빠뜨렸다", 후자는
// "지우려 했다"로 의도가 다르고 처방이 다르다.
func TestReplaceRawWithEmptyXMLRejected(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목", "지켜져야 할 문단"}))

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{{Op: "replaceRaw", Part: "word/document.xml",
			Path: "document/body[1]/p[2]", XML: ""}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "empty_xml" {
		t.Fatalf("빈 xml 이 empty_xml 로 거절되지 않았다: %+v", errs)
	}
	if !strings.Contains(errs[0].Detail, "delete") {
		t.Fatalf("안내가 delete 를 가리키지 않는다: %q", errs[0].Detail)
	}
	content, err := p.Part("word/document.xml")
	if err != nil {
		t.Fatalf("Part: %v", err)
	}
	if !bytes.Contains(content, []byte("지켜져야 할 문단")) {
		t.Fatalf("거절됐는데 문단이 사라졌다: %s", content)
	}
}
```

`internal/patch/apply_test.go` 의 import 에 `"strings"` 가 없으면 더한다.

CLI 계층 회귀도 함께 쓴다 — `cmd/panto/main_test.go` 끝에 붙인다. 설계 §1 이 CLI 로 측정한 증상이므로 CLI 로 잠근다.

```go
// TestApplyRejectsRawWithoutXML 은 설계 §1 증상 1 의 회귀 시험이다.
//
// 측정된 사실: {"op":"replaceRaw","path":…} (xml 없음) 이 ok:true 를 내면서
// 문단을 지웠다. exit 0 이라 호출자는 성공으로 읽고 넘어간다.
func TestApplyRejectsRawWithoutXML(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.docx")
	src := testutil.MinimalDocx([]string{"제목", "지켜져야 할 문단"})
	if err := os.WriteFile(inPath, src, 0o644); err != nil {
		t.Fatalf("입력 파일 쓰기 실패: %v", err)
	}
	patchPath := filepath.Join(dir, "patch.json")
	bad := `{"ops":[{"op":"replaceRaw","path":"document/body[1]/p[2]"}]}`
	if err := os.WriteFile(patchPath, []byte(bad), 0o644); err != nil {
		t.Fatalf("패치 파일 쓰기 실패: %v", err)
	}
	outPath := filepath.Join(dir, "out.docx")

	code := cmdApply([]string{inPath, "-p", patchPath, "-o", outPath})
	if code != exitInput {
		t.Fatalf("xml 없는 replaceRaw 인데 exit=%d (기대 %d)", code, exitInput)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("거절된 패치가 출력 파일을 만들었다: %v", err)
	}
}

// TestApplyRejectsRawWithEmptyXML 은 설계 §1 증상 2 의 회귀 시험이다.
func TestApplyRejectsRawWithEmptyXML(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.docx")
	src := testutil.MinimalDocx([]string{"제목", "지켜져야 할 문단"})
	if err := os.WriteFile(inPath, src, 0o644); err != nil {
		t.Fatalf("입력 파일 쓰기 실패: %v", err)
	}
	patchPath := filepath.Join(dir, "patch.json")
	bad := `{"ops":[{"op":"replaceRaw","path":"document/body[1]/p[2]","xml":""}]}`
	if err := os.WriteFile(patchPath, []byte(bad), 0o644); err != nil {
		t.Fatalf("패치 파일 쓰기 실패: %v", err)
	}
	outPath := filepath.Join(dir, "out.docx")

	code := cmdApply([]string{inPath, "-p", patchPath, "-o", outPath})
	if code != exitInput {
		t.Fatalf("빈 xml 인 replaceRaw 인데 exit=%d (기대 %d)", code, exitInput)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("거절된 패치가 출력 파일을 만들었다: %v", err)
	}
}
```

- [ ] **Step 2: 실패를 확인한다**

```bash
go test ./internal/patch/ -run 'TestSetTextWithout|TestSetTextWithEmpty|TestReplaceRawWithout|TestReplaceRawWithEmpty' -count=1
go test ./cmd/panto/ -run 'TestApplyRejectsRaw' -count=1
```

기대 — 전부 컴파일되고 다음처럼 실패한다:

| 테스트 | 기대 RED |
|---|---|
| `TestSetTextWithoutTextRejected` | FAIL — 거절이 없고 "지켜져야 할 텍스트"가 사라진다 |
| `TestReplaceRawWithoutXMLRejected` | FAIL — 거절이 없고 문단이 사라진다 |
| `TestReplaceRawWithEmptyXMLRejected` | FAIL — 거절이 없고 문단이 사라진다 |
| `TestApplyRejectsRawWithoutXML` | FAIL — `exit=0`, 출력 파일이 만들어진다 |
| `TestApplyRejectsRawWithEmptyXML` | FAIL — `exit=0`, 출력 파일이 만들어진다 |
| `TestSetTextWithEmptyStringAccepted` | **PASS** — RED 가 없는 경계 시험이다 (위 주석 참조) |

여섯 중 다섯이 실패해야 다음으로 간다. 하나라도 예상과 다르게 통과하면 그 테스트는 아무것도 지키지 않는 것이므로, 진행하지 말고 왜 통과했는지 밝힌다.

- [ ] **Step 3: 자료형을 바꾼다 (Step 3~5 는 한 덩어리다)**

> **이 셋은 나눌 수 없다.** 자료형만 바꾸면 `*op.Text` 가 nil 역참조로 **패닉**하고, 호출부를 안 고치면 컴파일이 안 된다. Step 3·4·5 를 하나의 작업으로 수행하고 Step 7 에서 한 번 검증한다. 중간 상태를 커밋하지 않는다.

먼저 자료형:

`internal/patch/patch.go` 의 `Op` 를 고친다:

```go
// Op 는 패치 연산 하나다. 연산은 setText·replaceRaw·delete 셋이다.
// Part 는 물리 파트 경로("ppt/slides/slide1.xml") 또는 논리 참조("pptx/slide[1]")다.
// 비어 있으면 본문 파트가 하나인 문서에 한해 그것으로 간주한다 — docx 하위호환.
//
// Text 와 XML 이 포인터인 이유: **"필드가 없다"(nil)와 "빈 값을 준다"("")를
// 구분해야 한다.** 값 타입이면 둘이 같은 제로값이 되어, text 를 빠뜨린 setText
// 가 조용히 텍스트를 지우고 ok:true 를 낸다 (설계 §1). 빈 문자열 자체는
// 정당한 값이므로 "빈 값 금지"로는 풀 수 없고, 존재 여부를 표현해야 한다.
type Op struct {
	Op   string  `json:"op"`
	Part string  `json:"part,omitempty"`
	Path string  `json:"path"`
	Text *string `json:"text,omitempty"` // setText 전용
	XML  *string `json:"xml,omitempty"`  // replaceRaw 전용
}

// Str 은 문자열 포인터를 만든다. Go 는 문자열 리터럴의 주소를 못 얻으므로
// Op 를 코드에서 조립할 때 필요하다.
func Str(v string) *string { return &v }
```

- [ ] **Step 4: 호출부를 고친다**

프로덕션 두 곳:

`internal/tmpl/extract.go:159-160` — 현재:

```go
						ops = append(ops, patch.Op{Op: "setText", Part: pt.Name,
							Path: n.Path, Text: "{{" + key + "}}"})
```

고친 뒤:

```go
						ops = append(ops, patch.Op{Op: "setText", Part: pt.Name,
							Path: n.Path, Text: patch.Str("{{" + key + "}}")})
```

`internal/tmpl/fill.go:114` — 현재:

```go
		ops = append(ops, patch.Op{Op: "setText", Part: partName, Path: k.Path, Text: v})
```

고친 뒤:

```go
		ops = append(ops, patch.Op{Op: "setText", Part: partName, Path: k.Path, Text: patch.Str(v)})
```

테스트의 `Op` 리터럴은 기계적으로 바꾼다 — `Text: "…"` → `Text: patch.Str("…")`, `XML: "…"` → `XML: patch.Str("…")`. Task 1 과 이 태스크 Step 1 이 더한 것까지 포함해 30곳 안팎이다. `internal/patch/apply_test.go` 의 패키지는 `patch_test` 이므로 `patch.Str` 로 부른다.

남은 곳은 컴파일러가 전부 짚어준다 — 하나도 놓칠 수 없다:

```bash
go build ./... && go vet ./...
```

`cannot use "…" (untyped string constant) as *string value` 가 나오는 파일·행을 차례로 고친다.

- [ ] **Step 5: 필드 정합 검사를 넣는다**

`internal/patch/apply.go` 에 `spliceOne` 바로 위에 함수를 더한다:

```go
// checkFields 는 연산과 필드가 맞는지 본다.
//
// Op 는 합집합 타입이다 — Text 는 setText 전용, XML 은 replaceRaw 전용이고
// delete 는 둘 다 쓰지 않는다. 이 계약을 검사하지 않으면 필드를 잘못 고르거나
// 빠뜨린 패치가 조용히 내용을 지운다 (설계 §1).
func checkFields(op Op) *Error {
	switch op.Op {
	case "setText":
		if op.Text == nil {
			return &Error{Path: op.Path, Reason: "missing_text",
				Detail: "setText 에 text 가 없다"}
		}
	case "replaceRaw":
		if op.XML == nil {
			return &Error{Path: op.Path, Reason: "missing_xml",
				Detail: "replaceRaw 에 xml 이 없다"}
		}
		if *op.XML == "" {
			return &Error{Path: op.Path, Reason: "empty_xml",
				Detail: "replaceRaw 의 xml 이 비었다 — 노드를 지우려면 delete 를 쓸 것"}
		}
	}
	return nil
}
```

`spliceOne` 안, `seen[op.Path] = true` 바로 다음에 끼운다:

```go
		// 필드 정합을 경로 조회보다 먼저 본다. 필드가 틀렸다는 건 경로가
		// 존재하든 말든 사실이고, 경로까지 틀린 패치에서 path_not_found 만
		// 보여주면 사용자가 경로를 고친 뒤 두 번째 오류를 만난다.
		if e := checkFields(op); e != nil {
			errs = append(errs, *e)
			continue
		}
```

`switch op.Op` 안의 두 역참조를 고친다:

```go
		case "replaceRaw":
			splices = append(splices, splice{span: n.Span, repl: []byte(*op.XML), path: op.Path})
```

`setText` 분기의 `op.Text` 세 곳(공백 검사 두 곳, 스플라이스 하나)을 `*op.Text` 로 바꾼다:

```go
			if strings.TrimSpace(*op.Text) != *op.Text {
```

```go
			splices = append(splices, splice{
				span: n.Inner,
				repl: []byte(xmlEscaper.Replace(*op.Text)),
				path: op.Path,
			})
```

`apply.go:17` 의 주석에서 `op.Text` 를 `*op.Text` 로 읽히게 다듬는다 (내용은 그대로).

- [ ] **Step 6: 낡아진 주석을 고친다**

`cmd/panto/main_test.go` 의 `TestApplyRejectsUnknownPatchField` 위 주석은 **"막을 수 있는 지점은 디코드뿐이다"** 로 끝난다. 이 변경이 그 문장을 거짓으로 만든다 — 이제 검증 계층이 `missing_text` 로 잡는다. 그 마지막 문장을 이렇게 바꾼다:

```go
// 텍스트를 지웠다. 빈 텍스트 자체는 정당한 연산이라 결과만 봐서는 오타와
// 구별되지 않는다. 디코드에서 모르는 필드를 막고(여기), 검증에서 빠뜨린
// 필드를 막는다(patch.checkFields) — 두 겹이다.
```

- [ ] **Step 7: 통과를 확인한다**

```bash
go test ./... -count=1
gofmt -l . && go vet ./...
```

기대: 전부 통과, 무음.

- [ ] **Step 8: 커밋**

```bash
git add internal/patch internal/tmpl cmd/panto/main_test.go
git commit -m "fix: 빠뜨린 필드가 조용히 내용을 지우던 결함 수정

Text·XML 을 포인터로 바꿔 '필드 없음'과 '빈 값'을 구분한다. 값 타입일 때는
둘이 같은 제로값이라, xml 을 빠뜨린 replaceRaw 가 노드를 통째로 지우면서
ok:true 를 냈다. 빈 문자열은 정당한 값이므로 존재 여부를 표현해야 풀린다."
```

---

## Task 3: `unused_field` — 잘못 고른 필드를 잡는다

설계 §1 증상 3 을 닫는다. PR #3 이 `DisallowUnknownFields` 로 *모르는* 키를 막았지만, `xml` 은 `Op` 가 아는 키라 그대로 통과한다.

**Files:**
- Modify: `internal/patch/apply.go` — `checkFields`
- Test: `internal/patch/apply_test.go`, `cmd/panto/main_test.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `checkFields(op Op) *Error` (Task 2), `patch.Str` (Task 2), `delete` 연산 (Task 1)

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/patch/apply_test.go` 끝에 붙인다.

```go
// TestUnusedFieldRejected 는 연산이 쓰지 않는 필드가 오면 거절하는지 본다.
//
// setText 에 xml 을 준 첫 케이스가 설계 §1 증상 3 이다 — 오늘은 xml 이 조용히
// 무시되고 text 가 빈 값으로 적용되어 텍스트가 사라진다.
func TestUnusedFieldRejected(t *testing.T) {
	cases := []struct {
		name string
		op   patch.Op
	}{
		{"setText 에 xml", patch.Op{Op: "setText", Part: "word/document.xml",
			Path: "document/body[1]/p[1]/r[1]/t[1]", XML: patch.Str(`<w:t>바뀜</w:t>`)}},
		{"replaceRaw 에 text", patch.Op{Op: "replaceRaw", Part: "word/document.xml",
			Path: "document/body[1]/p[1]", Text: patch.Str("바뀜")}},
		{"delete 에 text", patch.Op{Op: "delete", Part: "word/document.xml",
			Path: "document/body[1]/p[1]", Text: patch.Str("바뀜")}},
		{"delete 에 xml", patch.Op{Op: "delete", Part: "word/document.xml",
			Path: "document/body[1]/p[1]", XML: patch.Str(`<w:p/>`)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := open(t, testutil.MinimalDocx([]string{"지켜져야 할 텍스트"}))
			errs, err := patch.Apply(p, patch.Patch{Hash: p.Hash, Ops: []patch.Op{c.op}})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if len(errs) != 1 || errs[0].Reason != "unused_field" {
				t.Fatalf("안 쓰는 필드가 거절되지 않았다: %+v", errs)
			}
			content, err := p.Part("word/document.xml")
			if err != nil {
				t.Fatalf("Part: %v", err)
			}
			if !bytes.Contains(content, []byte("지켜져야 할 텍스트")) {
				t.Fatalf("거절됐는데 내용이 바뀌었다: %s", content)
			}
		})
	}
}

// TestUnusedFieldWinsOverMissingField 는 두 결함이 겹칠 때 어느 이유를 내는지
// 고정한다 (설계 §3.1).
//
// {"op":"setText","xml":…} 는 text 도 없고 안 쓰는 필드도 있다. 사용자가 실제로
// 한 실수는 "필드를 잘못 골랐다"이지 "값을 빠뜨렸다"가 아니므로 unused_field 를
// 낸다. missing_text 를 내면 사용자는 text 를 더하고 xml 은 그대로 둔 채
// 두 번째 오류를 만난다.
func TestUnusedFieldWinsOverMissingField(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"지켜져야 할 텍스트"}))

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{{Op: "setText", Part: "word/document.xml",
			Path: "document/body[1]/p[1]/r[1]/t[1]", XML: patch.Str(`<w:t>바뀜</w:t>`)}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "unused_field" {
		t.Fatalf("겹친 결함에서 unused_field 가 아니다: %+v", errs)
	}
}

// TestUnusedFieldCheckedBeforePathLookup 은 경로까지 틀린 패치에서 필드 오류를
// 먼저 내는지 본다 (설계 §3.1). 경로 오류를 먼저 내면 사용자는 경로를 고친 뒤
// 두 번째 오류를 만난다.
func TestUnusedFieldCheckedBeforePathLookup(t *testing.T) {
	p := open(t, testutil.MinimalDocx([]string{"제목"}))

	errs, err := patch.Apply(p, patch.Patch{
		Hash: p.Hash,
		Ops: []patch.Op{{Op: "setText", Part: "word/document.xml",
			Path: "document/body[1]/p[99]/r[1]/t[1]", XML: patch.Str(`<w:t>바뀜</w:t>`)}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(errs) != 1 || errs[0].Reason != "unused_field" {
		t.Fatalf("경로 오류보다 필드 오류가 먼저 나오지 않았다: %+v", errs)
	}
}
```

CLI 회귀 — `cmd/panto/main_test.go` 끝에 붙인다:

```go
// TestApplyRejectsSetTextWithXMLField 는 설계 §1 증상 3 의 회귀 시험이다.
//
// 측정된 사실: {"op":"setText","path":…,"xml":…} 가 ok:true 를 내면서 대상
// 텍스트를 지웠다. PR #3 의 DisallowUnknownFields 는 모르는 키만 막고, xml 은
// Op 가 아는 키라 그대로 통과했다.
func TestApplyRejectsSetTextWithXMLField(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.docx")
	src := testutil.MinimalDocx([]string{"지켜져야 할 텍스트"})
	if err := os.WriteFile(inPath, src, 0o644); err != nil {
		t.Fatalf("입력 파일 쓰기 실패: %v", err)
	}
	patchPath := filepath.Join(dir, "patch.json")
	bad := `{"ops":[{"op":"setText","path":"document/body[1]/p[1]/r[1]/t[1]","xml":"<w:t>바뀜</w:t>"}]}`
	if err := os.WriteFile(patchPath, []byte(bad), 0o644); err != nil {
		t.Fatalf("패치 파일 쓰기 실패: %v", err)
	}
	outPath := filepath.Join(dir, "out.docx")

	code := cmdApply([]string{inPath, "-p", patchPath, "-o", outPath})
	if code != exitInput {
		t.Fatalf("setText 에 xml 을 준 패치인데 exit=%d (기대 %d)", code, exitInput)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("거절된 패치가 출력 파일을 만들었다: %v", err)
	}
}
```

- [ ] **Step 2: 실패를 확인한다**

```bash
go test ./internal/patch/ -run 'TestUnusedField' -count=1
go test ./cmd/panto/ -run 'TestApplyRejectsSetTextWithXMLField' -count=1
```

기대: 전부 FAIL. `setText 에 xml` 케이스는 `missing_text` 를 내고(Task 2 가 넣은 검사), 나머지는 거절 없이 적용된다.

- [ ] **Step 3: `checkFields` 에 안 쓰는 필드 검사를 더한다**

Task 2 에서 만든 `checkFields` 를 고친다. **안 쓰는 필드를 빠뜨린 필드보다 먼저 본다** (설계 §3.1):

```go
// checkFields 는 연산과 필드가 맞는지 본다.
//
// Op 는 합집합 타입이다 — Text 는 setText 전용, XML 은 replaceRaw 전용이고
// delete 는 둘 다 쓰지 않는다. 이 계약을 검사하지 않으면 필드를 잘못 고르거나
// 빠뜨린 패치가 조용히 내용을 지운다 (설계 §1).
//
// 순서가 진단의 품질이다: 안 쓰는 필드를 먼저 본다. setText 에 xml 만 준
// 입력은 "값을 빠뜨렸다"가 아니라 "필드를 잘못 골랐다"이기 때문이다 (설계 §3.1).
func checkFields(op Op) *Error {
	switch op.Op {
	case "setText":
		if op.XML != nil {
			return &Error{Path: op.Path, Reason: "unused_field",
				Detail: "setText 는 xml 을 쓰지 않는다 — 텍스트는 text 에 준다"}
		}
		if op.Text == nil {
			return &Error{Path: op.Path, Reason: "missing_text",
				Detail: "setText 에 text 가 없다"}
		}
	case "replaceRaw":
		if op.Text != nil {
			return &Error{Path: op.Path, Reason: "unused_field",
				Detail: "replaceRaw 는 text 를 쓰지 않는다 — 마크업은 xml 에 준다"}
		}
		if op.XML == nil {
			return &Error{Path: op.Path, Reason: "missing_xml",
				Detail: "replaceRaw 에 xml 이 없다"}
		}
		if *op.XML == "" {
			return &Error{Path: op.Path, Reason: "empty_xml",
				Detail: "replaceRaw 의 xml 이 비었다 — 노드를 지우려면 delete 를 쓸 것"}
		}
	case "delete":
		if op.Text != nil || op.XML != nil {
			return &Error{Path: op.Path, Reason: "unused_field",
				Detail: "delete 는 text·xml 을 쓰지 않는다 — 지울 노드는 path 로만 지목한다"}
		}
	}
	return nil
}
```

- [ ] **Step 4: 통과를 확인한다**

```bash
go test ./... -count=1
gofmt -l . && go vet ./...
```

- [ ] **Step 5: README 를 고친다**

`README.md` 61행("다음 작업" 4번)이 지금은 이렇게 되어 있다:

```markdown
4. **선택적 블록** — 구조가 다른 문서 간 템플릿 추출은 공통부까지 됐다(`--allow-unrepresented`). "이 문단은 어떤 문서엔 있고 어떤 문서엔 없다"를 담으려면 `patch`에 insert/delete 연산이 필요하다
```

실측 결과 이 문장은 절반이 틀렸다 — 삭제도 삽입도 이미 표현 가능했고, 없던 것은 연산이 아니라 계약이었다. 이렇게 바꾼다:

```markdown
4. **선택적 블록** — 구조가 다른 문서 간 템플릿 추출은 공통부까지 됐다(`--allow-unrepresented`). "이 문단은 어떤 문서엔 있고 어떤 문서엔 없다"를 담는 다음 단계. `delete` 연산은 준비됐다([명시적 삭제 설계](docs/superpowers/specs/2026-08-12-explicit-delete-design.md)) — base 를 상위집합으로 두고 블록을 끄는 것이 삭제 하나로 표현된다
```

`README.md` 69행의 괄호도 사실과 맞춘다. 현재:

```markdown
- **슬라이드 추가·삭제·재정렬은 범위 밖이다.** `presentation.xml` 과 그 rels 를 함께 고쳐야 해서 제자리 변경이 아니다 — 선행 슬라이스가 `insert`/`delete` 연산을 뺀 것과 같은 이유다 (multipart spec §12)
```

바꾼 뒤:

```markdown
- **슬라이드 추가·삭제·재정렬은 범위 밖이다.** `presentation.xml` 과 그 rels 를 함께 고쳐야 해서 제자리 변경이 아니다. `delete` 연산은 **파트 안의 노드만** 지운다 — 지워진 노드가 참조하던 관계와 미디어 파트는 남는다 (multipart spec §12)
```

`README.md` 의 "알려진 한계" 목록에 한 줄 더한다:

```markdown
- **`delete` 는 스키마를 검증하지 않는다.** `<w:body>` 를 지우면 결과는 well-formed 이지만 Word 가 열지 못할 수 있다. panto 는 바이트 하네스지 스키마 검증기가 아니다 — 재스캔이 통과하면 통과시킨다
```

- [ ] **Step 6: 커밋**

```bash
git add internal/patch cmd/panto/main_test.go README.md
git commit -m "fix: 잘못 고른 필드가 조용히 내용을 지우던 결함 수정

setText 에 xml 을 주면 xml 이 무시되고 텍스트가 빈 값으로 적용돼 사라졌다.
PR #3 의 DisallowUnknownFields 는 모르는 키만 막고 xml 은 Op 가 아는 키다.
겹친 결함에서는 unused_field 를 낸다 — 사용자가 한 실수는 값을 빠뜨린 것이
아니라 필드를 잘못 고른 것이다."
```

---

## Self-Review

**1. 스펙 커버리지**

| 스펙 | 태스크 |
|---|---|
| §2.1 `delete` 연산 | Task 1 |
| §2.2 `*string` 전환 + 판정표 6행 | Task 2 (4행) + Task 3 (2행: unused_field) |
| §3 이유 5개 | `delete_root`(T1), `missing_text`·`missing_xml`·`empty_xml`(T2), `unused_field`(T3) |
| §3.1 검사 순서 | Task 2 Step 5(경로 조회보다 먼저), Task 3 Step 3(안 쓰는 필드 먼저) + 전용 테스트 2개 |
| §4 P1 명시성 | Task 2·3 의 CLI 회귀 3개 — §1 세 명령 그대로 |
| §4 P2 필드 정합 | Task 2·3 단위 테스트 |
| §4 P3 전부 아니면 전무 | CLI 테스트가 출력 파일 미생성을 본다 |
| §4 I2 국소성 | Task 1 `TestDeleteLocality…` |
| §5 한계 | Task 3 Step 5 README |
| §6 영향 파일 7개 | 전부 등장 |

**2. 자리표시자 스캔** — 없음. 모든 코드 단계에 실제 코드가 있다.

**3. 타입 정합** — `patch.Str(v string) *string` 은 Task 2 Step 3 에서 정의되고 Task 2 Step 1·4, Task 3 Step 1 에서 쓰인다. `checkFields(op Op) *Error` 는 Task 2 Step 5 에서 정의되고 Task 3 Step 3 에서 확장된다. `delete_root` 는 Task 1 에서만 쓰인다. `Op.Text`·`Op.XML` 의 `*string` 은 Task 2 이후 모든 곳에서 일관된다.

**4. RED 관측 가능성** — 이 계획을 쓰면서 한 번 틀렸던 지점이라 남긴다. Task 2 의 테스트를 `patch.Str` 로 쓰면 타입을 바꾸기 전에는 **컴파일이 안 되고**, 컴파일이 막히면 같은 패키지의 다른 RED 도 관측할 수 없다. 그래서 Task 2 Step 1 의 테스트는 오늘의 값 문법(`Text: "…"`, `XML: ""`)으로 쓰고, Step 4 의 기계적 변환이 함께 바꾼다. Task 3 의 테스트는 Task 2 이후라 `patch.Str` 을 바로 쓴다.

**5. 나눌 수 없는 단계** — Task 2 의 Step 3~5(자료형·호출부·검사)는 하나로 묶여 있다. 자료형만 바꾸면 `*op.Text` 가 nil 역참조로 패닉하고, 호출부를 안 고치면 컴파일이 안 된다. 중간 상태를 커밋하지 않는다.
