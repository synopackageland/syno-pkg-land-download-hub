package binaries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type PluginUpdate struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type PluginResult struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}
type pluginRecord struct {
	Repository *struct {
		URL       string `json:"url"`
		Directory string `json:"directory"`
	} `json:"repository"`
	Identity string `json:"identity"`
	Version  string `json:"version"`
	Disabled bool   `json:"disabled"`
	DevMode  bool   `json:"devMode"`
	Settings []struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Value any    `json:"value"`
	} `json:"settings"`
}

// Only stable numeric extension releases are compared. Unknown version formats
// do not authorize an upgrade or an automatic downgrade.
func NewerVersion(candidate, current string) bool {
	parse := func(raw string) ([3]int, bool) {
		var out [3]int
		parts := strings.Split(raw, ".")
		if len(parts) < 2 || len(parts) > 3 {
			return out, false
		}
		for i, p := range parts {
			if !regexp.MustCompile(`^[0-9]{1,6}$`).MatchString(p) {
				return out, false
			}
			out[i], _ = strconv.Atoi(p)
		}
		return out, true
	}
	a, ok := parse(candidate)
	if !ok {
		return false
	}
	b, ok := parse(current)
	if !ok {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// HoldsCoreUpdate reports an engine task status a core update waits for:
// any unfinished download.
func HoldsCoreUpdate(status string) bool { return status != "done" && status != "error" }

// HoldsExtensionUpdate reports an engine task status an extension update
// waits for: a download that runs or is queued to. A paused one resumes with
// the new version.
func HoldsExtensionUpdate(status string) bool { return HoldsCoreUpdate(status) && status != "pause" }

// Snapshot the complete engine store only while idle and stopped. This includes
// extension KV storage and engine metadata, so rollback preserves settings and
// disabled state without editing Gopeed's private database schema.
func pluginTransaction(ctx context.Context, data, endpoint, request string, plugin PluginUpdate, halt func(), boot func() error) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_@.-]{1,200}$`).MatchString(plugin.ID) {
		return errors.New("plugin_source_denied")
	}
	var tasks []struct{ Status string }
	if e := engineCall(ctx, data, endpoint, "GET", "/api/v1/tasks", nil, &tasks); e != nil {
		return errors.New("engine_unavailable")
	}
	for _, task := range tasks {
		if HoldsExtensionUpdate(task.Status) {
			return errors.New("downloads_running")
		}
	}
	path := "/api/v1/extensions/" + url.PathEscape(plugin.ID)
	var before pluginRecord
	if e := engineCall(ctx, data, endpoint, "GET", path, nil, &before); e != nil || before.Identity != plugin.ID || before.DevMode || !NewerVersion(plugin.Version, before.Version) {
		return errors.New("plugin_release_changed")
	}
	halt()
	snapshot := filepath.Join(data, "plugin-snapshot")
	if e := os.RemoveAll(snapshot); e != nil {
		boot()
		return errors.New("plugin_snapshot_failed")
	}
	if e := copyStore(filepath.Join(data, "storage"), snapshot); e != nil {
		boot()
		return errors.New("plugin_snapshot_failed")
	}
	marker := filepath.Join(data, "plugin-transaction.json")
	if e := Atomic(marker, map[string]string{"request": request, "plugin": plugin.ID}); e != nil {
		boot()
		return errors.New("plugin_snapshot_failed")
	}
	failure := boot()
	if failure == nil {
		failure = engineCall(ctx, data, endpoint, "POST", path+"/update", nil, nil)
	}
	var after pluginRecord
	if failure == nil {
		failure = engineCall(ctx, data, endpoint, "GET", path, nil, &after)
	}
	if failure == nil && (after.Identity != before.Identity || after.Version != plugin.Version || after.Disabled != before.Disabled || after.DevMode || !reflect.DeepEqual(before.Repository, after.Repository) || !preservesSettings(before, after)) {
		failure = errors.New("plugin verification failed")
	}
	if failure == nil {
		if e := removeDurable(marker); e != nil {
			return errors.New("plugin_commit_uncertain")
		}
		os.RemoveAll(snapshot)
		return nil
	}
	halt()
	if _, e := recoverPlugin(data); e != nil {
		return errors.New("plugin_recovery_failed")
	}
	if e := boot(); e != nil {
		return errors.New("plugin_recovery_failed")
	}
	return errors.New("plugin_update_failed_previous_restored")
}
func preservesSettings(before, after pluginRecord) bool {
	for _, old := range before.Settings {
		found := false
		for _, next := range after.Settings {
			if old.Name == next.Name {
				found = old.Type == next.Type && reflect.DeepEqual(old.Value, next.Value)
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func removeDurable(path string) error {
	if e := os.Remove(path); e != nil {
		return e
	}
	f, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func copyStore(source, dest string) error {
	var total int64
	count := 0
	e := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > 20000 {
			return errors.New("snapshot entry bound")
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(source, path)
		if e != nil {
			return e
		}
		target := filepath.Join(dest, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !info.Mode().IsRegular() {
			return errors.New("unsafe snapshot file")
		}
		total += info.Size()
		if total > 512<<20 {
			return errors.New("snapshot byte bound")
		}
		in, e := os.Open(path)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		n, e := io.CopyN(out, in, info.Size())
		if e == nil && n == info.Size() {
			e = out.Sync()
		}
		ce := out.Close()
		if e != nil {
			return e
		}
		return ce
	})
	if e != nil {
		return e
	}
	return filepath.WalkDir(dest, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer f.Close()
		return f.Sync()
	})
}

// The snapshot is retained until the restore commit is durable. Re-entering
// recovery after any rename/crash copies the same known complete old store.
func recoverPlugin(data string) (string, error) {
	b, e := os.ReadFile(filepath.Join(data, "plugin-transaction.json"))
	if errors.Is(e, os.ErrNotExist) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	var marker struct{ Request, Plugin string }
	if len(b) > 4096 || json.Unmarshal(b, &marker) != nil || marker.Request == "" {
		return "", errors.New("invalid plugin recovery marker")
	}
	replacement := filepath.Join(data, "plugin-restore")
	discard := filepath.Join(data, "plugin-discard")
	if e = os.RemoveAll(replacement); e != nil {
		return "", e
	}
	if e = copyStore(filepath.Join(data, "plugin-snapshot"), replacement); e != nil {
		return "", e
	}
	if e = os.RemoveAll(discard); e != nil {
		return "", e
	}
	if e = os.Rename(filepath.Join(data, "storage"), discard); e != nil && !errors.Is(e, os.ErrNotExist) {
		return "", e
	}
	if e = os.Rename(replacement, filepath.Join(data, "storage")); e != nil {
		return "", e
	}
	f, e := os.Open(data)
	if e != nil {
		return "", e
	}
	e = f.Sync()
	f.Close()
	if e != nil {
		return "", e
	}
	if state, read := ReadRuntime(data); read == nil {
		state.Request = marker.Request
		state.Ready = false
		state.Error = "plugin_update_failed_previous_restored"
		state.Plugin = &PluginResult{ID: marker.Plugin, Error: state.Error}
		if e = Atomic(filepath.Join(data, "runtime.json"), state); e != nil {
			return "", e
		}
	}
	if e = removeDurable(filepath.Join(data, "plugin-transaction.json")); e != nil {
		return "", e
	}
	os.RemoveAll(discard)
	os.RemoveAll(filepath.Join(data, "plugin-snapshot"))
	return marker.Request, nil
}

func (m Manager) UpdatePlugin(ctx context.Context, plugin PluginUpdate) (Runtime, error) {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	if e := Atomic(filepath.Join(m.Data, "engine-request.json"), Request{ID: id, Plugin: &plugin}); e != nil {
		return Runtime{}, e
	}
	timer := time.NewTimer(150 * time.Second)
	defer timer.Stop()
	for {
		st, e := ReadRuntime(m.Data)
		if e == nil && st.Request == id {
			if st.Error != "" {
				return st, errors.New(st.Error)
			}
			if !st.Ready {
				return st, errors.New("engine_unavailable")
			}
			return st, nil
		}
		select {
		case <-ctx.Done():
			return Runtime{}, errors.New("plugin_update_pending")
		case <-timer.C:
			return Runtime{}, errors.New("plugin_update_pending")
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func (m Manager) Pending() bool {
	b, e := os.ReadFile(filepath.Join(m.Data, "engine-request.json"))
	if errors.Is(e, os.ErrNotExist) {
		return false
	}
	var req Request
	if e != nil || len(b) > 16384 || json.Unmarshal(b, &req) != nil {
		return true
	}
	st, e := ReadRuntime(m.Data)
	return e != nil || req.ID != st.Request
}
