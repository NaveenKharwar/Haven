package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Helpers for the features that change the server itself (lid, local name,
// remote access). Haven runs as root on the server, so these can call
// systemctl, apt-get and friends directly.

func isLinux() bool { return runtime.GOOS == "linux" }

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// runHost runs a command and returns its trimmed combined output.
func runHost(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	s := strings.TrimSpace(out.String())
	if err != nil {
		if s == "" {
			s = err.Error()
		}
		return s, fmt.Errorf("%s: %s", name, lastLines(s, 4))
	}
	return s, nil
}

// hostStream runs a command and streams its output (to a job's log).
func hostStream(ctx context.Context, w io.Writer, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.Run()
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}

// aptInstall installs packages without prompts, waiting for apt's lock if
// automatic updates are running.
func aptInstall(ctx context.Context, w io.Writer, pkgs ...string) error {
	env := []string{"DEBIAN_FRONTEND=noninteractive"}
	if err := hostStream(ctx, w, env, "apt-get", "-o", "DPkg::Lock::Timeout=180", "update", "-q"); err != nil {
		return fmt.Errorf("apt-get update failed: %w", err)
	}
	args := append([]string{"-o", "DPkg::Lock::Timeout=180", "install", "-y", "-q"}, pkgs...)
	if err := hostStream(ctx, w, env, "apt-get", args...); err != nil {
		return fmt.Errorf("installing %s failed: %w", strings.Join(pkgs, ", "), err)
	}
	return nil
}

// ufwActive reports whether Ubuntu's firewall is switched on.
func ufwActive(ctx context.Context) bool {
	if !hasCmd("ufw") {
		return false
	}
	out, _ := runHost(ctx, 5*time.Second, "ufw", "status")
	return strings.Contains(out, "Status: active")
}

// lanAddresses lists the server's IPv4 addresses on real network
// interfaces (not Docker, VPN or loopback).
func lanAddresses() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		n := ifc.Name
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 ||
			strings.HasPrefix(n, "docker") || strings.HasPrefix(n, "br-") || strings.HasPrefix(n, "veth") ||
			strings.HasPrefix(n, "tailscale") || strings.HasPrefix(n, "utun") || strings.HasPrefix(n, "virbr") ||
			strings.HasPrefix(n, "bridge") || strings.HasPrefix(n, "awdl") || strings.HasPrefix(n, "llw") {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLinkLocalUnicast() {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}
