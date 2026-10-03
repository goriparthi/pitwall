// Package tools reports other AI tools. Process presence is only presence ("running; task status unavailable");
// Ollama also exposes loaded models through its local API.
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

type Tool struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Processes int    `json:"processes"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
}

type known struct {
	id, label string
	names     []string
}

var catalog = []known{
	{"codex", "Codex CLI", []string{"codex"}},
	{"claude-desktop", "Claude Desktop", []string{"Claude"}},
	{"cursor", "Cursor", []string{"Cursor"}},
	{"ollama", "Ollama", []string{"ollama", "Ollama", "ollama app"}},
}

// Detect maps process names (".exe" stripped) to known tools.
func Detect(names []string) []Tool {
	counts := map[string]int{}
	for _, n := range names {
		n = strings.TrimSuffix(n, ".exe")
		for _, k := range catalog {
			for _, m := range k.names {
				if n == m {
					counts[k.id]++
				}
			}
		}
	}
	var out []Tool
	for _, k := range catalog {
		if c := counts[k.id]; c > 0 {
			out = append(out, Tool{ID: k.id, Label: k.label, Processes: c, Status: "running", Detail: "running; task status unavailable"})
		}
	}
	return out
}

type Collector struct {
	mu    sync.RWMutex
	tools []Tool
	at    int64
}

func (c *Collector) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			c.scan()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func (c *Collector) scan() {
	procs, _ := process.Processes()
	names := make([]string, 0, len(procs))
	for _, p := range procs {
		if n, err := p.Name(); err == nil {
			names = append(names, n)
		}
	}
	found := Detect(names)
	for i := range found {
		if found[i].ID != "ollama" {
			continue
		}
		if n, ok := ollamaModels(); ok {
			if n > 0 {
				found[i].Status, found[i].Detail = "loaded", plural(n, "model")+" loaded"
			} else {
				found[i].Status, found[i].Detail = "idle", "no model loaded"
			}
		}
	}
	c.mu.Lock()
	c.tools, c.at = found, time.Now().UnixMilli()
	c.mu.Unlock()
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return strings.TrimSpace(strings.Join([]string{itoa(n), w + "s"}, " "))
}

func itoa(n int) string { return strconv.Itoa(n) }

func ollamaModels() (int, bool) {
	client := http.Client{Timeout: 800 * time.Millisecond}
	res, err := client.Get("http://127.0.0.1:11434/api/ps")
	if err != nil {
		return 0, false
	}
	defer res.Body.Close()
	var body struct {
		Models []json.RawMessage `json:"models"`
	}
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&body) != nil {
		return 0, false
	}
	return len(body.Models), true
}

func (c *Collector) Snapshot() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t := c.tools
	if t == nil {
		t = []Tool{}
	}
	return map[string]any{"tools": t, "updatedAt": c.at}
}
