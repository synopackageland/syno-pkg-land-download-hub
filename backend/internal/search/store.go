package search

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func id() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func lock(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func atomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".stage-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func readJSON(path string, out any, limit int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return err
	}
	if int64(len(b)) > limit {
		return fail("search_storage")
	}
	return json.Unmarshal(b, out)
}
func (s *Service) withState(ctx context.Context, owner string, fn func(*state) error) error {
	if owner == "" {
		return fail("identity_unavailable")
	}
	path := filepath.Join(s.Data, "users", digest([]byte(owner))+".json")
	// The owner's file is shared by every source of a search. On a busy NAS
	// a write can take seconds, so the wait has no fixed limit of its own: it
	// ends with the caller (a request deadline, a cancel). Its end is the
	// NAS being busy, never a site that did not answer.
	unlock, err := lock(ctx, path+".lock")
	if err != nil {
		if ctx.Err() != nil {
			return fail("search_storage_busy")
		}
		return err
	}
	defer unlock()
	st := state{Sources: map[string]Source{}, Results: map[string]Cached{}}
	if err = readJSON(path, &st, 16<<20); err != nil && !os.IsNotExist(err) {
		return fail("search_storage")
	}
	if st.Sources == nil || st.Results == nil {
		return fail("search_storage")
	}
	before, _ := json.Marshal(st)
	migrated := st.BrowserMigration != 1
	if migrated {
		for k, src := range st.Sources {
			if strings.Contains(strings.ToLower(src.Values["cookie"]), "cf_clearance=") {
				delete(src.Values, "cookie")
				delete(src.Values, "useragent")
				src.Revision = id()
				st.Sources[k] = src
			}
		}
		for k, r := range st.Results {
			if strings.Contains(strings.ToLower(r.Source.Values["cookie"]), "cf_clearance=") {
				delete(st.Results, k)
			}
		}
		st.BrowserMigration = 1
	}
	if st.Browser == nil {
		st.Browser = map[string]BrowserTicket{}
	}
	for k, t := range st.Browser {
		if time.Since(t.Created) > time.Hour {
			delete(st.Browser, k)
		}
	}
	if err = fn(&st); err != nil {
		return err
	}
	after, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if len(after) > 16<<20 {
		return fail("search_limit")
	}
	if !migrated && string(before) == string(after) {
		return nil
	}
	return atomic(path, after)
}

// Searches hold one of slotCount package-wide flock slots per source, across
// CGI processes. A search waits up to SlotWait for a free slot.
const slotCount = 64

func (s *Service) slot(ctx context.Context) (func(), error) {
	held, err := s.slots(ctx, 1)
	if err != nil {
		return nil, err
	}
	if len(held) == 0 {
		return nil, fail("search_busy")
	}
	return held[0], nil
}

// slots takes up to n free slots, waiting while it has fewer; it returns
// what it holds when the wait ends.
func (s *Service) slots(ctx context.Context, n int) ([]func(), error) {
	dir := filepath.Join(s.Data, "slots")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	wait := s.SlotWait
	if wait == 0 {
		wait = 4 * time.Second
	}
	deadline := time.Now().Add(wait)
	held, taken := []func(){}, map[int]bool{}
	for {
		for i := 0; i < slotCount && len(held) < n; i++ {
			if taken[i] {
				continue
			}
			f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%d.lock", i)), os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				for _, release := range held {
					release()
				}
				return nil, err
			}
			if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
				taken[i] = true
				held = append(held, func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() })
				continue
			}
			f.Close()
		}
		if len(held) == n || time.Now().After(deadline) || ctx.Err() != nil {
			return held, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}
