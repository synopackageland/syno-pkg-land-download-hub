package gopeed

import (
	"bytes"
	"context"
	"dsmapp/internal/multisave"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"dsmapp/internal/binaries"
	"dsmapp/internal/media"
	"dsmapp/internal/search"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
	"github.com/synopackageland/syno-pkg-land-sdk/filestation"
)

const EngineURL = "http://127.0.0.1:19893"

type Service struct {
	Search              *search.Service
	SearchIdentity      func(string) (background.Submitter, error)
	saveTimeout         time.Duration // Zero uses the SDK request allowance.
	metadataWait        time.Duration // Zero uses the production BT metadata budget.
	Delegate            BackgroundSave
	DestinationDelegate interface {
		SelectDestination(context.Context, api.Caller, *http.Request, delegate.SelectDestinationParams) (delegate.DestinationGrant, error)
		Destination(context.Context, api.Caller, *http.Request, string) (delegate.DestinationState, error)
		CheckDestination(context.Context, api.Caller, *http.Request, string, string) error
	}
	StagingDir string
	// Executor is the SDK's client of the package's executor, reading the
	// package's lifecycle mailbox; nil leaves an unreachable engine unexplained.
	Executor delegate.ExecutorClient
	// Media routes media page addresses to the media downloader; nil disables it.
	Media MediaDetector
	// MediaEnv and MediaExec default to the managed tools and a bounded run.
	MediaEnv  func() media.Env
	MediaExec media.Exec
	// MediaTools defaults to the package-owned tool store; tests replace it.
	MediaTools func() media.Tools
	// coreHTTP reaches the Gopeed release source; nil keeps it to GitHub.
	coreHTTP *http.Client
	// githubHTTP checks a new GitHub token; nil reaches GitHub itself.
	githubHTTP *http.Client
	CatalogURL string
	checkURL   func(string) bool
	Data       string
	Engine     string
	HTTP       *http.Client
	// fetch reaches user-supplied addresses (torrent files, Content-Type
	// probes); nil uses a client that refuses non-public destinations.
	fetch *http.Client
	Owner func(string) (string, error)
}

func New(data string) *Service {
	// DSM's /var/packages/<id>/var aliases the volume path used by the daemon.
	// Gopeed's download whitelist compares paths literally.
	if canonical, err := filepath.EvalSymlinks(data); err == nil {
		data = canonical
	}
	return &Service{Data: data, StagingDir: filepath.Join(filepath.Dir(data), "executor-staging"), Engine: EngineURL, HTTP: &http.Client{Transport: &http.Transport{Proxy: nil, DisableCompression: true, ResponseHeaderTimeout: 8 * time.Second}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Owner: func(name string) (string, error) {
		u, e := user.Lookup(name)
		if e != nil {
			return "", e
		}
		return u.Uid + ":" + name, nil
	}}
}
func refused(code string) error {
	return &api.Error{Code: code, Message: "Gopeed operation refused: " + code}
}

// sourceRefused tells a GitHub rate limit, with the time it resets, apart
// from an unreachable source and from a release that failed verification.
func sourceRefused(err error) error {
	if refusal := githubRefusal("tool_source_rate_limited", err); refusal != nil {
		return refusal
	}
	switch {
	case errors.Is(err, binaries.ErrUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return refused("tool_source_unavailable")
	}
	return refused("tool_release_unverified")
}
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
func (s *Service) Routes() []api.Route {
	routes := append(append(append(append(s.adminRoutes(), s.githubRoutes()...), s.mediaRoutes()...), s.batchRoutes()...), s.searchRoutes()...)
	routes = append(routes, s.mediaUserRoutes()...)
	routes = append(routes, s.folderRoutes()...)
	routes = append(routes, s.ctlRoutes()...)
	routes = append(routes, previewRoutes()...)
	handler := func(method string) api.RequestHandler {
		return func(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
			if method == "gopeed.publish" {
				owner, e := s.Owner(c.User())
				if e != nil {
					return nil, refused("identity_unavailable")
				}
				jobCtx, cancel := context.WithTimeout(ctx, s.saveBudget())
				defer cancel()
				return s.publish(jobCtx, r, c.User(), owner, raw)
			}
			// Choosing a folder never touches the engine. The SDK picker round
			// trip runs outside every lock; only the binding takes jobs.lock.
			if method == "gopeed.destination" {
				owner, e := s.Owner(c.User())
				if e != nil {
					return nil, refused("identity_unavailable")
				}
				in, selected, e := s.pickDestination(ctx, c, r, raw)
				if e != nil {
					return nil, e
				}
				return s.withState(ctx, func(st *State) (any, error) { return s.bindDestination(ctx, c, r, in, selected, st, owner) })
			}
			if method == "gopeed.request" {
				var in Input
				if e := decodeStrict(raw, &in); e != nil {
					return nil, e
				}
				if in.Method == "GET" && in.Path == "/api/v1/tasks" {
					owner, err := s.Owner(c.User())
					if err != nil {
						return nil, refused("identity_unavailable")
					}
					return s.readTaskList(ctx, owner, in)
				}
				if m := taskPath.FindStringSubmatch(in.Path); m != nil && in.Method == "PUT" && (m[2] == "/pause" || m[2] == "/continue") {
					if _, err := requestQuery(in); err != nil {
						return nil, err
					}
					owner, err := s.Owner(c.User())
					if err != nil {
						return nil, refused("identity_unavailable")
					}
					return s.controlTask(ctx, owner, m[1], strings.TrimPrefix(m[2], "/"))
				}
				if m := taskPath.FindStringSubmatch(in.Path); in.Method == "DELETE" && m != nil && m[2] == "" {
					if len(in.Query) > 4 {
						return nil, refused("invalid_request")
					}
					for key := range in.Query {
						if key != "force" && key != "status" {
							return nil, refused("endpoint_denied")
						}
					}
					owner, e := s.Owner(c.User())
					if e != nil {
						return nil, refused("identity_unavailable")
					}
					jobCtx, cancel := context.WithTimeout(ctx, s.saveBudget())
					defer cancel()
					return s.deleteTask(jobCtx, owner, m[1])
				}
			}
			return s.locked(ctx, c, func(owner string, st *State) (any, error) {
				switch method {
				case "gopeed.reveal":
					var in struct {
						ID string `json:"id"`
					}
					if e := decodeStrict(raw, &in); e != nil {
						return nil, e
					}
					j, ok := st.Jobs[in.ID]
					if !ok || j.Owner != owner {
						return nil, refused("task_denied")
					}
					if !j.Published {
						return nil, refused("not_published")
					}
					// Paths here are owner-bound navigation metadata. The browser
					// checks current File Station access before asking DSM to reveal.
					if !validFolder(j.Destination) || !validName(revealName(j)) {
						return nil, refused("invalid_file")
					}
					return map[string]string{"folder": j.destinationPath(), "filename": revealName(j)}, nil
				case "gopeed.folder":
					return nil, refused("background_destination_required")
				case "gopeed.pending":
					if e := decodeStrict(raw, &struct{}{}); e != nil {
						return nil, e
					}
					return s.pending(ctx, st, owner)
				default:
					return s.request(ctx, r, c, owner, st, raw)
				}
			})
		}
	}
	return append(routes,
		api.HandleRequest("gopeed.request", handler("gopeed.request")),
		api.HandleRequest("gopeed.folder", handler("gopeed.folder")),
		api.HandleRequest("gopeed.pending", handler("gopeed.pending")),
		api.HandleRequest("gopeed.publish", handler("gopeed.publish")),
		api.HandleRequest("gopeed.reveal", handler("gopeed.reveal")),
		api.HandleRequest("gopeed.destination", handler("gopeed.destination")),
	)
}

// locked runs fn for the caller under the engine mutation lock and jobs.lock.
func (s *Service) locked(ctx context.Context, c api.Caller, fn func(owner string, st *State) (any, error)) (any, error) {
	owner, e := s.Owner(c.User())
	if e != nil {
		return nil, refused("identity_unavailable")
	}
	return s.lockedOwner(ctx, owner, fn)
}

func (s *Service) lockedOwner(ctx context.Context, owner string, fn func(string, *State) (any, error)) (any, error) {
	lockCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	unlock, e := (binaries.Manager{Data: s.Data}).Lock(lockCtx)
	if e != nil {
		return nil, refused("engine_busy")
	}
	defer unlock()
	if (binaries.Manager{Data: s.Data}).Pending() {
		return nil, refused("engine_busy")
	}
	return s.withState(ctx, func(st *State) (any, error) { return fn(owner, st) })
}
func validFolder(p string) bool {
	return len(p) > 1 && len(p) <= 4096 && strings.HasPrefix(p, "/") && path.Clean(p) == p && !strings.ContainsAny(p, "\x00\r\n\\,\"")
}
func validName(n string) bool {
	return n != "" && n != "." && n != ".." && len(n) <= 255 && !strings.ContainsAny(n, "/\\\x00\r\n,\"")
}
func (s *Service) destination(ctx context.Context, r *http.Request, caller api.Caller, folder string) error {
	if !validFolder(folder) {
		return refused("invalid_folder")
	}
	if s.DestinationDelegate == nil {
		return refused("save_unavailable")
	}
	return s.DestinationDelegate.CheckDestination(ctx, caller, r, "save", folder)
}

type Input struct {
	Method string                     `json:"method"`
	Path   string                     `json:"path"`
	Query  map[string]json.RawMessage `json:"query"`
	Data   json.RawMessage            `json:"data"`
}
type Reply struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg,omitempty"`
	Data json.RawMessage `json:"data"`
}

func (s *Service) engine(ctx context.Context, method, p string, q url.Values, data any) (Reply, error) {
	return s.engineVia(ctx, s.HTTP, method, p, q, data)
}

// unsupportedProtocol is upstream's ErrUnSupportedProtocol text (Gopeed
// 1.9.3 pkg/download/downloader.go).
const unsupportedProtocol = "unsupported protocol"

// engineVia sends one private-token engine request with the given client.
func (s *Service) engineVia(ctx context.Context, client *http.Client, method, p string, q url.Values, data any) (Reply, error) {
	var reply Reply
	if !strings.HasPrefix(p, "/api/v1/") || strings.ContainsAny(p, "?#\r\n") {
		return reply, refused("endpoint_denied")
	}
	token, e := os.ReadFile(filepath.Join(s.Data, "engine.token"))
	if e != nil || len(token) != 64 {
		return reply, refused("engine_unavailable")
	}
	var b []byte
	if data != nil {
		b, e = json.Marshal(data)
		if e != nil {
			return reply, e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, s.Engine+p+"?"+q.Encode(), bytes.NewReader(b))
	if e != nil {
		return reply, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Token", string(token))
	resp, e := client.Do(req)
	if e != nil {
		return reply, s.unreachable(ctx)
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if e != nil || len(raw) > 2<<20 || resp.StatusCode != 200 || json.Unmarshal(raw, &reply) != nil {
		return reply, refused("engine_unavailable")
	}
	// Upstream diagnostics may contain sensitive URLs; never forward them.
	// Its fixed unsupported-protocol text names no URL and tells an address
	// no fetcher takes from a source that failed.
	if reply.Code != 0 && reply.Msg != unsupportedProtocol {
		reply.Msg = "Gopeed request failed"
	}
	return reply, nil
}

// unreachable names why the engine cannot answer: the package is stopped or
// starting (the SDK's executor client says so from the lifecycle mailbox), or
// the supervisor records an engine that is starting, restarting or in a
// crash loop in runtime.json.
func (s *Service) unreachable(ctx context.Context) error {
	if e := s.lifecycle(ctx); e != nil {
		return e
	}
	st, e := binaries.ReadRuntime(s.Data)
	switch {
	case e != nil:
	case st.StartingAt(time.Now()):
		return refused(delegate.CodeAppStarting)
	case st.Error == "engine_restarting" || st.Error == "engine_crash_loop":
		return refused(st.Error)
	}
	return refused("engine_unavailable")
}

// lifecycle is the executor client's refusal for a stopped or starting
// package; nil for any other answer.
func (s *Service) lifecycle(ctx context.Context) error {
	if s.Executor == nil {
		return nil
	}
	err := s.Executor.Call(ctx, "status", nil, nil, &json.RawMessage{})
	var refusal *api.Error
	if errors.As(err, &refusal) && (refusal.Code == delegate.CodeAppStopped || refusal.Code == delegate.CodeAppStarting || refusal.Code == delegate.CodeExecutorStarting) {
		return refusal
	}
	return nil
}

var taskPath = regexp.MustCompile(`^/api/v1/tasks/([A-Za-z0-9_-]{1,128})(/(pause|continue|stats))?$`)

func (s *Service) request(ctx context.Context, r *http.Request, caller api.Caller, owner string, st *State, raw json.RawMessage) (any, error) {
	var in Input
	if e := decodeStrict(raw, &in); e != nil {
		return nil, e
	}
	q, err := requestQuery(in)
	if err != nil {
		return nil, err
	}
	if in.Path == "/api/v1/removals" && in.Method == "GET" {
		rows := []Removal{}
		for _, r := range st.Removals {
			if r.Owner == owner {
				r.Owner = ""
				rows = append(rows, r)
			}
		}
		data, _ := json.Marshal(rows)
		return Reply{Data: data}, nil
	}
	if in.Path == "/api/v1/info" && in.Method == "GET" {
		return s.engine(ctx, "GET", in.Path, nil, nil)
	}
	if in.Path == "/api/v1/extensions" && in.Method == "GET" {
		return Reply{Data: json.RawMessage(`[]`)}, nil
	}
	if in.Path == "/api/v1/config" && in.Method == "GET" {
		data, _ := json.Marshal(map[string]any{"downloadDir": st.Folders[owner], "backgroundSave": st.Grants[owner] != ""})
		return Reply{Code: 0, Data: data}, nil
	}
	if in.Path == "/api/v1/tasks" && in.Method == "GET" {
		rep, e := s.engine(ctx, "GET", in.Path, q, nil)
		if e != nil {
			return nil, e
		}
		if rep.Code != 0 {
			return rep, nil
		}
		return s.filterTaskList(st, owner, rep)
	}
	if m := taskPath.FindStringSubmatch(in.Path); m != nil {
		job, ok := st.Jobs[m[1]]
		if !ok || job.Owner != owner {
			return nil, refused("task_denied")
		}
		if job.DeleteRequested && in.Method == "PUT" {
			return nil, refused("task_deleting")
		}
		if job.Preparation != nil {
			return s.preparationRequest(st, m[1], job, in, m[2])
		}
		if job.Media != nil {
			return s.mediaRequest(st, m[1], job, in, m[2])
		}
		allowed := (m[2] == "" && in.Method == "GET") || (m[2] == "/stats" && in.Method == "GET") || ((m[2] == "/pause" || m[2] == "/continue") && in.Method == "PUT")
		if !allowed {
			return nil, refused("endpoint_denied")
		}

		rep, e := s.engine(ctx, in.Method, in.Path, q, nil)
		if e != nil {
			return nil, e
		}
		if in.Method == "GET" && m[2] == "" && rep.Code == 0 {
			var t map[string]any
			if json.Unmarshal(rep.Data, &t) != nil {
				return nil, refused("engine_unavailable")
			}
			s.present(t, job)
			rep.Data, _ = json.Marshal(t)
		}
		if in.Method == "PUT" && rep.Code == 0 && (m[2] == "/pause" || m[2] == "/continue") {
			job.ManualPaused = m[2] == "/pause"
			st.Jobs[m[1]] = job
		}
		if in.Method == "DELETE" && rep.Code == 0 {
			delete(st.Jobs, m[1])
		}
		return rep, nil
	}
	return nil, refused("endpoint_denied")
}
func (s *Service) present(task map[string]any, job Job) {
	if job.ManualPaused && (!job.Published || job.Hash != "") {
		task["status"] = "pause"
	}
	if strings.HasPrefix(job.BatchKey, "search-") {
		hideSearchSource(task)
	}
	task["publication"] = map[string]any{"published": job.Published, "uncertain": job.Uncertain, "error": job.SaveError, "reason": job.SaveReason, "folder": job.destinationPath(), "filename": job.Filename, "state": job.SaveState, "delegated": job.DelegationID != "", "retryable": job.retryable()}
	task["publication"].(map[string]any)["manualPaused"] = job.ManualPaused
	// The New download dialog finds its own torrents by their batch key.
	task["publication"].(map[string]any)["batch"] = job.BatchKey
	task["publication"].(map[string]any)["pausePending"] = job.SavePausePending || job.ControlPending == "pause"
	if job.ManualPaused && (job.SavePausePending || job.ControlPending == "pause") {
		task["status"] = "pausing"
	}

	if job.DeleteRequested {
		publication := task["publication"].(map[string]any)
		publication["state"] = "canceling"
		publication["error"] = ""
		publication["reason"] = ""
		publication["uncertain"] = false
		publication["deleting"] = true
	}
	if meta, ok := task["meta"].(map[string]any); ok {
		if opts, ok := meta["opts"].(map[string]any); ok {
			opts["path"] = job.destinationPath()
		}
	}
	if job.multiFile() {
		presentFiles(task, job)
	}
}

// safeURL admits public HTTP(S) addresses without credentials.
func safeURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return false
	}
	return publicHostname(u.Hostname())
}
func decodeTask(raw json.RawMessage) (map[string]any, error) {
	var body map[string]any
	if len(raw) > 768<<10 || json.Unmarshal(raw, &body) != nil || body == nil {
		return nil, refused("invalid_request")
	}
	for k := range body {
		if k != "req" && k != "opts" && k != "rid" {
			return nil, refused("invalid_request")
		}
	}
	return body, nil
}
func (s *Service) resolve(ctx context.Context, r *http.Request, caller api.Caller, owner string, st *State, raw json.RawMessage) (any, error) {
	return s.resolveSourceStep(ctx, sourceAccess{caller: caller, request: r, owner: owner}, owner, st, raw)
}

func (s *Service) resolveSourceStep(ctx context.Context, a sourceAccess, owner string, st *State, raw json.RawMessage) (any, error) {
	body, e := decodeTask(raw)
	if e != nil {
		return nil, e
	}
	req, ok := body["req"].(map[string]any)
	if !ok {
		return nil, refused("invalid_request")
	}
	rawURL, _ := req["url"].(string)
	if !s.allowedURL(rawURL) {
		return nil, refused("url_denied")
	}
	address, e := engineAddress(rawURL)
	if e != nil {
		return nil, e
	}
	req["url"] = address
	// The engine starts its one ED2K client with the config of the first
	// ED2K task, so the firewall-facing ports are set before that.
	if strings.HasPrefix(strings.ToLower(address), "ed2k:") {
		if _, e = s.ensureProtocolConfig(ctx); e != nil {
			return nil, e
		}
	}
	opts, ok := body["opts"].(map[string]any)
	if !ok {
		return nil, refused("folder_required")
	}
	folder, _ := opts["path"].(string)
	if e = a.destination(ctx, s, folder); e != nil {
		return nil, e
	}
	if len(st.Jobs)+len(st.Resolved) >= 128 {
		return nil, refused("queue_limit")
	}
	current, err := a.current(ctx, s)
	if err != nil {
		return nil, err
	}
	if current.Path != folder {
		return nil, refused("background_destination_required")
	}
	grantID := current.ID
	st.Folders[owner], st.Grants[owner] = current.Path, current.ID
	root := s.StagingDir
	stage := filepath.Join(root, randomID())
	if e = os.MkdirAll(stage, 0700); e != nil {
		return nil, e
	}
	// Only a recorded preview keeps its stage; a refused resolve leaves none.
	defer func() {
		if !stageInUse(st, stage) {
			os.Remove(stage)
		}
	}()
	opts["path"] = stage
	delete(opts, "extra")
	delete(req, "extra")
	req["skipVerifyCert"] = false
	rep, e := s.engine(ctx, "POST", "/api/v1/resolve", nil, body)
	if e != nil {
		return nil, e
	}
	// Upstream answers ErrUnSupportedProtocol's text when neither a protocol
	// nor an enabled extension accepts the address; any other refusal is the
	// source's own failure (EOF, an HTTP error), which no extension fixes.
	if rep.Code != 0 {
		if rep.Msg == unsupportedProtocol {
			return nil, refused("source_unsupported")
		}
		return nil, refused("source_unreachable")
	}
	var data struct {
		ID  string            `json:"id"`
		Res extensionResource `json:"res"`
	}
	if json.Unmarshal(rep.Data, &data) != nil {
		return nil, refused("engine_unavailable")
	}
	filename, _ := opts["name"].(string)
	if data.ID == "" {
		// Only extensions answer without a resolve ID.
		return s.previewExtension(st, Job{Subfolder: sourceSubfolder(a, st, owner), Owner: owner, Destination: folder, DelegationID: grantID}, data.Res, filename)
	}
	if len(data.Res.Files) != 1 || data.Res.Files[0].Path != "" || data.Res.Files[0].Size > filestation.MaxUploadBytes {
		return nil, refused("single_file_required")
	}
	if filename == "" {
		filename = multisave.SafeFilename(data.Res.Files[0].Name)
	}
	if !validName(filename) {
		return nil, refused("invalid_filename")
	}
	// Gopeed caches resolve options by RID; create ignores replacement options.
	// Resolve again with the safe leaf before exposing a preview. Only this
	// unsubmitted resolve is replaced; no download or save has been admitted.
	if filename != data.Res.Files[0].Name && opts["name"] != filename {
		opts["name"] = filename
		rep, e = s.engine(ctx, "POST", "/api/v1/resolve", nil, body)
		if e != nil {
			return nil, e
		}
		if rep.Code != 0 {
			return nil, refused("source_unreachable")
		}
		data.ID, data.Res = "", extensionResource{}
		if json.Unmarshal(rep.Data, &data) != nil || !engineTaskID.MatchString(data.ID) || len(data.Res.Files) != 1 || data.Res.Files[0].Path != "" || data.Res.Files[0].Size > filestation.MaxUploadBytes {
			return nil, refused("single_file_required")
		}
	}
	// The dialog keeps one resolve; an earlier unsubmitted one is discarded.
	for rid, other := range st.Resolved {
		if other.Owner == owner && other.Kind == "" && other.SubmissionRID == "" {
			delete(st.Resolved, rid)
			s.removeEmptyStage(st, other.Stage)
		}
	}
	st.Resolved[data.ID] = Job{Subfolder: sourceSubfolder(a, st, owner), Owner: owner, Stage: stage, Destination: folder, Filename: filename, DelegationID: grantID}
	return rep, nil
}
func (s *Service) pending(ctx context.Context, st *State, owner string) (any, error) {
	out := []map[string]any{}
	for id, j := range st.Jobs {
		if j.Owner != owner || j.Published {
			continue
		}
		rep, e := s.engine(ctx, "GET", "/api/v1/tasks/"+id, nil, nil)
		if e != nil {
			return nil, e
		}
		var t struct {
			Status string `json:"status"`
		}
		if rep.Code == 0 && json.Unmarshal(rep.Data, &t) == nil && t.Status == "done" {
			out = append(out, map[string]any{"id": id, "filename": j.Filename, "folder": j.Destination, "uncertain": j.Uncertain})
		}
	}
	return out, nil
}
