package media

import (
	"reflect"
	"slices"
	"testing"
)

func TestOptionsRejectExecutableInput(t *testing.T) {
	for _, o := range []Options{{Format: "best --exec whoami"}, {Subtitles: "srt", SubtitleLanguages: []string{"en,--exec=foo"}}, {AudioFormat: "wav"}, {SponsorBlock: "all"}, {Subtitles: "srt"}} {
		if _, e := o.Normalize(); e == nil {
			t.Fatalf("accepted %#v", o)
		}
	}
}
func TestOptionArgumentsAreBackendOwned(t *testing.T) {
	o, e := (Options{Format: "audio", AudioFormat: "flac", Subtitles: "srt", SubtitleLanguages: []string{"en", "zh-TW"}, EmbedThumbnail: true, EmbedMetadata: true, SplitChapters: true, SponsorBlock: "remove"}).Normalize()
	if e != nil {
		t.Fatal(e)
	}
	args := DownloadArgs(Env{FFmpegDir: "/managed/ffmpeg", Home: "/private"}, Download{URL: "https://video.example/watch", Format: o.Format, Options: o, Output: "/stage", Control: "/control", Cookies: "/private/copy"})
	for _, flag := range []string{"--no-cookies-from-browser", "--ignore-config", "--embed-thumbnail", "--embed-metadata", "--split-chapters", "--sponsorblock-remove", "--write-subs", "--convert-subs"} {
		if !slices.Contains(args, flag) {
			t.Fatal(flag, args)
		}
	}
	for flag, want := range map[string]string{"--audio-format": "flac", "--sub-langs": "en,zh-TW", "--cookies": "/private/copy"} {
		i := slices.Index(args, flag)
		if i < 0 || args[i+1] != want {
			t.Fatal(flag, args)
		}
	}
	if !reflect.DeepEqual(args[len(args)-2:], []string{"--", "https://video.example/watch"}) {
		t.Fatal(args)
	}
}
