// Package multisave publishes several package-private staging files of one
// download task into its owner's SDK destination. It is independent of the
// downloader: BT, media and future multi-resource tasks describe their staged
// files as Sources and persist the returned Items in their own job record.
//
// Each file is one SDK files.moveFromStaging operation, so every write still
// runs with the owner's DSM permissions and never overwrites an existing file.
// The durable leaf layout flattens task-internal paths (Rel) into Name.
// The batch subfolder is applied separately by Params:
//
//   - one top-level file keeps its own name: "ubuntu.iso";
//   - otherwise every file is placed under the task folder, flattened with
//     " - ": "Album - cd1 - 01.flac".
//
// Names are made safe for DSM shares, bounded to MaxNameBytes and unique
// without regard to letter case. Rel keeps the intended structure for a future
// layout change without rewriting in-flight operation names.
package multisave

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// MaxFiles bounds one task's durable record and executor queue use.
	MaxFiles = 256
	// MaxFileBytes is the SDK files.moveFromStaging per-file limit.
	MaxFileBytes int64 = 16 << 30
	// MaxNameBytes is the DSM file name limit.
	MaxNameBytes = 255
	separator    = " - "
)

// Source is one completed file, relative to the task's content root.
type Source struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Item is the durable per-file publication record. Persist it before the first
// Advance: OperationID and the move parameters are derived only from it.
type Item struct {
	// Source is the slash path relative to the task's stage directory.
	Source string `json:"source"`
	// Rel is the intended path inside the destination; Name is its flattened form.
	Rel       string `json:"rel"`
	Name      string `json:"name"`
	SavedName string `json:"savedName,omitempty"`
	Conflict  string `json:"conflict,omitempty"`
	Size      int64  `json:"size"`
	Attempt   int    `json:"attempt,omitempty"`
	// State is "" before admission, then pending, queued, copying, uncertain,
	// committed, canceled, save_failed or access_denied.
	State  string `json:"state,omitempty"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

var errInvalid = errors.New("multisave: invalid file list")

func validComponent(c string) bool {
	return c != "" && c != "." && c != ".." && len(c) <= MaxNameBytes && !strings.ContainsAny(c, "\\\x00")
}

func validRelative(p string) bool {
	if p == "" || len(p) > 4096 || path.IsAbs(p) || path.Clean(p) != p {
		return false
	}
	for _, c := range strings.Split(p, "/") {
		if !validComponent(c) {
			return false
		}
	}
	return true
}

// Plan validates sources below root (a stage-relative folder, "" for the stage
// itself) and assigns each file its destination name. title names the task
// folder when the task has several files or nested files.
func Plan(title, root string, sources []Source) ([]Item, error) {
	if len(sources) == 0 || len(sources) > MaxFiles || (root != "" && !validRelative(root)) {
		return nil, errInvalid
	}
	seen := map[string]bool{}
	for _, s := range sources {
		if !validRelative(s.Path) || s.Size < 0 || s.Size > MaxFileBytes || seen[s.Path] {
			return nil, errInvalid
		}
		seen[s.Path] = true
	}
	folder := !(len(sources) == 1 && !strings.Contains(sources[0].Path, "/"))
	title = sanitize(title)
	items := make([]Item, len(sources))
	taken := map[string]bool{}
	for i, s := range sources {
		rel := s.Path
		if folder {
			rel = title + "/" + s.Path
		}
		name := destinationName(rel, "")
		if taken[strings.ToLower(name)] {
			name = destinationName(rel, "~"+digest(s.Path))
		}
		if taken[strings.ToLower(name)] {
			return nil, errInvalid
		}
		taken[strings.ToLower(name)] = true
		items[i] = Item{Source: path.Join(root, s.Path), Rel: rel, Name: name, Size: s.Size, Conflict: "rename"}
	}
	return items, nil
}

// destinationName is the only place mapping an intended destination path to
// leaf name the SDK writes. Batch subfolders are independent of this layout.
func destinationName(rel, suffix string) string {
	parts := strings.Split(rel, "/")
	for i := range parts {
		parts[i] = sanitize(parts[i])
	}
	return bound(strings.Join(parts, separator), suffix)
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

// sanitize replaces characters DSM shares, SMB clients or this package's
// validators refuse, and trailing dots/spaces Windows clients cannot open.
func sanitize(s string) string {
	s = strings.ToValidUTF8(s, "_")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\,":*?<>|`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.TrimRight(strings.TrimSpace(s), ". ")
	if s == "" {
		return "_"
	}
	return s
}

// bound fits name+suffix into MaxNameBytes, keeping a short extension and
// cutting only at rune boundaries.
func bound(name, suffix string) string {
	ext := path.Ext(name)
	if len(ext) > 16 || ext == name {
		ext = ""
	}
	stem := strings.TrimSuffix(name, ext)
	limit := MaxNameBytes - len(suffix) - len(ext)
	if len(stem) <= limit {
		return stem + suffix + ext
	}
	if suffix == "" {
		suffix = "~" + digest(name)
		limit -= len(suffix)
	}
	cut := stem[:limit]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + suffix + ext
}

// OperationID is the SDK job ID of one file attempt. It never collides with
// the single-file "gopeed-<task>" IDs and fits the SDK's 128-byte token.
func OperationID(task string, index, attempt int) string {
	id := "gopeed-" + task + ".f" + strconv.Itoa(index) + ".a" + strconv.Itoa(attempt)
	if len(id) <= 128 {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return "gopeed-" + hex.EncodeToString(sum[:])
}

// SafeFilename turns an untrusted suggested leaf into a bounded DSM filename.
// It does not interpret slashes as a multi-file resource hierarchy.
func SafeFilename(name string) string { return bound(sanitize(name), "") }
