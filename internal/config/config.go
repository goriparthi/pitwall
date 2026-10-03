// Package config loads, validates and hot-reloads the user config; built-in defaults are embedded per OS.
package config

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed default.json
var defaultJSON []byte

type Server struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type Display struct {
	Enabled     bool   `json:"enabled"`
	Transport   string `json:"transport"`
	Rotation    int    `json:"rotation"`
	FPS         int    `json:"fps"`
	JPEGQuality int    `json:"jpegQuality"`
	Brightness  int    `json:"brightness"`
}

type LED struct {
	Enabled       bool `json:"enabled"`
	MaxBrightness int  `json:"maxBrightness"`
	WorkingGlow   bool `json:"workingGlow"`
}

type UI struct {
	RotatePages          bool     `json:"rotatePages"`
	RotateSeconds        int      `json:"rotateSeconds"`
	CompletedHoldMinutes int      `json:"completedHoldMinutes"`
	Layouts              []string `json:"layouts"`
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// Action is the only kind of thing the launcher can run. OS-specific fields come from the mac/windows overrides.
type Action struct {
	ID      string  `json:"id"`
	Label   string  `json:"label"`
	Hint    string  `json:"hint,omitempty"`
	Key     string  `json:"key,omitempty"`
	Kind    string  `json:"kind"`
	App     string  `json:"app,omitempty"`
	Target  string  `json:"target,omitempty"`
	URL     string  `json:"url,omitempty"`
	Run     string  `json:"run,omitempty"`
	Mac     *Action `json:"mac,omitempty"`
	Windows *Action `json:"windows,omitempty"`
}

// Layout is a dashboard template: grid columns and the widget in each slot.
type Layout struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Columns  []string `json:"columns"`
	Slots    []string `json:"slots"`
	Focus    bool     `json:"focus,omitempty"`
	Requires string   `json:"requires,omitempty"` // an integration; the layout is hidden when it is absent
}

// Integrations are optional data sources. "auto" turns one on only when it is found on this machine.
type Integrations struct {
	Redline string `json:"redline"` // auto | off
}

type Config struct {
	Server   Server    `json:"server"`
	Display  Display   `json:"display"`
	LED      LED       `json:"led"`
	UI       UI        `json:"ui"`
	Projects []Project `json:"projects"`
	Actions  []Action  `json:"actions"`
	Layouts      []Layout     `json:"layouts"`
	Integrations Integrations `json:"integrations"`
}

var (
	ActionKinds  = map[string]bool{"terminal": true, "open-app": true, "open-url": true}
	ReservedKeys = map[string]bool{"f": true, "h": true, "r": true, "k": true}
	Widgets      = map[string]bool{"ai": true, "health": true, "health-mini": true, "launcher": true, "usage": true, "system": true, "limits": true}
	// WidgetRequires names the integration a widget needs; its slot is dropped when that is absent.
	WidgetRequires = map[string]string{"limits": "redline"}
	keyRe        = regexp.MustCompile(`^[a-z]$`)
	idRe         = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	columnRe     = regexp.MustCompile(`^(\d{2,4}px|\d(\.\d+)?fr)$`)
)

// StateDir holds token, logs, ui state and the hook event log.
func StateDir() string {
	if d := os.Getenv("PITWALL_STATE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			return filepath.Join(la, "pitwall")
		}
	}
	return filepath.Join(home, ".local", "state", "pitwall")
}

// Path is the user config file; created from the embedded defaults on first run.
func Path() string {
	if p := os.Getenv("PITWALL_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "pitwall", "config.json")
}

func TokenFile() string { return filepath.Join(StateDir(), "token") }

func ExpandHome(p string) string {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// Resolve merges the action's override for the current OS.
func (a Action) Resolve() Action {
	o := a.Mac
	if runtime.GOOS == "windows" {
		o = a.Windows
	}
	r := a
	r.Mac, r.Windows = nil, nil
	if o != nil {
		if o.App != "" {
			r.App = o.App
		}
		if o.Target != "" {
			r.Target = o.Target
		}
		if o.URL != "" {
			r.URL = o.URL
		}
		if o.Run != "" {
			r.Run = o.Run
		}
		if o.Kind != "" {
			r.Kind = o.Kind
		}
		if o.Hint != "" {
			r.Hint = o.Hint
		}
	}
	return r
}

// Parse applies defaults and validation; returns every problem at once.
func Parse(data []byte) (*Config, error) {
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config is not valid JSON: %w", err)
	}
	applyDefaults(&c)
	var errs []string
	if c.Server.Host != "127.0.0.1" && c.Server.Host != "localhost" && c.Server.Host != "::1" {
		errs = append(errs, "server.host must be a loopback address")
	}
	switch c.Display.Rotation {
	case 0, 90, 180, 270:
	default:
		errs = append(errs, "display.rotation must be 0, 90, 180 or 270")
	}
	for i := range c.Projects {
		p := &c.Projects[i]
		if p.ID == "" || p.Name == "" || p.Path == "" {
			errs = append(errs, fmt.Sprintf("project %d needs id, name and path", i))
		}
		p.Path = ExpandHome(p.Path)
	}
	ids, keys := map[string]bool{}, map[string]bool{}
	for i := range c.Actions {
		a := c.Actions[i].Resolve()
		if !idRe.MatchString(a.ID) || a.Label == "" {
			errs = append(errs, fmt.Sprintf("action %d needs a simple id and a label", i))
		}
		if ids[a.ID] {
			errs = append(errs, "duplicate action id "+a.ID)
		}
		ids[a.ID] = true
		if !ActionKinds[a.Kind] {
			errs = append(errs, fmt.Sprintf("action %s: kind must be terminal, open-app or open-url", a.ID))
		}
		if a.Kind == "open-url" && !strings.HasPrefix(a.URL, "http://") && !strings.HasPrefix(a.URL, "https://") {
			errs = append(errs, fmt.Sprintf("action %s: url must be http(s)", a.ID))
		}
		if (a.Kind == "open-app" || a.Kind == "terminal") && a.App == "" {
			errs = append(errs, fmt.Sprintf("action %s: app is required", a.ID))
		}
		if a.Key != "" {
			switch {
			case !keyRe.MatchString(a.Key):
				errs = append(errs, fmt.Sprintf("action %s: key must be one lowercase letter", a.ID))
			case ReservedKeys[a.Key]:
				errs = append(errs, fmt.Sprintf("action %s: key %s is reserved (f focus, h privacy, r rotate, k palette)", a.ID, a.Key))
			case keys[a.Key]:
				errs = append(errs, "duplicate action key "+a.Key)
			}
			keys[a.Key] = true
		}
	}
	for _, l := range c.Layouts {
		if !idRe.MatchString(l.ID) || l.Name == "" {
			errs = append(errs, fmt.Sprintf("layout %q needs a simple id and a name", l.ID))
		}
		if len(l.Columns) != len(l.Slots) || len(l.Slots) == 0 || len(l.Slots) > 4 {
			errs = append(errs, fmt.Sprintf("layout %s: columns and slots must match, 1 to 4 of each", l.ID))
		}
		for _, col := range l.Columns {
			if !columnRe.MatchString(col) {
				errs = append(errs, fmt.Sprintf("layout %s: column %q must look like 600px or 1fr", l.ID, col))
			}
		}
		for _, w := range l.Slots {
			if !Widgets[w] {
				errs = append(errs, fmt.Sprintf("layout %s: unknown widget %q", l.ID, w))
			}
		}
		if l.Requires != "" && l.Requires != "redline" {
			errs = append(errs, fmt.Sprintf("layout %s: requires must be \"redline\" or empty", l.ID))
		}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n  "))
	}
	return &c, nil
}

func applyDefaults(c *Config) {
	if c.Server.Host == "" {
		c.Server.Host = "127.0.0.1"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 7788
	}
	if c.Display.Transport == "" {
		c.Display.Transport = "us88"
	}
	c.Display.FPS = clamp(c.Display.FPS, 1, 30, 6)
	c.Display.JPEGQuality = clamp(c.Display.JPEGQuality, 40, 100, 88)
	c.Display.Brightness = clamp(c.Display.Brightness, 5, 100, 80)
	c.LED.MaxBrightness = clamp(c.LED.MaxBrightness, 0, 100, 60)
	c.UI.RotateSeconds = clamp(c.UI.RotateSeconds, 5, 600, 20)
	c.UI.CompletedHoldMinutes = clamp(c.UI.CompletedHoldMinutes, 1, 240, 10)
	if c.Integrations.Redline != "off" {
		c.Integrations.Redline = "auto"
	}
}

func clamp(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Store holds the live config and reloads it when the file changes; invalid edits keep the previous config.
type Store struct {
	mu      sync.RWMutex
	cfg     *Config
	path    string
	modTime time.Time
	OnError func(error)
	OnLoad  func()
}

func Load() (*Store, error) {
	p := Path()
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, defaultJSON, 0o600); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(StateDir(), 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: p}
	if err := s.reload(); err != nil {
		return nil, fmt.Errorf("invalid config %s:\n  %w", p, err)
	}
	return s, nil
}

func (s *Store) Get() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) File() string { return s.path }

func (s *Store) reload() error {
	st, err := os.Stat(s.path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	c, err := Parse(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg, s.modTime = c, st.ModTime()
	s.mu.Unlock()
	return nil
}

// Watch polls the file's mtime; portable and cheap at a 2 s interval.
func (s *Store) Watch(stop <-chan struct{}) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			st, err := os.Stat(s.path)
			if err != nil || !st.ModTime().After(s.modTime) {
				continue
			}
			if err := s.reload(); err != nil {
				s.mu.Lock()
				s.modTime = st.ModTime()
				s.mu.Unlock()
				if s.OnError != nil {
					s.OnError(err)
				}
			} else if s.OnLoad != nil {
				s.OnLoad()
			}
		}
	}
}
