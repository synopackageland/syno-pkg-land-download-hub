package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGalleryArchitectureDigestInstallAndRollback(t *testing.T) {
	f := newFixture(t)
	tools := f.tools(t, "amd64")
	body := elfBody(62)
	a := f.asset("mikf/gallery-dl", "v1.2.3", "gallery-dl.bin", body)
	f.releases["mikf/gallery-dl"] = []release{{Tag: "v1.2.4"}, {Tag: "v1.2.3", Assets: []asset{a}}}
	r, err := tools.Latest(context.Background(), "gallery-dl")
	if err != nil || r.Version != "1.2.3" || r.SHA256 != digest(body) || r.Expect != "1.2.3" {
		t.Fatal(r, err)
	}
	arm := f.tools(t, "arm64")
	if arm.Supported("gallery-dl") {
		t.Fatal("arm supported")
	}
	if _, e := arm.Latest(context.Background(), "gallery-dl"); e == nil {
		t.Fatal("arm release")
	}
	if _, e := arm.Install(context.Background(), r); e == nil {
		t.Fatal("cross arch install")
	}
	tools.Check = func(context.Context, string, string, Release) error { return nil }
	installed, e := tools.Install(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if e = verify(installed, Specs["gallery-dl"]); e != nil {
		t.Fatal(e)
	}
	exe := filepath.Join(installed.Dir, "gallery-dl")
	os.Chmod(exe, 0700)
	os.WriteFile(exe, append(body, 1), 0500)
	if verify(installed, Specs["gallery-dl"]) == nil {
		t.Fatal("rollback accepted changed bytes")
	}
	a.Digest = "sha256:" + strings.Repeat("0", 64)
	f.releases["mikf/gallery-dl"] = []release{{Tag: "v1.2.3", Assets: []asset{a}}}
	r, e = tools.Latest(context.Background(), "gallery-dl")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tools.Install(context.Background(), r); e == nil {
		t.Fatal("digest mismatch installed")
	}
}

func TestGalleryFallbackAndClosedArguments(t *testing.T) {
	env := Env{YtDlp: "yt", GalleryDlp: "gallery", Home: "/private"}
	calls := 0
	run := func(_ context.Context, exe string, args, environ []string, _ int64) ([]byte, []byte, error) {
		calls++
		if exe == "yt" {
			return nil, []byte("Unsupported URL"), errors.New("unsupported")
		}
		return []byte(`[[3,"https://images.example/1.jpg",{"title":"Album"}]]`), nil, nil
	}
	info, e := Probe(context.Background(), run, env, "https://gallery.example/post")
	if e != nil || info.Tool != "gallery-dl" || info.Kind != "media" || calls != 2 {
		t.Fatal(info, e, calls)
	}
	args := GalleryArgs(env, Download{URL: "https://gallery.example/post", Output: "/staging", Control: "/control", Cookies: "/private-cookies", MaxBytes: 16 << 30}, false)
	joined := strings.Join(args, " ")
	for _, expected := range []string{"--config-ignore", "cache.file=null", "postprocessors=[]", "--range 1-256", "--post-range 1-256", "--directory /staging", "metadata-url=gdl_file_url", "--filename \fE hash_sha1(gdl_file_url) + '.' + extension[:16]", "--filesize-max 17179869184", "--cookies /private-cookies", "-- https://gallery.example/post"} {
		if !strings.Contains(joined, expected) {
			t.Error(expected, args)
		}
	}
	if strings.Contains(joined, "{num") {
		t.Fatal("post-local counters can collide across posts")
	}
	calls = 0
	env.GalleryDlp = ""
	info, e = Probe(context.Background(), run, env, "https://gallery.example/post")
	if e != nil || info.Kind != "http" || calls != 1 {
		t.Fatal(info, e, calls)
	}
}
