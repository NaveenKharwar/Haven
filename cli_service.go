package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

var serviceAddrPattern = regexp.MustCompile(`(?:^|\s)--addr\s+[^\s]*:([0-9]+)(?:\s|$)`)

func runHavenServiceAction(action string, out io.Writer, run systemctlRunner, root bool) error {
	if !root {
		return fmt.Errorf("run `sudo haven %s` so Haven can manage its systemd service", action)
	}
	if _, err := run(action, "haven.service"); err != nil {
		return err
	}
	result := map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}[action]
	fmt.Fprintf(out, "Haven service %s.\n", result)
	return nil
}

func printHavenServiceStatus(out io.Writer, run systemctlRunner) error {
	style := cliStyle{color: cliColor(out)}
	property := func(name string) (string, error) {
		return run("show", "haven.service", "--property="+name, "--value")
	}
	active, err := property("ActiveState")
	if err != nil {
		return err
	}
	sub, _ := property("SubState")
	enabled, _ := property("UnitFileState")
	pid, _ := property("MainPID")
	execStart, _ := property("ExecStart")
	hosting, hostingErr := hostingExperimentStatus(run)

	fmt.Fprintln(out, style.accent(style.bold("HAVEN "+Version)))
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Service   %s", active)
	if sub != "" && sub != active {
		fmt.Fprintf(out, " (%s)", sub)
	}
	fmt.Fprintln(out)
	if enabled != "" {
		fmt.Fprintf(out, "  Startup   %s\n", enabled)
	}
	if pid != "" && pid != "0" {
		fmt.Fprintf(out, "  Process   %s\n", pid)
	}
	if match := serviceAddrPattern.FindStringSubmatch(execStart); len(match) == 2 {
		fmt.Fprintf(out, "  Port      %s\n", match[1])
	}
	if hostingErr == nil {
		state := "disabled"
		if hosting {
			state = "enabled"
		}
		fmt.Fprintf(out, "  Hosting   %s (experimental)\n", state)
	}
	return nil
}

func streamHavenServiceLogs(out, errOut io.Writer, lines int, follow bool) error {
	if lines < 1 {
		return errors.New("--lines must be at least 1")
	}
	args := []string{"-u", "haven.service", "-n", strconv.Itoa(lines), "--no-pager"}
	if follow {
		args = append(args, "--follow")
	}
	ctx := context.Background()
	var cancel context.CancelFunc
	if !follow {
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	cmd.Stdout = out
	cmd.Stderr = errOut
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("read Haven service logs: %w", err)
	}
	return nil
}
