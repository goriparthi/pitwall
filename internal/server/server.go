// Package server is the local HTTP API and UI host. Loopback only; POSTs need the per-install token and a
// same-origin Origin; the Host header is checked against DNS rebinding.
package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/goriparthi/pitwall/internal/collect/claude"
	"github.com/goriparthi/pitwall/internal/collect/demo"
	"github.com/goriparthi/pitwall/internal/collect/system"
	"github.com/goriparthi/pitwall/internal/collect/tools"
	"github.com/goriparthi/pitwall/internal/config"
	"github.com/goriparthi/pitwall/internal/launcher"
	"github.com/goriparthi/pitwall/internal/logx"
	"github.com/goriparthi/pitwall/web"
)

const bodyLimit = 16 << 10

type M = map[string]any

type UIState struct {
	Page       string `json:"page"`
	Focus      bool   `json:"focus"`
	Privacy    bool   `json:"privacy"`
	Project    string `json:"project"`
	Rotate     bool   `json:"rotate"`
	Brightness int    `json:"brightness"`
}

type Server struct {
	Cfg      *config.Store
	Log      *logx.Logger
	Demo     bool
	System   *system.Collector
	Claude   *claude.Collector
	Tools    *tools.Collector
	DemoSrc  *demo.Source
	Launcher *launcher.Launcher
	Shutdown func()

	token   string
	mu      sync.RWMutex
	ui      UIState
	display M
	clients map[chan []byte]bool
	cmu     sync.Mutex
	srv     *http.Server
}

var (
	tokenRe  = regexp.MustCompile(`^[0-9a-f]{48}$`)
	actionRe = regexp.MustCompile(`^/v1/actions/([A-Za-z0-9_-]{1,40})/run$`)
	ackRe    = regexp.MustCompile(`^/v1/attention/([^/]{1,200})/ack$`)
)

// LoadToken reuses the per-install token so open pages and hooks survive restarts.
func LoadToken() (string, error) {
	if b, err := os.ReadFile(config.TokenFile()); err == nil && tokenRe.Match(bytes.TrimSpace(b)) {
		return string(bytes.TrimSpace(b)), nil
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	t := hex.EncodeToString(buf)
	return t, os.WriteFile(config.TokenFile(), []byte(t), 0o600)
}

func (s *Server) uiFile() string { return filepath.Join(config.StateDir(), "ui.json") }

func (s *Server) Init() error {
	t, err := LoadToken()
	if err != nil {
		return err
	}
	s.token = t
	c := s.Cfg.Get()
	s.ui = UIState{Page: c.PageOrder()[0], Project: "all", Rotate: c.UI.RotatePages, Brightness: c.Display.Brightness}
	if b, err := os.ReadFile(s.uiFile()); err == nil {
		_ = json.Unmarshal(b, &s.ui)
	}
	if !s.layoutExists(s.ui.Page) {
		s.ui.Page = c.PageOrder()[0]
	}
	s.display = M{"state": "unknown"}
	s.clients = map[chan []byte]bool{}
	return nil
}

func (s *Server) layoutExists(id string) bool {
	for _, l := range s.Cfg.Get().AllLayouts() {
		if l.ID == id {
			return true
		}
	}
	return false
}

func (s *Server) UI() UIState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ui
}

func (s *Server) setUI(u UIState) {
	s.mu.Lock()
	s.ui = u
	s.mu.Unlock()
	if b, err := json.Marshal(u); err == nil {
		_ = os.WriteFile(s.uiFile(), b, 0o600)
	}
	s.broadcast()
}

// PatchUI applies a validated partial update (used by HTTP, tray and hotkeys).
func (s *Server) PatchUI(p map[string]any) UIState {
	u := s.UI()
	if v, ok := p["page"].(string); ok && s.layoutExists(v) {
		u.Page = v
	}
	for k, dst := range map[string]*bool{"focus": &u.Focus, "privacy": &u.Privacy, "rotate": &u.Rotate} {
		if v, ok := p[k].(bool); ok {
			*dst = v
		}
	}
	if v, ok := p["project"].(string); ok {
		if v == "all" {
			u.Project = v
		}
		for _, pr := range s.Cfg.Get().Projects {
			if pr.ID == v {
				u.Project = v
			}
		}
	}
	if v, ok := p["brightness"].(float64); ok {
		u.Brightness = max(5, min(100, int(v)))
	}
	s.setUI(u)
	return u
}

func (s *Server) SetDisplay(state M) {
	state["updatedAt"] = time.Now().UnixMilli()
	s.mu.Lock()
	s.display = state
	s.mu.Unlock()
}

func (s *Server) Token() string { return s.token }

func (s *Server) RunAction(id string) launcher.Result {
	if s.Demo {
		return launcher.Result{Status: 409, Error: "actions are disabled in demo mode"}
	}
	proj := s.UI().Project
	if proj == "all" {
		proj = ""
	}
	return s.Launcher.Run(id, proj)
}

func (s *Server) Ack(id string) { s.Claude.Ack(id) }

// Snapshot is the single state document every view renders from.
func (s *Server) Snapshot() M {
	c := s.Cfg.Get()
	projects := []M{}
	for _, p := range c.Projects {
		projects = append(projects, M{"id": p.ID, "name": p.Name, "path": p.Path})
	}
	s.mu.RLock()
	ui, display := s.ui, s.display
	s.mu.RUnlock()
	out := M{
		"mode": "live", "now": time.Now().UnixMilli(), "ui": ui, "display": display,
		"projects": projects, "actions": s.Launcher.List(),
		"layouts": c.AllLayouts(), "pageOrder": c.PageOrder(),
	}
	if s.Demo {
		sys, cl, tl := s.DemoSrc.Snapshot()
		out["mode"], out["system"], out["claude"], out["tools"] = "demo", sys, cl, tl
	} else {
		out["system"], out["claude"], out["tools"] = s.System.Snapshot(), s.Claude.Snapshot(), s.Tools.Snapshot()
	}
	return out
}

// Agents returns the agent list in a typed-enough form for the LED policy and tray.
func (s *Server) Agents() []map[string]any {
	b, _ := json.Marshal(s.Snapshot()["claude"])
	var cl struct {
		Agents []map[string]any `json:"agents"`
	}
	_ = json.Unmarshal(b, &cl)
	return cl.Agents
}

func (s *Server) broadcast() {
	s.cmu.Lock()
	n := len(s.clients)
	s.cmu.Unlock()
	if n == 0 {
		return
	}
	b, err := json.Marshal(s.Snapshot())
	if err != nil {
		s.Log.Error("snapshot marshal failed", M{"message": err.Error()})
		return
	}
	msg := append(append([]byte("data: "), b...), '\n', '\n')
	s.cmu.Lock()
	for ch := range s.clients {
		select {
		case ch <- msg:
		default: // slow client: drop this tick rather than block everyone
		}
	}
	s.cmu.Unlock()
}

func (s *Server) rotator(ctx context.Context) {
	last := time.Now()
	seen := map[string]bool{}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		urgent := map[string]bool{}
		for _, a := range s.Agents() {
			if st := a["status"]; st == "waiting" || st == "failed" {
				urgent[fmt.Sprint(a["id"], ":", st, ":", a["since"])] = true
			}
		}
		fresh := false
		for k := range urgent {
			if !seen[k] {
				fresh = true
			}
		}
		seen = urgent
		u := s.UI()
		order := s.Cfg.Get().PageOrder()
		// A newly urgent item jumps to the first page once; rotation pauses while anything is urgent.
		if fresh && u.Page != order[0] {
			u.Page, last = order[0], time.Now()
			s.setUI(u)
		} else if u.Rotate && len(urgent) == 0 && time.Since(last) >= time.Duration(s.Cfg.Get().UI.RotateSeconds)*time.Second {
			i := 0
			for j, id := range order {
				if id == u.Page {
					i = j
				}
			}
			u.Page, last = order[(i+1)%len(order)], time.Now()
			s.setUI(u)
		}
	}
}

func (s *Server) Start(ctx context.Context) error {
	c := s.Cfg.Get()
	addr := net.JoinHostPort(c.Server.Host, fmt.Sprint(c.Server.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s (is another instance running?): %w", addr, err)
	}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.handle), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.Log.Error("server stopped", M{"message": err.Error()})
		}
	}()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.broadcast()
			}
		}
	}()
	go s.rotator(ctx)
	s.Log.Info("listening on http://"+addr, M{"mode": map[bool]string{true: "demo", false: "live"}[s.Demo]})
	return nil
}

func (s *Server) Stop() {
	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(ctx)
	}
}

func (s *Server) allowedHost(h string) bool {
	port := fmt.Sprint(s.Cfg.Get().Server.Port)
	return h == "127.0.0.1:"+port || h == "localhost:"+port
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.Header().Set("cache-control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) authorized(r *http.Request) bool {
	t := r.Header.Get("x-pitwall-token")
	if subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) != 1 {
		return false
	}
	o := r.Header.Get("origin")
	return o == "" || s.allowedHost(strings.TrimPrefix(strings.TrimPrefix(o, "http://"), "https://"))
}

// readBody drains oversized bodies without buffering them, then reports 413 (a reset would drop the reply).
func readBody(r *http.Request) (map[string]any, int) {
	data, err := io.ReadAll(io.LimitReader(r.Body, bodyLimit+1))
	if err != nil {
		return nil, 400
	}
	if len(data) > bodyLimit {
		_, _ = io.Copy(io.Discard, r.Body)
		return nil, 413
	}
	out := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return out, 0
	}
	if json.Unmarshal(data, &out) != nil {
		return nil, 400
	}
	return out, 0
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.Log.Error("request panic", M{"path": r.URL.Path, "panic": fmt.Sprint(rec)})
			writeJSON(w, 500, M{"error": "internal error"})
		}
	}()
	if !s.allowedHost(r.Host) {
		writeJSON(w, 421, M{"error": "unexpected host"})
		return
	}
	p := r.URL.Path
	if r.Method == http.MethodGet {
		switch p {
		case "/health":
			s.mu.RLock()
			ds := s.display["state"]
			s.mu.RUnlock()
			writeJSON(w, 200, M{"ok": true, "mode": map[bool]string{true: "demo", false: "live"}[s.Demo], "display": ds, "pid": os.Getpid()})
		case "/v1/state":
			writeJSON(w, 200, s.Snapshot())
		case "/v1/stream":
			s.stream(w, r)
		default:
			s.static(w, p)
		}
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, M{"error": "method not allowed"})
		return
	}
	if !s.authorized(r) {
		writeJSON(w, 403, M{"error": "forbidden"})
		return
	}
	body, code := readBody(r)
	if code != 0 {
		writeJSON(w, code, M{"error": http.StatusText(code)})
		return
	}
	switch {
	case p == "/v1/hooks/claude":
		if s.Demo {
			writeJSON(w, 202, M{"ok": true, "ignored": "demo mode"})
		} else if s.Claude.Ingest(body) {
			writeJSON(w, 202, M{"ok": true})
		} else {
			writeJSON(w, 400, M{"error": "missing session_id or hook_event_name"})
		}
	case p == "/v1/ui":
		writeJSON(w, 200, M{"ok": true, "ui": s.PatchUI(body)})
	case p == "/v1/shutdown":
		writeJSON(w, 200, M{"ok": true})
		if s.Shutdown != nil {
			go s.Shutdown()
		}
	case actionRe.MatchString(p):
		res := s.RunAction(actionRe.FindStringSubmatch(p)[1])
		status := 200
		if !res.OK {
			status = res.Status
		}
		writeJSON(w, status, res)
	case ackRe.MatchString(p):
		s.Ack(ackRe.FindStringSubmatch(p)[1]) // r.URL.Path is already unescaped
		s.broadcast()
		writeJSON(w, 200, M{"ok": true})
	default:
		writeJSON(w, 404, M{"error": "not found"})
	}
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, M{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-store")
	ch := make(chan []byte, 4)
	s.cmu.Lock()
	s.clients[ch] = true
	s.cmu.Unlock()
	defer func() {
		s.cmu.Lock()
		delete(s.clients, ch)
		s.cmu.Unlock()
	}()
	if b, err := json.Marshal(s.Snapshot()); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := w.Write(msg); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func (s *Server) static(w http.ResponseWriter, p string) {
	rel := strings.TrimPrefix(path.Clean(p), "/")
	if rel == "" || rel == "." {
		rel = "index.html"
	}
	data, err := fs.ReadFile(web.FS, rel)
	if err != nil {
		writeJSON(w, 404, M{"error": "not found"})
		return
	}
	if rel == "index.html" {
		// the token rides in the page; other origins cannot read it, which is what makes it a CSRF guard
		data = bytes.Replace(data, []byte("__PITWALL_TOKEN__"), []byte(s.token), 1)
	}
	ct := mime.TypeByExtension(path.Ext(rel))
	if ct == "" {
		ct = "application/octet-stream"
	}
	h := w.Header()
	h.Set("content-type", ct)
	h.Set("cache-control", "no-store")
	h.Set("content-security-policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
	h.Set("x-content-type-options", "nosniff")
	_, _ = w.Write(data)
}

func (s *Server) Brightness() int { return s.UI().Brightness }

// Statuses lists agent statuses for the LED policy.
func (s *Server) Statuses() []string {
	var out []string
	for _, a := range s.Agents() {
		if st, ok := a["status"].(string); ok {
			out = append(out, st)
		}
	}
	return out
}
