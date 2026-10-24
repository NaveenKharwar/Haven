package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// A Job is a long-running task (install, update, ...) whose progress the UI
// can follow. Running these in the background means a slow image download
// never times out a web request.
type Job struct {
	mu      sync.Mutex
	ID      string
	App     string
	Action  string
	Status  string // running, success, failed
	Error   string
	Started time.Time
	Ended   time.Time
	lines   []string
	partial []byte
}

type JobView struct {
	ID      string    `json:"id"`
	App     string    `json:"app"`
	Action  string    `json:"action"`
	Status  string    `json:"status"`
	Error   string    `json:"error,omitempty"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitempty"`
	Log     []string  `json:"log,omitempty"`
	Help    *Help     `json:"help,omitempty"`
}

const maxJobLines = 3000

// Write lets a Job be used as the output of a command.
func (j *Job) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	data := append(j.partial, p...)
	// Docker progress output uses \r to redraw a line; treat it as a newline.
	data = bytes.ReplaceAll(data, []byte("\r"), []byte("\n"))
	parts := bytes.Split(data, []byte("\n"))
	for _, part := range parts[:len(parts)-1] {
		if s := strings.TrimRight(string(part), " \t"); s != "" {
			j.addLine(s)
		}
	}
	j.partial = append([]byte(nil), parts[len(parts)-1]...)
	return len(p), nil
}

func (j *Job) addLine(s string) {
	j.lines = append(j.lines, s)
	if len(j.lines) > maxJobLines {
		j.lines = j.lines[len(j.lines)-maxJobLines:]
	}
}

func (j *Job) Logf(format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.addLine("» " + fmt.Sprintf(format, args...))
}

func (j *Job) View(withLog bool) JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := JobView{ID: j.ID, App: j.App, Action: j.Action, Status: j.Status, Error: j.Error, Started: j.Started, Ended: j.Ended}
	if withLog {
		v.Log = append([]string(nil), j.lines...)
		if len(j.partial) > 0 {
			v.Log = append(v.Log, string(j.partial))
		}
		if j.Status == "failed" {
			v.Help = explainFailure(v.Log, j.Error)
		}
	}
	return v
}

type Jobs struct {
	mu     sync.Mutex
	m      map[string]*Job
	OnDone func(*Job) // called after a job finishes, either way
	path   string     // finished jobs are kept here so a restart doesn't lose the story
}

func NewJobs() *Jobs { return &Jobs{m: map[string]*Job{}} }

type savedJob struct {
	ID      string    `json:"id"`
	App     string    `json:"app"`
	Action  string    `json:"action"`
	Status  string    `json:"status"`
	Error   string    `json:"error"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended"`
	Lines   []string  `json:"lines"`
}

const keepSavedJobs = 30
const keepSavedLines = 400

// NewJobsAt is NewJobs that remembers finished jobs in a file.
func NewJobsAt(path string) *Jobs {
	js := NewJobs()
	js.path = path
	if b, err := os.ReadFile(path); err == nil {
		var saved []savedJob
		if json.Unmarshal(b, &saved) == nil {
			for _, x := range saved {
				js.m[x.ID] = &Job{ID: x.ID, App: x.App, Action: x.Action, Status: x.Status, Error: x.Error, Started: x.Started, Ended: x.Ended, lines: x.Lines}
			}
		}
	}
	return js
}

func (js *Jobs) save() {
	if js.path == "" {
		return
	}
	js.mu.Lock()
	var done []*Job
	for _, j := range js.m {
		if j.View(false).Status != "running" {
			done = append(done, j)
		}
	}
	js.mu.Unlock()
	sort.Slice(done, func(a, b int) bool { return done[a].Started.After(done[b].Started) })
	if len(done) > keepSavedJobs {
		done = done[:keepSavedJobs]
	}
	out := make([]savedJob, 0, len(done))
	for _, j := range done {
		j.mu.Lock()
		lines := j.lines
		if len(lines) > keepSavedLines {
			lines = lines[len(lines)-keepSavedLines:]
		}
		out = append(out, savedJob{j.ID, j.App, j.Action, j.Status, j.Error, j.Started, j.Ended, append([]string(nil), lines...)})
		j.mu.Unlock()
	}
	b, _ := json.Marshal(out)
	_ = writeFileAtomic(js.path, b, 0o600)
}

func (js *Jobs) Start(app, action string, timeout time.Duration, fn func(ctx context.Context, j *Job) error) *Job {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	j := &Job{ID: hex.EncodeToString(b), App: app, Action: action, Status: "running", Started: time.Now()}
	js.mu.Lock()
	js.m[j.ID] = j
	// Keep the most recent 50 jobs.
	if len(js.m) > 50 {
		all := make([]*Job, 0, len(js.m))
		for _, x := range js.m {
			all = append(all, x)
		}
		sort.Slice(all, func(a, b int) bool { return all[a].Started.Before(all[b].Started) })
		for _, x := range all[:len(all)-50] {
			if x.View(false).Status != "running" {
				delete(js.m, x.ID)
			}
		}
	}
	js.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("internal error: %v", r)
					log.Printf("job %s panicked: %v", j.ID, r)
				}
			}()
			err = fn(ctx, j)
		}()
		j.mu.Lock()
		j.Ended = time.Now()
		if err != nil {
			j.Status = "failed"
			j.Error = err.Error()
			j.addLine("✗ " + err.Error())
		} else {
			j.Status = "success"
			j.addLine("✓ Done")
		}
		j.mu.Unlock()
		js.save()
		if js.OnDone != nil {
			js.OnDone(j)
		}
	}()
	return j
}

func (js *Jobs) Get(id string) (*Job, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()
	j, ok := js.m[id]
	return j, ok
}

func (js *Jobs) List() []JobView {
	js.mu.Lock()
	defer js.mu.Unlock()
	out := make([]JobView, 0, len(js.m))
	for _, j := range js.m {
		out = append(out, j.View(false))
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Started.After(out[b].Started) })
	return out
}
