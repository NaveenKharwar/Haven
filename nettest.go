package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

// Explaining why the server can't reach a website, and testing it step by
// step so the person can see where it breaks.

// reachReason turns a low-level network error into a short plain reason.
func reachReason(err error) string {
	var dns *net.DNSError
	var uaErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var certInvalid x509.CertificateInvalidError
	switch {
	case errors.As(err, &dns):
		if dns.IsNotFound {
			return "its name isn't found by this server's DNS"
		}
		return "this server's DNS didn't answer"
	case errors.As(err, &uaErr), errors.As(err, &hostErr), errors.As(err, &certInvalid):
		return "its security certificate isn't accepted (check the server's clock)"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "the connection was refused"
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		return "there is no route to it from this server"
	case errors.Is(err, context.DeadlineExceeded), os.IsTimeout(err):
		return "the connection timed out"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "the connection timed out"
	}
	if strings.Contains(err.Error(), "tls:") {
		return "the secure connection failed"
	}
	return "the connection failed"
}

type NetStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	MS     int64  `json:"ms,omitempty"`
}

type NetTest struct {
	Host    string    `json:"host"`
	Steps   []NetStep `json:"steps"`
	Verdict string    `json:"verdict"`
	Fix     string    `json:"fix,omitempty"`
	Works   bool      `json:"works"`
}

// Hosts the widgets use. The test only ever contacts these.
var widgetHosts = map[string]bool{
	"api.open-meteo.com":           true,
	"geocoding-api.open-meteo.com": true,
	"news.google.com":              true,
	"api.frankfurter.dev":          true,
	"query1.finance.yahoo.com":     true,
}

func testHost(ctx context.Context, host string) NetTest {
	t := NetTest{Host: host}
	add := func(name string, ok bool, detail string, since time.Time) {
		t.Steps = append(t.Steps, NetStep{Name: name, OK: ok, Detail: detail, MS: time.Since(since).Milliseconds()})
	}

	// 1. DNS
	start := time.Now()
	dctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	ips, err := net.DefaultResolver.LookupIPAddr(dctx, host)
	cancel()
	if err != nil || len(ips) == 0 {
		add("Find the address (DNS)", false, reachReason(orDefault(err)), start)
		t.Verdict = "This server can't look up " + host + "."
		t.Fix = "The server's DNS is not working for this name. Set a public DNS such as 1.1.1.1 or 8.8.8.8 in Server → Network, or in your router's DNS settings, and try again. Some ISPs block or filter certain sites; a different DNS often fixes that."
		return t
	}
	var v4, v6 []net.IP
	for _, a := range ips {
		if a.IP.To4() != nil {
			v4 = append(v4, a.IP)
		} else {
			v6 = append(v6, a.IP)
		}
	}
	add("Find the address (DNS)", true, fmt.Sprintf("%d IPv4, %d IPv6 address(es)", len(v4), len(v6)), start)

	// 2. Can we connect, over IPv4 and over IPv6 separately?
	connect := func(label string, ips []net.IP) bool {
		if len(ips) == 0 {
			return false
		}
		s := time.Now()
		cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		var d net.Dialer
		c, err := d.DialContext(cctx, "tcp", net.JoinHostPort(ips[0].String(), "443"))
		if err != nil {
			add("Connect over "+label, false, reachReason(err), s)
			return false
		}
		c.Close()
		add("Connect over "+label, true, "connected", s)
		return true
	}
	ok4 := connect("IPv4", v4)
	ok6 := connect("IPv6", v6)
	if !ok4 && !ok6 {
		t.Verdict = "This server found " + host + " but can't connect to it."
		t.Fix = "Something between the server and the internet is blocking or dropping the connection: the router, a firewall, or the ISP. Check that other websites work from the server, try restarting the router, and try a different DNS (Server → Network). If it only fails for this site, your ISP may be blocking it."
		return t
	}

	// 3. A real HTTPS request
	s := time.Now()
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, "https://"+host+"/", nil)
	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		add("Ask for the page (HTTPS)", false, reachReason(err), s)
		t.Verdict = "The connection opens, but the secure request fails."
		if strings.Contains(reachReason(err), "certificate") {
			t.Fix = "Check the server's date and time (`timedatectl`). A wrong clock makes every secure site fail. If the clock is right, a router or ISP may be intercepting secure traffic."
		} else {
			t.Fix = "This is usually a flaky connection or an ISP filter. Try again in a few minutes, and try a different DNS (Server → Network)."
		}
		return t
	}
	resp.Body.Close()
	add("Ask for the page (HTTPS)", true, fmt.Sprintf("the site answered (HTTP %d)", resp.StatusCode), s)

	t.Works = true
	switch {
	case !ok4 && ok6:
		t.Verdict = host + " works, but only over IPv6."
	case ok4 && len(v6) > 0 && !ok6:
		t.Verdict = host + " works over IPv4. IPv6 doesn't, which is common and harmless."
	default:
		t.Verdict = host + " is reachable from this server right now."
	}
	t.Fix = "If a widget still shows an error, it was probably a short interruption. Haven has been told to try again now."
	return t
}

func orDefault(err error) error {
	if err == nil {
		return errors.New("no address found")
	}
	return err
}

// Retry clears the "try again in 2 minutes" wait so widgets refetch now.
func (w *Widgets) Retry() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range w.cache {
		c.retryAt = time.Time{}
		if c.err != "" {
			c.at = time.Time{}
		}
	}
}

func (s *Server) widgetTest(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	if !widgetHosts[host] {
		jsonError(w, http.StatusBadRequest, "that isn't a site Haven's widgets use")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	res := testHost(ctx, host)
	if res.Works {
		s.widgets.Retry()
	}
	writeJSON(w, http.StatusOK, res)
}
