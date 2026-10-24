package main

import (
	"strings"
)

// Help explains a failed job in plain words: what went wrong, how to fix it,
// and, when there is one, a command to run on the server.
type Help struct {
	Title   string `json:"title"`
	Why     string `json:"why"`
	Fix     string `json:"fix"`
	Command string `json:"command,omitempty"`
}

type failRule struct {
	any  []string // any of these in the lowercased log
	help Help
}

// failRules are checked in order; the first match wins. They read what the
// tools printed, because that is where the real reason is.
var failRules = []failRule{
	{[]string{"toomanyrequests", "429 too many", "rate limit", "pull rate"}, Help{
		"Docker Hub is limiting downloads",
		"Docker Hub only lets a server that is not signed in download a limited number of images every few hours, and this server used them up.",
		"Sign this server in to a free Docker Hub account (hub.docker.com) to raise the limit, then try again. Or just wait a few hours.",
		"sudo docker login"}},
	{[]string{"no space left on device"}, Help{
		"The disk is full",
		"There is no room left to store the downloaded images or app data.",
		"Free some space: remove apps you don't use, then delete unused images. Check Server > Overview for what is using the disk.",
		"sudo docker system prune -a"}},
	{[]string{"address already in use", "port is already allocated", "bind for 0.0.0.0"}, Help{
		"The port is already used",
		"Another program on this server is already using the port this app wants.",
		"Open the app's settings and choose a different web port, then install again.",
		"sudo ss -ltnp"}},
	{[]string{"permission denied"}, Help{
		"Permission denied",
		"A folder or file the app needs can't be written by it, or Haven isn't allowed to use Docker.",
		"Check the folders you chose for this app exist and are writable. If Docker itself is refusing, make sure the Haven service runs as root.",
		"sudo systemctl status haven"}},
	{[]string{"pull access denied", "unauthorized", "requested access to the resource is denied", "manifest unknown", "not found: manifest"}, Help{
		"The image can't be downloaded",
		"The registry refused the download, or that version of the image doesn't exist (any more).",
		"Try again later. If it keeps failing, the app's image may have moved; check the app's website.",
		""}},
	{[]string{"no matching manifest", "exec format error"}, Help{
		"Not built for this CPU",
		"This app has no version for this server's processor type.",
		"Pick a different app, or a version that supports this processor.",
		"uname -m"}},
	{[]string{"no such host", "temporary failure in name resolution", "dial tcp", "i/o timeout", "tls handshake timeout", "network is unreachable", "lookup "}, Help{
		"The server can't reach the internet",
		"Downloads failed because the server could not connect to the registry (internet or DNS problem).",
		"Check the cable or Wi-Fi, and DNS. Then try again.",
		"ping -c 3 1.1.1.1 && nslookup registry-1.docker.io"}},
	{[]string{"cannot connect to the docker daemon", "is the docker daemon running"}, Help{
		"Docker isn't running",
		"Haven couldn't talk to Docker.",
		"Start Docker, then try again.",
		"sudo systemctl start docker"}},
	{[]string{"unhealthy", "did not become healthy", "exited with code", "restarting"}, Help{
		"The app started but keeps crashing",
		"The app was downloaded and started, then stopped or failed its health check.",
		"Open the app and use Diagnose to see its own log and the likely cause. Wrong settings (a password, a folder) are the usual reason.",
		""}},
	{[]string{"context deadline exceeded", "timed out"}, Help{
		"It took too long",
		"The step did not finish in time, usually because of a slow internet connection or a busy server.",
		"Try again; already-downloaded parts are kept, so the second try is faster.",
		""}},
}

// explainFailure returns help for a failed job from its log, or nil.
func explainFailure(logLines []string, errText string) *Help {
	text := strings.ToLower(strings.Join(logLines, "\n") + "\n" + errText)
	for _, r := range failRules {
		for _, k := range r.any {
			if strings.Contains(text, k) {
				h := r.help
				return &h
			}
		}
	}
	return &Help{
		Title: "Something went wrong",
		Why:   "Haven doesn't recognise this error.",
		Fix:   "Read the last few lines of the log below, use Copy log, and search the exact error text online. Server > Health check can also spot common problems.",
	}
}
