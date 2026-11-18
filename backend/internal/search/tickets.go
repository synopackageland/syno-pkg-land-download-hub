package search

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Pin copies the exact result into private durable task input. It survives result
// cache expiry, CGI exit and restart; it carries no DSM session. The download
// job's stable owner-bound key deduplicates both admission and worker recovery.
func (s *Service) Pin(ctx context.Context, owner, rid, identity string) (string, error) {
	result, err := s.Result(ctx, owner, rid)
	if err != nil {
		return "", err
	}
	key := "search-" + digest([]byte(identity + "\x00" + result.Source.ID + "\x00" + result.Source.Revision))[:48]
	dir := filepath.Join(s.Data, "tickets", digest([]byte(owner)))
	unlock, err := lock(ctx, filepath.Join(dir, "tickets.lock"))
	if err != nil {
		return "", err
	}
	defer unlock()
	path := filepath.Join(dir, key+".json")
	if _, err = os.Stat(path); err == nil {
		return key, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(entries) > 4096 {
		return "", fail("search_limit")
	}
	result.Source = Source{SourceInput: SourceInput{ID: result.Source.ID}, Revision: result.Source.Revision}
	raw, _ := json.Marshal(result)
	if err = atomic(path, raw); err != nil {
		return "", err
	}
	return key, nil
}
func (s *Service) ResolveTicket(ctx context.Context, owner, key string) (string, error) {
	if !strings.HasPrefix(key, "search-") || len(key) != 55 {
		return "", fail("search_result_expired")
	}
	suffix := strings.TrimPrefix(key, "search-")
	for _, c := range suffix {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", fail("search_result_expired")
		}
	}
	var r Cached
	if readJSON(filepath.Join(s.Data, "tickets", digest([]byte(owner)), key+".json"), &r, 2<<20) != nil {
		return "", fail("search_result_expired")
	}
	// Revoking or disabling the source refuses work not yet handed to a downloader.
	if err := s.withState(ctx, owner, func(st *state) error {
		src, ok := st.Sources[r.Source.ID]
		if !ok || !src.Enabled || src.Revision != r.Source.Revision {
			return fail("search_source_disabled")
		}
		r.Source = src
		return nil
	}); err != nil {
		return "", err
	}
	return s.resolveCached(ctx, owner, r)
}
func (s *Service) ForgetTicket(owner, key string) {
	if strings.HasPrefix(key, "search-") && len(key) == 55 && filepath.Base(key) == key {
		os.Remove(filepath.Join(s.Data, "tickets", digest([]byte(owner)), key+".json"))
	}
}

// AdmissionKey is independent of source revision; the immutable ticket is not.
func AdmissionKey(identity string) string { return digest([]byte(identity))[:48] }
