package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	hostingServiceDropIn = "/etc/systemd/system/haven.service.d/90-haven-hosting-experimental.conf"
	hostingServiceConfig = "# Managed by `haven hosting`; use the CLI to change this file.\n[Service]\nEnvironment=HAVEN_HOSTING_EXPERIMENTAL=1\n"
)

type systemctlRunner func(args ...string) (string, error)

func runHostingCLI(args []string, out io.Writer) error {
	if len(args) != 1 || (args[0] != "enable" && args[0] != "disable" && args[0] != "status") {
		return errors.New("usage: haven hosting <enable|disable|status>")
	}
	run := systemctlCommandRunner()
	if args[0] == "status" {
		enabled, err := hostingExperimentStatus(run)
		if err != nil {
			return err
		}
		if enabled {
			fmt.Fprintln(out, "Experimental Hosting is enabled.")
		} else {
			fmt.Fprintln(out, "Experimental Hosting is disabled.")
		}
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("run this command with sudo so Haven can update its systemd service")
	}
	enable := args[0] == "enable"
	if err := configureHostingExperiment(hostingServiceDropIn, enable, run); err != nil {
		return err
	}
	printHostingExperimentResult(out, enable)
	return nil
}

func systemctlCommandRunner() systemctlRunner {
	return func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
		if err != nil {
			detail := strings.TrimSpace(string(result))
			if detail != "" {
				return "", fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), detail)
			}
			return "", fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
		}
		return strings.TrimSpace(string(result)), nil
	}
}

func printHostingExperimentResult(out io.Writer, enable bool) {
	if enable {
		fmt.Fprintln(out, "Experimental Hosting is enabled. Reconnect to Haven and open Hosting.")
	} else {
		fmt.Fprintln(out, "Experimental Hosting is disabled. Existing website containers were not removed.")
	}
}

func runExperimentalCLI(args []string, in io.Reader, out io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: haven experimental")
	}
	return runExperimentalMenu(in, out, hostingServiceDropIn, systemctlCommandRunner(), os.Geteuid() == 0)
}

func runExperimentalMenu(in io.Reader, out io.Writer, path string, run systemctlRunner, root bool) error {
	enabled, err := hostingExperimentStatus(run)
	if err != nil {
		return err
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	style := cliStyle{color: cliColor(out)}
	fmt.Fprintln(out, style.accent(style.bold("HAVEN EXPERIMENTAL")))
	fmt.Fprintln(out, "Opt-in features may change before general release.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, style.bold("FEATURES"))
	fmt.Fprintf(out, "  1  Website and App Hosting [%s]\n", state)
	fmt.Fprintln(out)
	fmt.Fprint(out, "Select a feature to manage (1, or q to quit): ")
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return scanner.Err()
	}
	choice := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if choice == "" || choice == "q" || choice == "quit" {
		return nil
	}
	if choice != "1" {
		return errors.New("choose 1 for Website and App Hosting, or q to quit")
	}
	action := "Enable"
	if enabled {
		action = "Disable"
	}
	fmt.Fprintf(out, "\nWebsite and App Hosting is %s. %s it? [y/N]: ", state, action)
	if !scanner.Scan() {
		return scanner.Err()
	}
	confirm := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if confirm != "y" && confirm != "yes" {
		fmt.Fprintln(out, "No changes made.")
		return nil
	}
	if !root {
		return errors.New("rerun `sudo haven experimental` to change experimental features")
	}
	if err := configureHostingExperiment(path, !enabled, run); err != nil {
		return err
	}
	printHostingExperimentResult(out, !enabled)
	return nil
}

func hostingExperimentStatus(run systemctlRunner) (bool, error) {
	environment, err := run("show", "haven.service", "--property=Environment", "--value")
	if err != nil {
		return false, err
	}
	for _, value := range strings.Fields(environment) {
		if strings.Trim(value, "\"'") == "HAVEN_HOSTING_EXPERIMENTAL=1" {
			return true, nil
		}
	}
	return false, nil
}

func configureHostingExperiment(path string, enable bool, run systemctlRunner) error {
	previous, readErr := os.ReadFile(path)
	existed := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, readErr)
	}
	if existed && string(previous) != hostingServiceConfig {
		return fmt.Errorf("%s exists but is not managed by Haven; move it aside or update it manually", path)
	}

	restore := func() error {
		if existed {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := writeFileAtomic(path, previous, 0o644); err != nil {
				return err
			}
		} else {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			_ = os.Remove(filepath.Dir(path))
		}
		if _, err := run("daemon-reload"); err != nil {
			return fmt.Errorf("reload systemd after rollback: %w", err)
		}
		return nil
	}
	rollback := func(restart bool) error {
		if err := restore(); err != nil {
			return fmt.Errorf("restore previous configuration: %w", err)
		}
		if restart {
			if _, err := run("restart", "haven.service"); err != nil {
				return fmt.Errorf("restart after rollback: %w", err)
			}
		}
		return nil
	}
	if enable {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create systemd drop-in directory: %w", err)
		}
		if err := writeFileAtomic(path, []byte(hostingServiceConfig), 0o644); err != nil {
			return fmt.Errorf("write systemd drop-in: %w", err)
		}
	} else if existed {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove systemd drop-in: %w", err)
		}
		_ = os.Remove(filepath.Dir(path))
	}
	if _, err := run("daemon-reload"); err != nil {
		return errors.Join(err, rollback(false))
	}
	if _, err := run("restart", "haven.service"); err != nil {
		return errors.Join(err, rollback(true))
	}
	active, err := hostingExperimentStatus(run)
	if err != nil || active != enable {
		if err != nil {
			err = fmt.Errorf("verify Hosting setting: %w", err)
		} else {
			err = errors.New("systemd did not apply the Hosting setting; the previous configuration was restored")
		}
		return errors.Join(err, rollback(true))
	}
	return nil
}
