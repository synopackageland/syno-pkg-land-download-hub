package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNFOEscapesAndPairsWithVideo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "A [id].mp4"), []byte("video"), 0600)
	os.WriteFile(filepath.Join(dir, "A [id].info.json"), []byte(`{"title":"A & B","description":"<tag>","duration":125,"upload_date":"20261101","uploader":"Studio","id":"abc"}`), 0600)
	if e := WriteNFO(dir); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(dir, "A [id].nfo"))
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"<movie>", "A &amp; B", "&lt;tag&gt;", "2026-11-01", "<runtime>2</runtime>", `<uniqueid type="downloadhub" default="true">abc</uniqueid>`} {
		if !strings.Contains(string(b), v) {
			t.Fatal(string(b))
		}
	}
	if _, e := os.Stat(filepath.Join(dir, "A [id].info.json")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
