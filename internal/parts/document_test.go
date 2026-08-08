package parts_test

import (
	"errors"
	"strings"
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

// TestTreeFillsNodePartPerPart 는 서로 다른 두 파트를 스캔했을 때 각 파트의
// 노드가 자기 파트의 이름을 갖는지 본다. Tree 가 모든 노드에 같은(예: 첫 스캔한
// 파트의) 이름을 잘못 채워도 파트 하나만 스캔하는 테스트로는 잡히지 않는다 —
// 두 파트를 스캔해 서로 다른 이름이 나오는지 직접 대조해야 잡힌다.
func TestTreeFillsNodePartPerPart(t *testing.T) {
	d, err := parts.Open(openReal(t, "deck-a.pptx"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	name1 := d.Parts()[0].Name
	name2 := d.Parts()[1].Name
	if name1 == name2 {
		t.Fatal("테스트 전제가 깨졌다 — 두 파트 이름이 같다")
	}

	t1, err := d.Tree(name1)
	if err != nil {
		t.Fatalf("Tree(%s): %v", name1, err)
	}
	t2, err := d.Tree(name2)
	if err != nil {
		t.Fatalf("Tree(%s): %v", name2, err)
	}
	if len(t1.Nodes) == 0 || len(t2.Nodes) == 0 {
		t.Fatal("노드가 비었다")
	}

	for _, n := range t1.Nodes {
		if n.Part != name1 {
			t.Fatalf("%s 의 노드가 Part=%q, want %q", name1, n.Part, name1)
		}
	}
	for _, n := range t2.Nodes {
		if n.Part != name2 {
			t.Fatalf("%s 의 노드가 Part=%q, want %q", name2, n.Part, name2)
		}
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

	// 겹치는 선택자 — 앞 선택자가 이미 고른 파트를 뒤 선택자가 다시 가리켜도
	// 유효한 선택자로 인정돼야 한다. picked 는 선택자 간에 공유되는 합집합이므로
	// "이 선택자가 뭘 새로 골랐나"가 아니라 "picked 가 늘었나"로 판정하면
	// 이런 겹침에서 뒤 선택자가 오탈자로 오검출된다.
	overlap, err := d.Select([]string{"ppt/slides/*", "pptx/slide[2]"})
	if err != nil {
		t.Fatalf("Select 겹침: %v", err)
	}
	if len(overlap) != 3 {
		t.Fatalf("겹치는 선택자 → %d개, want 3 (중복 없이 계획 순서)", len(overlap))
	}
	for i, pt := range overlap {
		if pt.Name != all[i].Name {
			t.Fatalf("overlap[%d] = %+v, want %+v (계획 순서)", i, pt, all[i])
		}
	}

	// 유효한 선택자 옆에 와도 오탈자 선택자는 여전히 거절되고, 에러가 그 선택자를 지목한다
	if _, err := d.Select([]string{"ppt/slides/*", "ppt/nope/*"}); err == nil {
		t.Error("유효한 선택자와 같이 와도 오탈자 선택자가 거절되지 않았다")
	} else if !strings.Contains(err.Error(), "ppt/nope/*") {
		t.Errorf("에러가 오탈자 선택자를 지목하지 않는다: %v", err)
	}
}

// TestSelectRejectionReasons 는 선택자가 아무 파트도 못 고를 때의 사유가
// patch.resolvePart 와 같은 세 갈래로 갈리는지 본다.
//
// Select 는 모든 실패를 한 문구로 뭉쳐 CLI 가 전부 part_not_found 로 냈다 —
// 40 줄 옆의 patch.resolvePart 는 같은 입력 부류를 part_not_found /
// ref_not_found / part_not_scannable 로 정확히 갈랐고 TestPartResolutionErrors
// 가 셋을 고정하고 있었다. 같은 질문에 두 답이 나오지 않도록 판정은 이 패키지
// 한 곳에만 둔다.
func TestSelectRejectionReasons(t *testing.T) {
	d, err := parts.Open(openReal(t, "deck-a.pptx"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cases := []struct{ sel, reason string }{
		{"ppt/theme/theme1.xml", "part_not_scannable"}, // 컨테이너엔 있으나 본문 파트가 아니다
		{"pptx/slide[99]", "ref_not_found"},            // 논리 참조 모양인데 안 풀린다
		{"ppt/slides/slide99.xml", "part_not_found"},   // 물리 경로가 컨테이너에 없다
		{"ppt/nope/*", "part_not_found"},               // 아무것도 못 고르는 glob
		{"ppt/slides/[", "part_not_found"},             // glob 문법 오류 — 어차피 아무것도 못 고른다
	}
	for _, c := range cases {
		_, err := d.Select([]string{c.sel})
		if err == nil {
			t.Errorf("%s: 거절되지 않았다", c.sel)
			continue
		}
		var se *parts.SelectError
		if !errors.As(err, &se) {
			t.Errorf("%s: *parts.SelectError 가 아니다: %v", c.sel, err)
			continue
		}
		if se.Reason != c.reason {
			t.Errorf("%s → Reason %q, want %q", c.sel, se.Reason, c.reason)
		}
		if !strings.Contains(se.Error(), c.sel) {
			t.Errorf("%s: 오류가 선택자를 지목하지 않는다: %v", c.sel, se)
		}
	}
}
