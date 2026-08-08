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
