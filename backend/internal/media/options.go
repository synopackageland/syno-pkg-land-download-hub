package media

import (
	"regexp"
	"slices"
	"strings"
)

// Options contains choices, never executable arguments or output paths.
type Options struct {
	Format            string   `json:"format"`
	SubtitleLanguages []string `json:"subtitleLanguages,omitempty"`
	Subtitles         string   `json:"subtitles,omitempty"`
	AudioFormat       string   `json:"audioFormat,omitempty"`
	EmbedThumbnail    bool     `json:"embedThumbnail,omitempty"`
	EmbedMetadata     bool     `json:"embedMetadata,omitempty"`
	SplitChapters     bool     `json:"splitChapters,omitempty"`
	SponsorBlock      string   `json:"sponsorBlock,omitempty"`
	NFO               bool     `json:"nfo,omitempty"`
}

var languageRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,34}$`)

func (o Options) Normalize() (Options, error) {
	if o.Format == "" {
		o.Format = "best"
	}
	if !ValidFormat(o.Format) || !slices.Contains([]string{"", "off", "embed", "srt", "vtt"}, o.Subtitles) || !slices.Contains([]string{"", "mp3", "m4a", "opus", "flac"}, o.AudioFormat) || !slices.Contains([]string{"", "off", "remove", "mark"}, o.SponsorBlock) || len(o.SubtitleLanguages) > 16 {
		return o, &Error{"media_options_invalid"}
	}
	for _, l := range o.SubtitleLanguages {
		if !languageRE.MatchString(l) {
			return o, &Error{"media_options_invalid"}
		}
	}
	if o.Subtitles != "" && o.Subtitles != "off" && len(o.SubtitleLanguages) == 0 {
		return o, &Error{"media_options_invalid"}
	}
	if o.AudioFormat != "" {
		o.Format = "audio"
	}
	return o, nil
}

func (o Options) NeedsFFmpeg() bool {
	return o.AudioFormat != "" || o.EmbedThumbnail || o.EmbedMetadata || o.SplitChapters || o.Subtitles == "embed" || o.Subtitles == "srt" || o.SponsorBlock == "remove" || o.SponsorBlock == "mark"
}

func optionArgs(o Options) []string {
	var a []string
	if o.Subtitles != "" && o.Subtitles != "off" {
		subtitleFormat := "vtt/best"
		if o.Subtitles == "vtt" {
			subtitleFormat = "vtt"
		}
		a = append(a, "--write-subs", "--sub-langs", strings.Join(o.SubtitleLanguages, ","), "--sub-format", subtitleFormat)
		if o.Subtitles == "embed" {
			a = append(a, "--embed-subs")
		} else if o.Subtitles == "srt" {
			a = append(a, "--convert-subs", "srt")
		}
	}
	if o.EmbedThumbnail {
		a = append(a, "--embed-thumbnail")
	}
	if o.EmbedMetadata {
		a = append(a, "--embed-metadata")
	}
	if o.SplitChapters {
		a = append(a, "--split-chapters", "-o", "chapter:%(title).110B [%(id)s] - %(section_number)03d %(section_title).80B.%(ext)s")
	}
	if o.SponsorBlock == "remove" {
		a = append(a, "--sponsorblock-remove", "all")
	} else if o.SponsorBlock == "mark" {
		a = append(a, "--sponsorblock-mark", "all")
	}
	if o.NFO && o.Format != "audio" {
		a = append(a, "--write-info-json", "--clean-info-json")
	}
	return a
}
