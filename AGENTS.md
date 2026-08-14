# Pantograph

OOXML(docx·pptx) 문서를 **바이트 단위로 재현**하고, 재현에 성공했는지 기계가 판정하게 하는 하네스.

핵심 주장은 하나다: **도구가 실패를 성공이라 말하면 안 된다.** 이 저장소의 결함 대부분은 "기능이 없다"가 아니라 "없는데 있다고 보고한다"였다.

## Tech Stack

- **언어**: Go 1.26.5
- **의존성**: **없음** — `go.mod` 에 `require` 블록이 없다. 새 의존성을 추가하지 않는다
- **테스트**: 표준 `testing`. 픽스처는 `testdata/`
- **CLI**: `cmd/panto` — `dump` / `diff` / `apply` / `tmpl`

## Commands

```bash
go test ./... -count=1     # 전체 (10 패키지)
gofmt -l .                 # 무음이어야 한다
go vet ./...               # 무음이어야 한다
go build -o /tmp/panto ./cmd/panto
```

**세 명령이 전부 깨끗해야 커밋한다.**

## Project Structure

```
pantograph/
├── cmd/panto/          CLI. 종료 코드 0=성공 1=입력오류 2=내부오류
├── internal/
│   ├── xmlscan/        XML 을 훑어 노드의 바이트 주소를 낸다. 의존 없음
│   ├── opc/            zip 컨테이너. 바이트 정확 라이터 + 자체검사 게이트
│   ├── parts/          본문 파트 계획. **형식 지식의 유일한 소재지**
│   ├── patch/          경로 지정 패치 → 바이트 스플라이스
│   ├── align/          계층적 LCS 정렬. diff·tmpl 이 공유
│   ├── diff/           경로 단위 차이 측정
│   ├── tmpl/           같은 양식 N벌에서 {{key}} 템플릿 역추출·되채우기
│   ├── dump/           구조 덤프
│   └── testutil/       결정론적 픽스처 생성기
├── testdata/
│   ├── real/           **같은 양식** 문서 N벌 — 왕복·정렬·역추출 시험용
│   └── design/         서로 다른 **디자인 견본** — 서식 수확용
└── docs/superpowers/
    ├── specs/          설계 문서. 슬라이스마다 하나
    └── plans/          구현 계획
```

**의존 방향**: `xmlscan` → 없음 / `parts` → opc·xmlscan / `align` → parts·xmlscan / `patch`·`diff`·`tmpl` → 그 위. 순환 없음.

## 이 프로젝트의 불변식

전역 규칙(`~/.Codex/rules/`)에 없는, **이 저장소 고유의 계약**이다.

### 설계 제약

- **재직렬화 금지** — `encoding/xml` 로 XML 을 **다시 쓰지 않는다**. 파싱은 바이트 주소를 구하려는 것이고, 편집은 원본 바이트를 잘라 붙이는 것뿐이다. `encoding/xml` 은 네임스페이스를 망가뜨린다. 그래서 `internal/xmlscan` 에는 재직렬화 함수가 **의도적으로 없다**. 읽기·토큰화는 괜찮다
- **형식 지식은 `internal/parts` 에만** — 다른 패키지는 docx 와 pptx 를 구분하지 못해야 한다. 거절 문구가 `w:t`·`a:t` 같은 형식 특정 요소명을 대면 안 된다. 구체 예가 필요하면 손에 든 노드의 `Type` 을 쓴다
- **외부 의존성 0**

### 동작 계약

- **I1 항등** — 빈 패치는 바이트 동일한 문서를 낸다
- **I2 국소성** — 손대지 않은 zip 엔트리는 **압축 바이트까지** 동일하다
- **I3 결정성** — 같은 입력이면 항상 같은 바이트
- **I4a 템플릿 가역성** — 원래 값으로 되채우면 원본과 같다. **파트 내용 기준**이지 파일 전체 바이트가 아니다(수정된 파트는 재압축되므로 컨테이너 크기가 달라진다)
- **전부 아니면 전무** — 거절이 하나라도 있으면 패키지는 손대지 않은 상태로 남고, CLI 는 **출력 파일을 만들지 않는다**

### 오류 보고

- **`reason` 은 처방이 다르면 나눈다.** `reason` 은 사람이 읽는 문구가 아니라 **에이전트의 다음 행동을 가르는 값**이다. 같은 처방이면 같은 이유를 쓰고, 다른 처방이면 반드시 나눈다
- **종료 코드로 재시도 여부가 갈린다.** 1=입력 오류(패치를 고쳐 다시 시도) / 2=내부 오류(도구가 고장). 입력 결함을 2로 보내면 에이전트가 잘못 분기한다
- 현재 어휘: `hash_mismatch` `part_not_found` `path_not_found` `duplicate_path` `overlap` `unknown_op` `unused_field` `missing_text` `missing_xml` `empty_xml` `unbalanced_xml` `incomplete_xml` `delete_root` `empty_part` `type_mismatch` `self_closing_target` `whitespace_needs_preserve` `invalid_xml` `nontext_diff` `structure_mismatch` `template_drift` `missing_key` `too_few_documents` `unrepresented_structure`

### 표기

- **주석·오류 문구는 한국어.** 커밋 메시지는 conventional 접두사(영어) + 한국어 본문

## 검증에 관하여

이 저장소에서 반복해서 처벌받은 실패가 둘 있다. 둘 다 테스트가 초록인 채로 일어났다.

**① 안 잰 것을 아는 것처럼 쓰기.** 설계 문서가 "이건 재스캔이 잡아준다"고 세 번 주장했고 세 번 다 틀렸다. **안전망은 주장하기 전에 잰다.** 코드를 읽어서 확신했다면 아직 안 잰 것이다.

**② 통과하지만 아무것도 안 지키는 테스트.** RED 를 거쳤어도 그 테스트가 잡으려던 결함이 아닌 **다른 이유로** 실패했을 수 있다. 그리고 작성 시점엔 유효했던 테스트가 나중에 다른 검사가 생기며 판별력을 잃기도 한다.

그래서 새 테스트에는 **변이시험**을 붙인다 — 담당 로직만 깨뜨리고 그 테스트가 실제로 실패하는지 확인한 뒤 정확히 복원(`git checkout -- <file>`). 변이가 컴파일됐는지 먼저 확인할 것. 빌드 실패를 "실패 없음"으로 읽는 사고가 있었다.

**계기가 답을 감출 수 있다.** 실제 사례: `encoding/xml` 의 `Token` 은 빈 스택의 종료 태그를 토큰 대신 오류로 내서 **세어야 할 바로 그 태그를 감춘다**(`RawToken` 을 써야 한다). 삼켜진 내용이 주석 안에 바이트로 남아 `grep` 이 "이상 없음"을 낸 적도 있다 — 스캔된 노드로 세야 보였다.

## 픽스처 — 공개 저장소다

`testdata/` 는 커밋되고 이 저장소는 **public** 이다.

- **내용과 `docProps` 를 둘 다 확인한다.** 본문만 보고 "개인정보 없다"고 했다가 네 픽스처 전부 `docProps/core.xml` 에 실명이 있던 사고가 있었다
- `docProps/thumbnail.jpeg` 도 본다. 원본 페이지를 렌더한 이미지라 텍스트를 지워도 그림에 남는다
- 조직 자산(디자인 토큰 값·내부 문서)은 넣지 않는다. 서식만 필요하면 `panto apply` 의 `setText` 로 본문을 익명화한다 — `setText` 는 `Inner` 만 바꾸므로 서식이 상하지 않는다
- `testdata/real/` 의 테스트는 `*.docx` 를 glob 해 **전부 같은 양식**으로 취급한다. 디자인 견본을 거기 두면 `nontext_diff` 로 깨진다. 그건 `testdata/design/` 에 둔다

## 설계 문서

슬라이스마다 스펙이 있고, 그것이 그 슬라이스의 계약이다. 작업 전에 해당 스펙을 읽는다.

`docs/superpowers/specs/` — 왕복(2026-08-06) · 멀티파트(08-08) · diff(08-09) · LCS 정렬(08-10) · tmpl 정렬(08-11) · 명시적 삭제(08-12) · 보고서 파이프라인(08-14)

전역 작업 원칙(설계 선행·TDD·Surgical·디버깅·git)은 `~/.Codex/rules/` 에 있다. 여기서 반복하지 않는다.
