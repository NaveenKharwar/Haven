package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// News from publishers' own RSS/Atom feeds, with a short summary for each
// story. Feed addresses can be typed in by the user, so Haven only fetches
// public internet addresses: never the server itself or other devices on
// the home network.

func validFeedURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || len(raw) > 300 {
		return errors.New("the feed address must start with https://")
	}
	if u.User != nil {
		return errors.New("the feed address can't contain a login")
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false // carrier-grade NAT range, used by Tailscale
	}
	return true
}

// publicClient refuses to connect to non-public addresses. The check runs on
// the address actually being dialled, so redirects and DNS tricks can't get
// around it.
func publicClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 8 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil || !isPublicIP(net.ParseIP(host)) {
				return errors.New("that address isn't on the public internet")
			}
			return nil
		},
	}
	return &http.Client{
		Timeout:   12 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 8 * time.Second, MaxIdleConns: 4},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 || req.URL.Scheme != "https" {
				return errors.New("too many redirects, or a redirect away from https")
			}
			return nil
		},
	}
}

type feedXML struct {
	Title string `xml:"channel>title"`
	Items []struct {
		Title  string `xml:"title"`
		Link   string `xml:"link"`
		Desc   string `xml:"description"`
		Date   string `xml:"pubDate"`
		DCDate string `xml:"http://purl.org/dc/elements/1.1/ date"`
	} `xml:"channel>item"`
	// Atom
	ATitle  string `xml:"http://www.w3.org/2005/Atom title"`
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
		Summary   string `xml:"summary"`
		Content   string `xml:"content"`
		Updated   string `xml:"updated"`
		Published string `xml:"published"`
	} `xml:"http://www.w3.org/2005/Atom entry"`
}

var (
	tagRe   = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRe = regexp.MustCompile(`\s+`)
)

// plainText turns a feed's HTML description into a short plain summary.
func plainText(s string, max int) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > max {
		s = strings.TrimSpace(string(r[:max])) + "…"
	}
	return s
}

var dateLayouts = []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 02 Jan 2006 15:04:05 MST", "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05"}

func parseFeedDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseFeed reads RSS 2.0 or Atom and returns the feed title and stories.
func parseFeed(b []byte, limit int) (string, []NewsItem, error) {
	var f feedXML
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	dec.Strict = false
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	if err := dec.Decode(&f); err != nil {
		return "", nil, errors.New("that doesn't look like an RSS or Atom feed")
	}
	title := strings.TrimSpace(f.Title)
	if title == "" {
		title = strings.TrimSpace(f.ATitle)
	}
	items := []NewsItem{}
	add := func(t, link, desc string, when time.Time) {
		t, link = plainText(t, 200), strings.TrimSpace(link)
		if t == "" || !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://") {
			return
		}
		items = append(items, NewsItem{Title: t, Link: link, Summary: plainText(desc, 280), Published: when})
	}
	for _, it := range f.Items {
		d := it.Date
		if d == "" {
			d = it.DCDate
		}
		add(it.Title, it.Link, it.Desc, parseFeedDate(d))
	}
	for _, e := range f.Entries {
		link := ""
		for _, l := range e.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = l.Href
				break
			}
		}
		desc := e.Summary
		if desc == "" {
			desc = e.Content
		}
		d := e.Published
		if d == "" {
			d = e.Updated
		}
		add(e.Title, link, desc, parseFeedDate(d))
	}
	if len(items) == 0 {
		return title, nil, errors.New("the feed has no stories")
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return title, items, nil
}

func (w *Widgets) fetchFeed(ctx context.Context, rawURL string) ([]byte, error) {
	if err := validFeedURL(rawURL); err != nil {
		return nil, err
	}
	return w.getWith(ctx, w.public, rawURL)
}

func (w *Widgets) feed(ctx context.Context, src NewsSource) (any, error) {
	b, err := w.fetchFeed(ctx, src.URL)
	if err != nil {
		return nil, err
	}
	_, items, err := parseFeed(b, 40)
	if err != nil {
		return nil, err
	}
	return News{Scope: "feed", Label: src.Name, Items: items}, nil
}

type FeedCheck struct {
	Title string `json:"title"`
	Count int    `json:"count"`
}

// CheckFeed lets the UI confirm a pasted address is a working feed before saving.
func (w *Widgets) CheckFeed(ctx context.Context, rawURL string) (*FeedCheck, error) {
	b, err := w.fetchFeed(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("couldn't load it: %w", err)
	}
	title, items, err := parseFeed(b, 40)
	if err != nil {
		return nil, err
	}
	return &FeedCheck{Title: plainText(title, 40), Count: len(items)}, nil
}
