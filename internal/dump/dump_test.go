package dump_test

import (
	"bytes"
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/dump"
	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/testutil"
)

// I3 결정성 — 같은 입력을 두 번 덤프하면 바이트가 같아야 한다.
func TestDumpIsDeterministic(t *testing.T) {
	src := testutil.MinimalDocx([]string{"제목", "본문", "꼬리말"})

	run := func() []byte {
		p, err := opc.OpenBytes(src)
		if err != nil {
			t.Fatalf("OpenBytes: %v", err)
		}
		d, err := dump.Build(p)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		b, err := dump.Marshal(d)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		return b
	}

	for i := 0; i < 20; i++ {
		if a, b := run(), run(); !bytes.Equal(a, b) {
			t.Fatalf("반복 %d 에서 덤프가 달라졌다\n--- A ---\n%s\n--- B ---\n%s", i, a, b)
		}
	}
}

func TestDumpCarriesHashAndParts(t *testing.T) {
	p, err := opc.OpenBytes(testutil.MinimalDocx([]string{"제목"}))
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	d, err := dump.Build(p)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if d.Doc.Hash != p.Hash {
		t.Fatalf("Hash = %q, want %q", d.Doc.Hash, p.Hash)
	}
	if d.Doc.Format != "docx" {
		t.Fatalf("Format = %q, want %q", d.Doc.Format, "docx")
	}
	if d.Doc.ScannedPart != dump.ScannedPart {
		t.Fatalf("ScannedPart = %q, want %q", d.Doc.ScannedPart, dump.ScannedPart)
	}
	if len(d.Doc.Parts) != 3 {
		t.Fatalf("Parts %d개, want 3: %v", len(d.Doc.Parts), d.Doc.Parts)
	}
	if len(d.Nodes) == 0 {
		t.Fatal("노드가 비었다")
	}
}
