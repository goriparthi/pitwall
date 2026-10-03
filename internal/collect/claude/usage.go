package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// Usage is measured from transcript "usage" fields; content fields are never decoded.
type Usage struct {
	Title            string `json:"-"`
	Model            string `json:"model"`
	ContextTokens    int64  `json:"contextTokens"`
	InputTokens      int64  `json:"inputTokens"`
	OutputTokens     int64  `json:"outputTokens"`
	CacheReadTokens  int64  `json:"cacheReadTokens"`
	CacheWriteTokens int64  `json:"-"`
	Turns            int    `json:"turns"`
	TodayInput       int64  `json:"-"`
	TodayOutput      int64  `json:"-"`
	TodayTurns       int    `json:"-"`
}

type msgUsage struct{ in, out, cr, cw int64 }

// Accumulator dedupes streamed lines by message id (each id counted once with its latest numbers).
type Accumulator struct {
	U        Usage
	seen     map[string]msgUsage
	turnIDs  map[string]bool
	dayStart func() int64
}

func NewAccumulator() *Accumulator {
	return &Accumulator{seen: map[string]msgUsage{}, turnIDs: map[string]bool{}, dayStart: StartOfToday}
}

func StartOfToday() int64 {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location()).UnixMilli()
}

// Only the fields needed for accounting; message content is skipped by the decoder.
type line struct {
	Type        string `json:"type"`
	AITitle     string `json:"aiTitle"`
	UUID        string `json:"uuid"`
	IsSidechain bool   `json:"isSidechain"`
	Timestamp   string `json:"timestamp"`
	Message     *struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage      *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

var (
	tagAssistant = []byte(`"assistant"`)
	tagTitle     = []byte(`"ai-title"`)
)

func (a *Accumulator) Line(b []byte) {
	if !bytes.Contains(b, tagAssistant) && !bytes.Contains(b, tagTitle) {
		return
	}
	var d line
	if json.Unmarshal(b, &d) != nil {
		return
	}
	if d.Type == "ai-title" && d.AITitle != "" {
		a.U.Title = trunc(d.AITitle, 90)
		return
	}
	if d.Type != "assistant" || d.Message == nil || d.Message.Usage == nil {
		return
	}
	u := d.Message.Usage
	id := d.Message.ID
	if id == "" {
		id = d.UUID
	}
	cur := msgUsage{in: u.Input + u.CacheRead + u.CacheWrite, out: u.Output, cr: u.CacheRead, cw: u.CacheWrite}
	ts := time.Now().UnixMilli()
	if t, err := time.Parse(time.RFC3339Nano, d.Timestamp); err == nil {
		ts = t.UnixMilli()
	}
	if !d.IsSidechain {
		if d.Message.Model != "" {
			a.U.Model = d.Message.Model
		}
		a.U.ContextTokens = cur.in + cur.out
	}
	prev := a.seen[id]
	a.seen[id] = cur
	dIn, dOut := cur.in-prev.in, cur.out-prev.out
	a.U.InputTokens += dIn
	a.U.OutputTokens += dOut
	a.U.CacheReadTokens += cur.cr - prev.cr
	a.U.CacheWriteTokens += cur.cw - prev.cw
	turnEnd := !d.IsSidechain && d.Message.StopReason == "end_turn" && !a.turnIDs[id]
	if turnEnd {
		a.turnIDs[id] = true
		a.U.Turns++
	}
	if ts >= a.dayStart() {
		a.U.TodayInput += dIn
		a.U.TodayOutput += dOut
		if turnEnd {
			a.U.TodayTurns++
		}
	}
}

// Tail reads a JSONL file incrementally from the last offset; a shrunk file restarts accounting.
type Tail struct {
	mu     sync.Mutex
	Path   string
	offset int64
	rest   []byte
	Acc    *Accumulator
	Found  bool
	Day    int64
}

func NewTail(path string) *Tail { return &Tail{Path: path, Acc: NewAccumulator(), Day: StartOfToday()} }

// Snap returns a consistent copy of the totals.
func (t *Tail) Snap() (Usage, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Acc.U, t.Found
}

// Read is called by one goroutine per tail; file I/O happens unlocked and each chunk is applied under a
// short lock, so a large first read never stalls snapshots.
func (t *Tail) Read() {
	st, err := os.Stat(t.Path)
	t.mu.Lock()
	t.Found = err == nil
	if err != nil {
		t.mu.Unlock()
		return
	}
	if st.Size() < t.offset {
		t.offset, t.rest, t.Acc = 0, nil, NewAccumulator()
	}
	offset := t.offset
	t.mu.Unlock()
	if st.Size() == offset {
		return
	}
	f, err := os.Open(t.Path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return
	}
	buf := make([]byte, 1<<20)
	for offset < st.Size() {
		n, err := f.Read(buf)
		if n > 0 {
			offset += int64(n)
			t.mu.Lock()
			data := append(t.rest, buf[:n]...)
			for {
				i := bytes.IndexByte(data, '\n')
				if i < 0 {
					break
				}
				t.Acc.Line(data[:i])
				data = data[i+1:]
			}
			t.rest = append([]byte(nil), data...)
			t.offset = offset
			t.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
}
