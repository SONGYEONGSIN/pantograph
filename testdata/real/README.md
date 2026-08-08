# 실제 Word·PowerPoint 픽스처

Microsoft Word·PowerPoint 16.x (macOS) 가 저장한 `.docx`·`.pptx`. 합성 내용이며
개인정보는 없다.

| 파일 | 용도 |
|---|---|
| `form-a.docx` | I1(항등)·I2(국소성), 그리고 docx I4a 의 베이스 |
| `form-b.docx` | docx I4a — `form-a` 와 같은 양식, 성명·금액·비고만 다름 |
| `deck-a.pptx` | I1·I2(pptx), 그리고 pptx I4a 시도의 베이스 |
| `deck-b.pptx` | pptx I4a 시도 — `deck-a` 와 같은 양식(슬라이드 3장, 같은 레이아웃), 슬라이드 3장 모두 제목만 다름 |

`비고` 필드에 `&` 를 일부러 넣었다. 되채우기가 텍스트를 디코드했다 재인코드하므로
(스펙 §13) 엔티티 왕복이 I4a 를 깨는지 이 필드가 시험한다. Word 는 `&amp;` 로 쓴다.

두 문서의 문단 구조는 같아야 한다 — v1 템플릿 역추출은 경로 집합이 완전히
일치할 때만 동작하고 다르면 `structure_mismatch` 로 거절한다.

## `deck-a.pptx` / `deck-b.pptx`

같은 커스텀 레이아웃(슬라이드 마스터의 레이아웃 2)으로 만든 슬라이드 3장짜리
덱 두 벌. `tmpl.Extract` 의 파트 집합·구조 비교가 파트 하나만 스캔해서는
시험되지 않는다는 점(Task 7 리뷰 finding) 때문에, 두 덱은 **슬라이드 3장
모두**에서 제목 텍스트가 다르다 — `deck-a` 는 "표지"/"둘째 장"/"셋째 장",
`deck-b` 는 "겉표지"/"두번째쪽"/"마지막쪽". 이렇게 해야 `tmpl.Extract` 가
잡는 키가 파트(슬라이드) 여러 개에 걸치고, 파트별 루프가 실제로 여러 번
도는지 확인할 수 있다.

제목 텍스트는 한글로만 골랐다 — PowerPoint 는 같은 텍스트 상자 안에서
스크립트(한글 vs 라틴)가 바뀌면 `lang` 속성이 다른 새 `<a:r>` 런으로
자동으로 쪼갠다. "표지 B" 처럼 한글과 영문을 섞으면 `deck-a` 는 런 1개,
`deck-b` 는 런 2개가 되어 구조 자체가 달라지고 `structure_mismatch` 로
거절된다 — 애초에 I4a 를 시험할 수 없다.

**알려진 한계 (Task 8 에서 발견)**: `TestPptxTemplateReversalReal` 은 pptx
I4a 를 실제 PowerPoint 산출물로 시험하지만, 현재 `tmpl.Extract` 단계에서
`nontext_diff` 로 거절되어 **실패한다**. PowerPoint 는 도형을 새로 만들
때마다 `p:cNvPr/a:extLst/a:ext` 안에 `a16:creationId`(도형별 GUID, 슬라이드당
2개)와 `p:extLst` 안에 `p14:creationId`(슬라이드별 숫자 ID) 를 찍는데, 이
값은 내용과 무관하게 매 생성마다 달라진다 — docx 의 `w14:paraId`·`w:rsid*`
와 같은 부류의 휘발성 식별자다. 하지만 `internal/tmpl/schema.go` 의
`VolatileAttrs`(`paraId`, `textId`, `rsid*` 접두사) 는 docx 전용이라
`a16:creationId`/`p14:creationId` 를 모른다 — 그래서 독립적으로 저장된
PowerPoint 문서 쌍은 텍스트가 완전히 같아도 이 지점에서 거절될 것이다.
테스트를 완화하거나 픽스처를 바꿔 피하지 않았다 — spec 이 들어야 할
설계 finding 이다. `TestPptxTemplateReversalReal` 이 이 실패를 그대로
증거로 남긴다.
