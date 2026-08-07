package opc

// 이 파일만 패키지 내부 테스트다. Write 가 "재조립했는데 결과가 같았다"가 아니라
// "재조립을 아예 안 했다"를 확인하려면 내부 상태를 건드려야 하기 때문이다.

import (
	"bytes"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

// TestWriteIsVerbatimWhenNothingDirty 는 고친 파트가 없을 때 Write 가 원본
// 바이트를 그대로 흘려보내는지 본다 (I1).
//
// Open 의 자기검사가 "재조립해도 원본이 나온다"를 보장하므로, 출력 바이트만
// 비교해서는 두 경로를 구분할 수 없다. 그래서 재조립 경로를 타면 반드시 다른
// 결과가 나오도록 엔트리 목록을 하나로 줄여놓고 Write 를 부른다.
func TestWriteIsVerbatimWhenNothingDirty(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p, err := OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}

	// 재조립하면 엔트리 1개짜리 zip 이 나온다 — 원본과 같을 수 없다.
	p.order = p.order[:1]

	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(src, got) {
		t.Fatalf("빈 패치 경로가 원본을 그대로 쓰지 않고 재조립했다 (원본 %d바이트, 결과 %d바이트)",
			len(src), len(got))
	}
}

// TestWriteReassemblesWhenDirty 는 파트를 고치면 재조립 경로로 간다는 것을
// 같은 방식으로 확인한다 — 위 테스트가 dirty 여부와 무관하게 통과하는
// (즉 Write 가 언제나 원본을 뱉는) 퇴행을 잡는다.
func TestWriteReassemblesWhenDirty(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문"})
	p, err := OpenBytes(src)
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	if err := p.Replace("word/document.xml", []byte(`<w:document/>`)); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, err := p.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if bytes.Equal(src, got) {
		t.Fatal("파트를 고쳤는데 원본 바이트가 그대로 나왔다")
	}
}
