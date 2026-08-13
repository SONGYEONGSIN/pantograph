package patch

import "testing"

// TestBadResultClassifiesSplicedPart 는 스플라이스 결과를 판정하는 술어를
// 직접 부른다 (package patch 내부 테스트라 소문자 함수를 쓸 수 있다).
//
// empty_part 갈래는 CLI 로는 도달할 수 없다 — replaceRaw 의 조각은 요소를 하나
// 이상 담아야 하고(checkFields), 루트 delete 는 delete_root 가 먼저 막는다.
// 두 검사를 지나치는 셋째 길(어휘적으로 미완결인 조각, 스펙 §5)로도 노드 0개에
// 닿지 못한다는 것은 실측으로 확인했다.
//
// 그래도 백스톱은 남긴다: 내용을 지우는 경로의 마지막 그물이라 "지금 도달
// 불가"가 "불필요"를 뜻하지 않는다 — 요소 규칙을 무력화하면 루트를 공백으로
// 바꾸는 패치가 실제로 여기서 걸린다(실측). 다만 아무도 시험할 수 없는 그물은
// 아무도 믿을 수 없으므로, 종단 경로 대신 술어를 여기서 시험한다.
func TestBadResultClassifiesSplicedPart(t *testing.T) {
	const decl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
	for _, c := range []struct{ name, out, wantReason string }{
		{"요소가 하나도 없음", decl + "\n   ", "empty_part"},
		{"주석만 남음", decl + "<!-- 전부 지워짐 -->", "empty_part"},
		{"깨진 XML", decl + "<w:p>", "invalid_xml"},
		{"성립하는 파트", decl + `<w:document><w:body/></w:document>`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			reason, detail := badResult([]byte(c.out), "document")
			if reason != c.wantReason {
				t.Fatalf("사유 %q, 기대 %q (detail=%q)", reason, c.wantReason, detail)
			}
			if reason != "" && detail == "" {
				t.Fatal("거절인데 detail 이 비었다 — 사용자가 무슨 일이 났는지 알 수 없다")
			}
		})
	}
}
