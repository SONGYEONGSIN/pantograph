# 실제 Word·PowerPoint 픽스처

Microsoft Word·PowerPoint 16.x (macOS) 가 저장한 `.docx`·`.pptx`. 합성 내용이며
개인정보는 없다.

**`docProps/core.xml` 은 저작자 메타데이터를 지웠다.** Word·PowerPoint 는 이
파트의 `dc:creator`·`cp:lastModifiedBy` 에 저장한 macOS 계정의 실명을 그대로
찍는다 — 문서 본문과는 무관한 저작 도구의 부산물이지만, 공개 저장소에 실명이
들어가는 건 원치 않는다. 네 파일 모두 이 두 필드만 `pantograph` 로 바꿨고,
나머지 40(pptx)/10(docx) 개 파트는 이 프로젝트의 `internal/opc.Open` →
`Part`/`Replace`/`Bytes` 로 다시 써서 원래 Word/PowerPoint 가 낸 압축 바이트
그대로(재압축 없이) 남겼다 — Python `zipfile` 이나 `archive/zip` 으로 통째로
다시 만들면 컨테이너 전체가 다른 라이터의 산출물로 바뀌어 I1·I2 가 시험하는
"실제 오피스 제품군이 낸 바이트" 라는 전제 자체가 허물어진다. 즉 이 파일들은
**docProps/core.xml 한 파트만 pantograph 자신이 쓴 바이트이고, 나머지는
여전히 Word/PowerPoint 원본 바이트다** — I1(항등)·I2(국소성)이 시험하는
"건드리지 않은 엔트리" 개념과 정확히 같은 방식으로 지웠다.

| 파일 | 용도 |
|---|---|
| `form-a.docx` | I1(항등)·I2(국소성), 그리고 docx I4a 의 베이스 |
| `form-b.docx` | docx I4a — `form-a` 와 같은 양식, 성명·금액·비고만 다름 |
| `deck-a.pptx` | I1·I2(pptx), 그리고 pptx I4a 의 베이스 |
| `deck-b.pptx` | pptx I4a — `deck-a` 와 같은 양식(슬라이드 3장, 같은 레이아웃), 슬라이드 3장 모두 제목만 다름 |

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

**PowerPoint 의 도형·슬라이드 생성 ID (Task 8 에서 발견, 같은 태스크에서
해소)**: `TestPptxTemplateReversalReal` 을 처음 붙였을 때 `tmpl.Extract`
가 `nontext_diff` 로 거절했다. PowerPoint 는 도형을 새로 만들 때마다
`p:cNvPr/a:extLst/a:ext` 안에 `a16:creationId`(도형별 GUID, 슬라이드당
2개)를, 슬라이드를 새로 만들 때마다 `p:extLst` 안에
`p14:creationId`(슬라이드별 숫자 ID)를 찍는데, 이 값은 내용과 무관하게
매 생성마다 새로 나온다 — docx 의 `w14:paraId`·`w:rsid*` 와 같은 부류의
휘발성 식별자다. 다만 속성 이름 자체(`id`, `val`)는 OOXML 전역에서 진짜
내용(색상 값 등)도 나르는 흔한 이름이라 속성 이름 단위로는 못 뺀다 —
`internal/tmpl/schema.go` 의 `VolatileElements`(요소 이름 `creationId`
전체를 통째로 비교에서 뺀다) 로 해결했다.
