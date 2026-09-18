package assets

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestPriorityAndSafeNames(t *testing.T) {
	d := t.TempDir()
	base := filepath.Join(d, "base.jar")
	f, e := os.Create(base)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("assets/example/value.txt")
	w.Write([]byte("base"))
	z.Close()
	f.Close()
	overlay := filepath.Join(d, "overlay")
	os.MkdirAll(filepath.Join(overlay, "assets/example"), 0755)
	os.WriteFile(filepath.Join(overlay, "assets/example/value.txt"), []byte("overlay"), 0644)
	s, e := Open([]string{base, overlay})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	b, e := s.Read("assets/example/value.txt")
	if e != nil || string(b) != "overlay" {
		t.Fatalf("priority: %q %v", b, e)
	}
	for _, n := range []string{"../value.txt", "/etc/passwd", "assets/../../value.txt", `assets\..\value.txt`} {
		if _, e := s.Read(n); e == nil {
			t.Fatalf("unsafe path accepted: %q", n)
		}
	}
	if _, e = s.Read("assets/missing.json"); !os.IsNotExist(e) {
		t.Fatalf("missing must be IsNotExist: %v", e)
	}
}

func TestFailedOpenClosesPriorArchives(t *testing.T) {
	if s, e := Open([]string{filepath.Join(t.TempDir(), "absent.jar")}); e == nil || s != nil {
		t.Fatalf("invalid stack returned: %v %v", s, e)
	}
}
