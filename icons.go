package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Favicons for the Quick links widget. Haven's server fetches them, so the
// browser never contacts the linked sites (nothing about which sites you
// link leaks to anyone but the sites themselves). Results are cached.

type iconEntry struct {
	data  []byte
	ctype string
	at    time.Time
	miss  bool
}

type iconCache struct {
	mu sync.Mutex
	m  map[string]*iconEntry
}

const (
	maxIconBytes = 200 << 10
	iconTTL      = 24 * time.Hour
	iconMissTTL  = time.Hour
)

var (
	linkTagRe = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	relRe     = regexp.MustCompile(`(?is)\brel\s*=\s*["']?([^"'>]*)["']?`)
	hrefRe    = regexp.MustCompile(`(?is)\bhref\s*=\s*["']([^"']+)["']`)
)

// imageType accepts only formats that are safe to show in an <img>.
func imageType(b []byte) string {
	switch ct := http.DetectContentType(b); {
	case ct == "image/png", ct == "image/jpeg", ct == "image/gif", ct == "image/webp", ct == "image/x-icon", ct == "image/vnd.microsoft.icon":
		return ct
	}
	return ""
}

func (w *Widgets) fetchBytes(ctx context.Context, c *http.Client, u string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("status " + strconv.Itoa(resp.StatusCode))
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// iconFrom downloads one icon address. Addresses on the linked site itself use
// the normal client (which can reach the home network, as the link does).
// Anything on another host must be a public address.
func (w *Widgets) iconFrom(ctx context.Context, origin *url.URL, addr string) ([]byte, string, bool) {
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, "", false
	}
	client := w.public
	if u.Host == origin.Host {
		client = w.client
	}
	b, err := w.fetchBytes(ctx, client, u.String(), maxIconBytes)
	if err != nil || len(b) == 0 {
		return nil, "", false
	}
	ct := imageType(b)
	return b, ct, ct != ""
}

// Favicon returns the icon of a linked site: /favicon.ico first, then the icon the page declares.
func (w *Widgets) Favicon(ctx context.Context, link string) ([]byte, string, error) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, "", errors.New("not a web address")
	}
	origin := &url.URL{Scheme: u.Scheme, Host: u.Host}
	key := origin.String()

	w.iconMu.Lock()
	if e := w.icons[key]; e != nil {
		ttl := iconTTL
		if e.miss {
			ttl = iconMissTTL
		}
		if time.Since(e.at) < ttl {
			w.iconMu.Unlock()
			if e.miss {
				return nil, "", errors.New("no icon")
			}
			return e.data, e.ctype, nil
		}
	}
	w.iconMu.Unlock()

	store := func(e *iconEntry) {
		w.iconMu.Lock()
		if w.icons == nil {
			w.icons = map[string]*iconEntry{}
		}
		if len(w.icons) > 200 { // bounded: forget everything rather than grow forever
			w.icons = map[string]*iconEntry{}
		}
		e.at = time.Now()
		w.icons[key] = e
		w.iconMu.Unlock()
	}

	if b, ct, ok := w.iconFrom(ctx, origin, key+"/favicon.ico"); ok {
		store(&iconEntry{data: b, ctype: ct})
		return b, ct, nil
	}
	// The page may name its own icon.
	if page, err := w.fetchBytes(ctx, w.client, key+"/", 256<<10); err == nil {
		for _, tag := range linkTagRe.FindAllString(string(page), 20) {
			rel := relRe.FindStringSubmatch(tag)
			href := hrefRe.FindStringSubmatch(tag)
			if rel == nil || href == nil || !strings.Contains(strings.ToLower(rel[1]), "icon") {
				continue
			}
			ref, err := url.Parse(strings.TrimSpace(href[1]))
			if err != nil {
				continue
			}
			if b, ct, ok := w.iconFrom(ctx, origin, origin.ResolveReference(ref).String()); ok {
				store(&iconEntry{data: b, ctype: ct})
				return b, ct, nil
			}
		}
	}
	store(&iconEntry{miss: true})
	return nil, "", errors.New("no icon")
}

// widgetFavicon serves the icon of a configured quick link, by its position in
// the list, so it can only ever fetch addresses the admin saved.
func (s *Server) widgetFavicon(w http.ResponseWriter, r *http.Request) {
	i, err := strconv.Atoi(r.URL.Query().Get("i"))
	links := s.store.UI().Widgets.Links
	if err != nil || i < 0 || i >= len(links) {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	b, ct, err := s.widgets.Favicon(ctx, links[i].URL)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(b)
}
