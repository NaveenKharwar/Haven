package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Remote access through Tailscale: a private network between your devices,
// so Haven and its apps open from anywhere without opening router ports.
//
// Haven installs Tailscale, starts the sign-in, shows the sign-in link (and
// a QR code) in the UI, and then shows the addresses to use away from home.

type RemoteStatus struct {
	Supported   bool       `json:"supported"`
	Reason      string     `json:"reason,omitempty"`
	Installed   bool       `json:"installed"`
	State       string     `json:"state"` // NotInstalled, NotRunning, NeedsLogin, Stopped, Starting, Running
	AuthURL     string     `json:"authUrl,omitempty"`
	HostName    string     `json:"hostName,omitempty"`
	DNSName     string     `json:"dnsName,omitempty"`
	IPs         []string   `json:"ips,omitempty"`
	Tailnet     string     `json:"tailnet,omitempty"`
	MagicDNS    bool       `json:"magicDns"`
	Peers       int        `json:"peers"`
	PeersOnline int        `json:"peersOnline"`
	KeyExpiry   *time.Time `json:"keyExpiry,omitempty"`
	Version     string     `json:"version,omitempty"`
	URLs        []string   `json:"urls,omitempty"`
	JobID       string     `json:"jobId,omitempty"`
}

type Remote struct {
	jobs   *Jobs
	port   string
	port80 *ExtraListener
	mu     sync.Mutex
	jobID  string
	auth   string // sign-in link seen in `tailscale up` output
}

type tsStatus struct {
	BackendState string `json:"BackendState"`
	AuthURL      string `json:"AuthURL"`
	Version      string `json:"Version"`
	Self         *struct {
		HostName     string     `json:"HostName"`
		DNSName      string     `json:"DNSName"`
		TailscaleIPs []string   `json:"TailscaleIPs"`
		KeyExpiry    *time.Time `json:"KeyExpiry"`
	} `json:"Self"`
	CurrentTailnet *struct {
		Name            string `json:"Name"`
		MagicDNSEnabled bool   `json:"MagicDNSEnabled"`
	} `json:"CurrentTailnet"`
	Peer map[string]struct {
		Online bool `json:"Online"`
	} `json:"Peer"`
}

func parseTailscaleStatus(b []byte) (*tsStatus, error) {
	var s tsStatus
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Remote) runningJob() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs.Get(r.jobID); ok && j.View(false).Status == "running" {
		return r.jobID
	}
	return ""
}

func (r *Remote) Status(ctx context.Context) RemoteStatus {
	st := RemoteStatus{State: "NotInstalled", JobID: r.runningJob()}
	if !isLinux() {
		st.Reason = "Remote access is set up on the Linux server."
		return st
	}
	st.Supported = true
	if !hasCmd("tailscale") {
		return st
	}
	st.Installed = true
	out, err := runHost(ctx, 8*time.Second, "tailscale", "status", "--json")
	ts, perr := parseTailscaleStatus([]byte(out))
	if perr != nil || ts.BackendState == "" {
		st.State = "NotRunning"
		if err == nil {
			st.State = "Starting"
		}
		return st
	}
	st.State = ts.BackendState
	st.Version = ts.Version
	st.AuthURL = ts.AuthURL
	if st.AuthURL == "" && st.State == "NeedsLogin" {
		r.mu.Lock()
		st.AuthURL = r.auth
		r.mu.Unlock()
	}
	if ts.Self != nil {
		st.HostName = ts.Self.HostName
		st.DNSName = strings.TrimSuffix(ts.Self.DNSName, ".")
		st.IPs = ts.Self.TailscaleIPs
		st.KeyExpiry = ts.Self.KeyExpiry
	}
	if ts.CurrentTailnet != nil {
		st.Tailnet = ts.CurrentTailnet.Name
		st.MagicDNS = ts.CurrentTailnet.MagicDNSEnabled
	}
	for _, p := range ts.Peer {
		st.Peers++
		if p.Online {
			st.PeersOnline++
		}
	}
	if st.State == "Running" {
		st.URLs = r.urls(st)
	}
	return st
}

func (r *Remote) urls(st RemoteStatus) []string {
	port := ":" + r.port
	if r.port80.On() {
		port = ""
	}
	var out []string
	if st.MagicDNS && st.DNSName != "" {
		short, _, _ := strings.Cut(st.DNSName, ".")
		out = append(out, "http://"+short+port, "http://"+st.DNSName+port)
	}
	for _, ip := range st.IPs {
		if !strings.Contains(ip, ":") { // IPv4 is easier to type
			out = append(out, "http://"+ip+port)
		}
	}
	return out
}

// Setup installs Tailscale if needed and signs this server in.
func (r *Remote) Setup() (*Job, error) {
	if !isLinux() {
		return nil, errors.New("remote access is set up on the Linux server")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs.Get(r.jobID); ok && j.View(false).Status == "running" {
		return j, nil
	}
	r.auth = ""
	job := r.jobs.Start("server", "remote-access", 20*time.Minute, func(ctx context.Context, j *Job) error {
		if !hasCmd("tailscale") {
			j.Logf("Installing Tailscale with its official installer (tailscale.com/install.sh)")
			if !hasCmd("curl") {
				if err := aptInstall(ctx, j, "curl", "ca-certificates"); err != nil {
					return err
				}
			}
			if err := hostStream(ctx, j, nil, "sh", "-c", "curl -fsSL https://tailscale.com/install.sh | sh"); err != nil {
				return errors.New("could not install Tailscale; check the server's internet connection")
			}
		}
		j.Logf("Starting the Tailscale service")
		if err := hostStream(ctx, j, nil, "systemctl", "enable", "--now", "tailscaled"); err != nil {
			return err
		}
		time.Sleep(2 * time.Second)
		return r.up(ctx, j)
	})
	r.jobID = job.ID
	return job, nil
}

var authURLRe = regexp.MustCompile(`https://login\.tailscale\.com/\S+`)

// up runs `tailscale up`. If this server needs signing in, Tailscale prints
// a link; Haven captures it for the UI, then waits until sign-in finishes.
func (r *Remote) up(ctx context.Context, j *Job) error {
	if out, _ := runHost(ctx, 8*time.Second, "tailscale", "status", "--json"); out != "" {
		if ts, err := parseTailscaleStatus([]byte(out)); err == nil && ts.BackendState == "Running" {
			j.Logf("Already connected")
			return nil
		}
	}
	j.Logf("Connecting. If a sign-in link appears, open it and sign in to your Tailscale account")
	output, err := r.runUp(ctx, j, []string{"up"})
	if err != nil && strings.Contains(output, "requires mentioning all non-default flags") {
		// This server was set up with Tailscale before, with custom options.
		// Tailscale prints the exact command that keeps them; use it.
		if args := keepSettingsArgs(output); args != nil {
			j.Logf("Keeping your existing Tailscale options")
			output, err = r.runUp(ctx, j, args)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("sign-in wasn't finished in time; press Set up again for a new link")
		}
		return fmt.Errorf("tailscale up failed: %s", lastLines(output, 3))
	}
	r.mu.Lock()
	r.auth = ""
	r.mu.Unlock()
	if out, _ := runHost(ctx, 8*time.Second, "tailscale", "status", "--json"); out != "" {
		if ts, err := parseTailscaleStatus([]byte(out)); err == nil && ts.Self != nil {
			j.Logf("Connected as %s (%s)", strings.TrimSuffix(ts.Self.DNSName, "."), strings.Join(ts.Self.TailscaleIPs, ", "))
		}
	}
	return nil
}

func (r *Remote) runUp(ctx context.Context, j *Job, args []string) (string, error) {
	pr, pw := io.Pipe()
	var buf strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			line := sc.Text()
			buf.WriteString(line + "\n")
			_, _ = j.Write([]byte(line + "\n"))
			if m := authURLRe.FindString(line); m != "" {
				r.mu.Lock()
				r.auth = m
				r.mu.Unlock()
				j.Logf("Sign in here: %s", m)
			}
		}
	}()
	err := hostStream(ctx, pw, nil, "tailscale", args...)
	pw.Close()
	<-done
	return buf.String(), err
}

// keepSettingsArgs pulls the suggested "tailscale up --flag ..." command out
// of Tailscale's error message. Only --flags are accepted.
func keepSettingsArgs(output string) []string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "tailscale up") {
			continue
		}
		f := strings.Fields(line)[1:] // drop "tailscale"
		for _, a := range f[1:] {
			if !strings.HasPrefix(a, "--") {
				return nil
			}
		}
		return f
	}
	return nil
}

// Action turns remote access on or off, or signs this server out.
func (r *Remote) Action(ctx context.Context, action string) (*Job, error) {
	if !isLinux() || !hasCmd("tailscale") {
		return nil, errors.New("Tailscale isn't installed on this server")
	}
	switch action {
	case "up":
		return r.Setup()
	case "down", "logout":
		if _, err := runHost(ctx, 30*time.Second, "tailscale", action); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return nil, errors.New("unknown action")
}
