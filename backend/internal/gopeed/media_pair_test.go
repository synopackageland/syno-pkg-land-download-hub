package gopeed

import (
	"dsmapp/internal/multisave"
	"path"
	"strings"
	"testing"
)

func TestPublishedSidecarsKeepVideoStemAfterLongNameTruncation(t *testing.T) {
	stem := strings.Repeat("長", 70)
	sources := []multisave.Source{{Path: stem + ".mp4", Size: 10}, {Path: stem + ".nfo", Size: 10}, {Path: stem + ".en.srt", Size: 10}}
	items, e := multisave.Plan(strings.Repeat("標", 70), "", sources)
	if e != nil {
		t.Fatal(e)
	}
	if e = pairMediaSidecars(items); e != nil {
		t.Fatal(e)
	}
	video := strings.TrimSuffix(items[0].Name, path.Ext(items[0].Name))
	if items[1].Name != video+".nfo" || items[2].Name != video+".en.srt" {
		t.Fatal(items)
	}
}
