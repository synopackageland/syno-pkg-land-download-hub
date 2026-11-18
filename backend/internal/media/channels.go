package media

import (
	"context"
	"encoding/json"
	"strings"
)

// ChannelEntries lists a bounded latest page. The caller owns scheduling,
// cookies, outbound policy and durable admission; no wall-clock timeout here.
func ChannelEntries(ctx context.Context, run Exec, env Env, source, cookies string) ([]string, error) {
	args := append(common(env), "--flat-playlist", "--dump-single-json", "--skip-download", "--playlist-end", "100")
	if cookies != "" {
		args = append(args, "--cookies", cookies)
	}
	args = append(args, "--", source)
	b, stderr, e := run(ctx, env.YtDlp, args, Environ(env.Home), probeMax)
	if e != nil {
		return nil, &Error{Code: Classify(string(stderr))}
	}
	var list struct {
		Entries []struct {
			URL        string `json:"url"`
			WebpageURL string `json:"webpage_url"`
		}
	}
	if json.Unmarshal(b, &list) != nil || len(list.Entries) > 100 {
		return nil, &Error{"media_failed"}
	}
	urls := []string{}
	for _, entry := range list.Entries {
		u := entry.WebpageURL
		if u == "" {
			u = entry.URL
		}
		if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
			urls = append(urls, u)
		}
	}
	return urls, nil
}
