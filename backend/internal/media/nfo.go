package media

import (
	"encoding/json"
	"encoding/xml"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type nfoID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr"`
	Value   string `xml:",chardata"`
}

type movieNFO struct {
	XMLName   xml.Name `xml:"movie"`
	Title     string   `xml:"title"`
	Plot      string   `xml:"plot"`
	Premiered string   `xml:"premiered,omitempty"`
	Studio    string   `xml:"studio,omitempty"`
	Runtime   int      `xml:"runtime,omitempty"`
	ID        nfoID    `xml:"uniqueid"`
}

// WriteNFO turns private yt-dlp metadata into adjacent Kodi/Jellyfin sidecars.
// Metadata JSON never becomes a published result. XML values are escaped.
func WriteNFO(stage string) error {
	return filepath.WalkDir(stage, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".info.json") {
			return nil
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() || st.Size() > 2<<20 {
			return &Error{"media_metadata_invalid"}
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		var info struct {
			Title, Description, Uploader, ID string
			Extractor                        string `json:"extractor_key"`
			Duration                         float64
			UploadDate                       string `json:"upload_date"`
		}
		if json.Unmarshal(b, &info) != nil {
			return &Error{"media_metadata_invalid"}
		}
		date := ""
		if parsed, e := time.Parse("20060102", info.UploadDate); e == nil {
			date = parsed.Format("2006-01-02")
		}
		id := bound(info.ID, 200)
		if info.Extractor != "" {
			id = strings.ToLower(bound(info.Extractor, 64)) + ":" + id
		}

		nfo, e := xml.MarshalIndent(movieNFO{Title: bound(info.Title, 500), Plot: bound(info.Description, 20000), Studio: bound(info.Uploader, 500), Runtime: int(info.Duration / 60), Premiered: date, ID: nfoID{Type: "downloadhub", Default: true, Value: id}}, "", "  ")
		if e != nil {
			return e
		}
		base := strings.TrimSuffix(p, ".info.json")
		// A video may be remuxed. Match its actual final name, never metadata paths.
		entries, e := os.ReadDir(filepath.Dir(p))
		if e != nil {
			return e
		}
		for _, file := range entries {
			if file.IsDir() || file.Type()&os.ModeSymlink != 0 {
				continue
			}
			ext := strings.ToLower(filepath.Ext(file.Name()))
			if ext != ".mp4" && ext != ".mkv" && ext != ".webm" && ext != ".mov" && ext != ".m4v" {
				continue
			}
			stem := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
			root := filepath.Base(base)
			if stem != root && !strings.HasPrefix(stem, root+" - ") {
				continue
			}
			out := filepath.Join(filepath.Dir(p), stem+".nfo")
			if st, e := os.Lstat(out); e == nil && !st.Mode().IsRegular() {
				return &Error{"media_metadata_invalid"}
			}
			if e = os.WriteFile(out, append([]byte(xml.Header), nfo...), 0600); e != nil {
				return e
			}
		}
		return os.Remove(p)
	})
}
