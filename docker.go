package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Docker talks to Docker through the official `docker` CLI. Using the CLI
// (instead of re-implementing compose) means apps behave exactly as they
// would if you ran `docker compose` yourself.
type Docker struct{}

type Container struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Image   string  `json:"image"`
	State   string  `json:"state"` // running, exited, restarting, created, paused, dead
	Status  string  `json:"status"`
	Health  string  `json:"health"` // healthy, unhealthy, starting, or ""
	Ports   string  `json:"ports"`
	Project string  `json:"project"`
	Service string  `json:"service"`
	CPU     float64 `json:"cpu,omitempty"` // percent of the whole server
	Mem     uint64  `json:"mem,omitempty"` // bytes
}

func (d *Docker) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(out.String())
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return out.Bytes(), fmt.Errorf("%s", msg)
	}
	return out.Bytes(), nil
}

// stream runs docker and sends its output, line by line, to w.
func (d *Docker) stream(ctx context.Context, w io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

func (d *Docker) Version(ctx context.Context) (string, error) {
	out, err := d.run(ctx, "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *Docker) ComposeVersion(ctx context.Context) (string, error) {
	out, err := d.run(ctx, "compose", "version", "--short")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

type psLine struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
	Ports  string `json:"Ports"`
	Labels string `json:"Labels"`
}

func (d *Docker) ListContainers(ctx context.Context) ([]Container, error) {
	out, err := d.run(ctx, "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var list []Container
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var p psLine
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			continue
		}
		c := Container{ID: p.ID, Name: p.Names, Image: p.Image, State: strings.ToLower(p.State), Status: p.Status, Ports: p.Ports}
		if len(c.ID) > 12 {
			c.ID = c.ID[:12]
		}
		labels := parseLabels(p.Labels)
		c.Project = labels["com.docker.compose.project"]
		c.Service = labels["com.docker.compose.service"]
		st := strings.ToLower(p.Status)
		switch {
		case strings.Contains(st, "(unhealthy)"):
			c.Health = "unhealthy"
		case strings.Contains(st, "(health: starting)"):
			c.Health = "starting"
		case strings.Contains(st, "(healthy)"):
			c.Health = "healthy"
		}
		list = append(list, c)
	}
	return list, nil
}

func parseLabels(s string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			m[strings.TrimSpace(k)] = v
		}
	}
	return m
}

// ImageIDs returns, for each container in a compose project, the image
// reference it was started from and the exact image ID. Used for rollback.
func (d *Docker) ImageIDs(ctx context.Context, project string) (map[string]string, error) {
	out, err := d.run(ctx, "ps", "-a", "--filter", "label=com.docker.compose.project="+project, "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(out))
	res := map[string]string{}
	if len(ids) == 0 {
		return res, nil
	}
	args := append([]string{"inspect", "--format", "{{.Config.Image}} {{.Image}}"}, ids...)
	out, err = d.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		ref, id, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok {
			res[ref] = id
		}
	}
	return res, nil
}
