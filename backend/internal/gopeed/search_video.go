package gopeed

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"dsmapp/internal/media"
	"dsmapp/internal/search"
)

func (s *Service) searchVideo(ctx context.Context, in search.Request) (search.Reply, error) {
	env := s.mediaEnv()
	if env.YtDlp == "" {
		return search.Reply{Code: "search_tool_missing"}, nil
	}
	if in.Query == "" || in.Page < 1 || in.Page > 10 || (in.Kind != "ytsearch" && in.Kind != "bilisearch") {
		return search.Reply{Code: "invalid_request"}, nil
	}
	if err := media.PrepareHome(env.Home); err != nil {
		return search.Reply{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	out, stderr, err := s.mediaExec()(ctx, env.YtDlp, media.SearchArgs(env, in.Kind, in.Query, in.Page), media.Environ(env.Home), 4<<20)
	if err != nil {
		code := "search_network"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			code = "search_timeout"
		} else if media.Classify(string(stderr)) == "media_login_required" {
			code = "search_login"
		}
		return search.Reply{Code: code}, nil
	}
	rep := search.Reply{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 4096), 128<<10)
	for scanner.Scan() {
		var row struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			URL     string `json:"url"`
			Webpage string `json:"webpage_url"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			return search.Reply{Code: "search_parse"}, nil
		}
		link := row.Webpage
		if link == "" {
			link = row.URL
		}
		if !safeURL(link) {
			continue
		}
		parsed, _ := url.Parse(link)
		// Flat results are pages, never arbitrary local or generic extractor inputs.
		if in.Kind == "ytsearch" && parsed.Hostname() != "www.youtube.com" && parsed.Hostname() != "youtube.com" && parsed.Hostname() != "youtu.be" {
			continue
		}
		if in.Kind == "bilisearch" && parsed.Hostname() != "www.bilibili.com" && parsed.Hostname() != "bilibili.com" {
			continue
		}
		rep.Results = append(rep.Results, search.Hit{Category: "movie", Title: row.Title, Link: link, Hash: in.Kind + ":" + row.ID})
		if len(rep.Results) >= 100 {
			break
		}
	}
	if scanner.Err() != nil {
		return search.Reply{Code: "search_parse"}, nil
	}
	if len(rep.Results) == 0 {
		rep.Code = "search_empty"
	}
	return rep, nil
}
func videoIdentity(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	host := strings.ToLower(u.Hostname())
	if host == "youtu.be" {
		return "youtube:" + strings.Trim(u.Path, "/")
	}
	if host == "youtube.com" || host == "www.youtube.com" {
		if id := u.Query().Get("v"); id != "" {
			return "youtube:" + id
		}
		return "youtube:" + strings.TrimPrefix(u.Path, "/shorts/")
	}
	if host == "www.bilibili.com" || host == "bilibili.com" {
		return "bilibili:" + strings.TrimSuffix(u.Path, "/")
	}
	return link
}
