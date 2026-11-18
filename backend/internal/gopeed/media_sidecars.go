package gopeed

import (
	"crypto/sha256"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"dsmapp/internal/multisave"
)

// Pair sidecars with the published video name even when multisave shortened
// a long filename. Keep room for a language suffix on SRT/VTT files.
func pairMediaSidecars(items []multisave.Item) error {
	stems := map[string]string{}
	for i, item := range items {
		ext := strings.ToLower(path.Ext(item.Source))
		if ext != ".mp4" && ext != ".mkv" && ext != ".webm" && ext != ".mov" && ext != ".m4v" {
			continue
		}
		name := strings.TrimSuffix(item.Name, path.Ext(item.Name))
		if len(name) > 220 {
			sum := sha256.Sum256([]byte(name))
			name = name[:200]
			for !utf8.ValidString(name) {
				name = name[:len(name)-1]
			}
			name += fmt.Sprintf("~%x", sum[:5])
		}
		stems[strings.TrimSuffix(item.Source, path.Ext(item.Source))] = name
		items[i].Name = name + path.Ext(item.Name)
	}
	for i, item := range items {
		ext := strings.ToLower(path.Ext(item.Source))
		if ext != ".nfo" && ext != ".srt" && ext != ".vtt" {
			continue
		}
		for source, name := range stems {
			if item.Source == source+ext || strings.HasPrefix(item.Source, source+".") {
				suffix := strings.TrimPrefix(item.Source, source)
				if len(name+suffix) > 255 {
					return refused("media_metadata_invalid")
				}
				items[i].Name = name + suffix
				break
			}
		}
	}
	seen := map[string]bool{}
	for _, item := range items {
		key := strings.ToLower(item.Name)
		if seen[key] {
			return refused("media_metadata_invalid")
		}
		seen[key] = true
	}
	return nil
}
