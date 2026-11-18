package cardigann

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestDownloadHubMethodTemplate(t *testing.T) {
	for _, keyword := range []string{"ubuntu", ""} {
		tc := &TemplateContext{Keywords: keyword}
		req, err := (Engine{}).buildSearchRequest(context.Background(), Config{BaseURL: "https://fixture.invalid/"}, tc, &SearchBlock{}, SearchPathBlock{Path: "search", Method: "{{ if .Keywords }}POST{{ else }}GET{{ end }}"})
		if err != nil {
			t.Fatal(err)
		}
		want := http.MethodGet
		if keyword != "" {
			want = http.MethodPost
		}
		if req.Method != want {
			t.Fatalf("method = %s, want %s", req.Method, want)
		}
	}
}
func TestDownloadHubFullMonthDate(t *testing.T) {
	got, err := filterDateparse(context.Background(), "12 October 2012 05:03:29 +03:00", []string{"d MMMM yyyy HH:mm:ss zzz"}, &TemplateContext{})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil || parsed.Year() != 2012 || parsed.Month() != time.October {
		t.Fatalf("date=%s err=%v", got, err)
	}
}

func TestDownloadHubTemplateOutputIsCapped(t *testing.T) {
	big := `{{ range $i := .Categories }}{{ range $j := $.Categories }}{{ "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" }}{{ end }}{{ end }}`
	cats := make([]string, 2000)
	tc := &TemplateContext{Categories: cats}
	if _, err := render(big, tc); err == nil || !errors.Is(err, errTemplateOutputTooLarge) {
		t.Fatalf("err = %v, want output too large", err)
	}
	if got, err := render("{{ .Keywords }}", &TemplateContext{Keywords: "ok"}); err != nil || got != "ok" {
		t.Fatalf("got %q err %v", got, err)
	}
}

// A JSON selector indexing the root array ("$[0].id", TPB's rows.count) must
// find the element: the bracket rewrite used to leave a leading dot.
func TestDownloadHubJSONRootIndex(t *testing.T) {
	if got := jsonPath("$[0].id"); got != "0.id" {
		t.Fatalf("jsonPath = %q", got)
	}
	doc, err := ParseDoc(ResponseJSON, []byte(`[{"id":"7"}]`))
	if err != nil {
		t.Fatal(err)
	}
	v, ok, err := (SelectorBlock{Selector: "$[0].id"}).Extract(context.Background(), doc, &TemplateContext{})
	if err != nil || !ok || v != "7" {
		t.Fatalf("got %q ok=%v err=%v", v, ok, err)
	}
}
