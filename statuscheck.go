package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Status checks: green or red lights for things the admin chooses (the router,
// a website, an app's port). Haven checks them every 30 seconds in the
// background, so opening the Overview never waits on a slow target.

type StatusCheck struct {
	Name   string `json:"name"`
	Target string `json:"target"` // http(s)://host[:port]/path, or host:port for a TCP check
}

type CheckResult struct {
	Name    string    `json:"name"`
	Target  string    `json:"target"`
	Up      bool      `json:"up"`
	Pending bool      `json:"pending,omitempty"` // not checked yet
	MS      int64     `json:"ms,omitempty"`
	Error   string    `json:"error,omitempty"`
	Since   time.Time `json:"since,omitempty"` // when it last changed between up and down
}

const (
	maxChecks     = 12
	checkInterval = 30 * time.Second
)

var hostPortRe = regexp.MustCompile(`^[A-Za-z0-9.\-]{1,253}:\d{1,5}$`)

// validCheckTarget accepts an http(s) address or host:port, and nothing else.
func validCheckTarget(t string) error {
	t = strings.TrimSpace(t)
	if len(t) == 0 || len(t) > 300 {
		return errors.New("enter an address to check")
	}
	if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
		u, err := url.Parse(t)
		if err != nil || u.Host == "" || u.User != nil {
			return errors.New("that web address doesn't look right")
		}
		return nil
	}
	if !hostPortRe.MatchString(t) {
		return errors.New("use a web address (https://…) or host:port, for example 192.168.1.1:80")
	}
	port, _ := strconv.Atoi(t[strings.LastIndex(t, ":")+1:])
	if port < 1 || port > 65535 {
		return errors.New("the port must be between 1 and 65535")
	}
	return nil
}

// probe returns whether the target answers, how long it took, and why not.
func probe(ctx context.Context, target string) (bool, int64, string) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		// Home devices often use self-signed certificates. This only asks
		// "does it answer?", and never sends or reads anything private.
		client := &http.Client{
			Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return false, 0, "bad address"
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := client.Do(req)
		if err != nil {
			return false, 0, reachReason(err)
		}
		resp.Body.Close()
		ms := time.Since(start).Milliseconds()
		if resp.StatusCode >= 500 {
			return false, ms, fmt.Sprintf("it answered with an error (%d)", resp.StatusCode)
		}
		return true, ms, ""
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return false, 0, reachReason(err)
	}
	c.Close()
	return true, time.Since(start).Milliseconds(), ""
}

type StatusChecker struct {
	store  *Store
	notify func(level, title, body, key, route string)
	kick   chan struct{}

	mu       sync.Mutex
	res      map[string]*CheckResult
	failures map[string]int
	told     map[string]bool // down notification sent, so recovery is announced once
}

func NewStatusChecker(store *Store, notify func(level, title, body, key, route string)) *StatusChecker {
	return &StatusChecker{store: store, notify: notify, kick: make(chan struct{}, 1),
		res: map[string]*CheckResult{}, failures: map[string]int{}, told: map[string]bool{}}
}

// Kick runs the checks now (for example right after the list is edited).
func (c *StatusChecker) Kick() {
	if c == nil {
		return
	}
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

func (c *StatusChecker) Run(ctx context.Context) {
	t := time.NewTicker(checkInterval)
	defer t.Stop()
	for {
		c.once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.kick:
		}
	}
}

func (c *StatusChecker) once(ctx context.Context) {
	list := c.store.UI().Widgets.Checks
	type out struct {
		check StatusCheck
		up    bool
		ms    int64
		err   string
	}
	results := make([]out, len(list))
	var wg sync.WaitGroup
	for i, ch := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			up, ms, e := probe(ctx, ch.Target)
			results[i] = out{ch, up, ms, e}
		}()
	}
	wg.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()
	keep := map[string]bool{}
	for _, r := range results {
		key := r.check.Target
		keep[key] = true
		prev := c.res[key]
		cur := &CheckResult{Name: r.check.Name, Target: key, Up: r.up, MS: r.ms, Error: r.err}
		if prev != nil && !prev.Pending && prev.Up == r.up {
			cur.Since = prev.Since
		} else {
			cur.Since = time.Now()
		}
		c.res[key] = cur
		if r.up {
			c.failures[key] = 0
			if c.told[key] && c.notify != nil {
				c.notify("ok", r.check.Name+" is back", "It is answering again.", fmt.Sprintf("checkup:%s:%d", key, time.Now().Unix()/60), "")
			}
			delete(c.told, key)
			continue
		}
		c.failures[key]++
		// Two misses in a row (about a minute), so one dropped packet isn't reported.
		if c.failures[key] >= 2 && !c.told[key] && c.notify != nil {
			c.told[key] = true
			c.notify("bad", r.check.Name+" is down", r.err+". Address: "+key, fmt.Sprintf("checkdown:%s:%d", key, time.Now().Unix()/3600), "")
		}
	}
	for k := range c.res {
		if !keep[k] {
			delete(c.res, k)
			delete(c.failures, k)
			delete(c.told, k)
		}
	}
}

// Results returns the latest result for each configured check, in order.
func (c *StatusChecker) Results(list []StatusCheck) []CheckResult {
	if c == nil {
		return []CheckResult{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]CheckResult, 0, len(list))
	for _, ch := range list {
		if r, ok := c.res[ch.Target]; ok {
			cp := *r
			cp.Name = ch.Name
			out = append(out, cp)
		} else {
			out = append(out, CheckResult{Name: ch.Name, Target: ch.Target, Pending: true})
		}
	}
	return out
}
