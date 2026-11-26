package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigureHostingExperiment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haven.service.d", "90-haven-hosting-experimental.conf")
	var calls []string
	run := func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "show" {
			if _, err := os.Stat(path); err == nil {
				return "OTHER=value 'HAVEN_HOSTING_EXPERIMENTAL=1'", nil
			}
			return "OTHER=value", nil
		}
		return "", nil
	}
	if err := configureHostingExperiment(path, true, run); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != hostingServiceConfig {
		t.Fatalf("unexpected drop-in: %q, %v", content, err)
	}
	if err := configureHostingExperiment(path, false, run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed drop-in was not removed: %v", err)
	}
	want := []string{
		"daemon-reload", "restart haven.service", "show haven.service --property=Environment --value",
		"daemon-reload", "restart haven.service", "show haven.service --property=Environment --value",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected systemctl calls:\nwant %v\n got %v", want, calls)
	}
}

func TestConfigureHostingExperimentPreservesForeignDropIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haven.service.d", "90-haven-hosting-experimental.conf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const foreign = "[Service]\nEnvironment=KEEP_ME=1\n"
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	err := configureHostingExperiment(path, true, func(...string) (string, error) { called = true; return "", nil })
	if err == nil || !strings.Contains(err.Error(), "not managed by Haven") {
		t.Fatalf("expected ownership error, got %v", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil || string(content) != foreign || called {
		t.Fatalf("foreign drop-in changed: %q, %v; systemctl called=%v", content, readErr, called)
	}
}

func TestConfigureHostingExperimentRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haven.service.d", "90-haven-hosting-experimental.conf")
	restarts := 0
	reloads := 0
	err := configureHostingExperiment(path, true, func(args ...string) (string, error) {
		if args[0] == "daemon-reload" {
			reloads++
		}
		if args[0] == "restart" {
			restarts++
			if restarts == 1 {
				return "", errors.New("restart failed")
			}
		}
		return "", nil
	})
	if err == nil || restarts != 2 || reloads != 2 {
		t.Fatalf("expected restart failure and rollback, got %v after %d restarts and %d reloads", err, restarts, reloads)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed enable left its drop-in behind: %v", statErr)
	}
}

func TestConfigureHostingExperimentRollsBackFailedVerification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haven.service.d", "90-haven-hosting-experimental.conf")
	var calls []string
	err := configureHostingExperiment(path, true, func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})
	if err == nil || !strings.Contains(err.Error(), "did not apply") {
		t.Fatalf("expected verification failure, got %v", err)
	}
	want := []string{
		"daemon-reload", "restart haven.service", "show haven.service --property=Environment --value",
		"daemon-reload", "restart haven.service",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected systemctl calls:\nwant %v\n got %v", want, calls)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed verification left its drop-in behind: %v", statErr)
	}
}

func TestHostingExperimentStatus(t *testing.T) {
	enabled, err := hostingExperimentStatus(func(args ...string) (string, error) {
		return `"PORT=8484 HAVEN_HOSTING_EXPERIMENTAL=1"`, nil
	})
	if err != nil || !enabled {
		t.Fatalf("expected enabled status, got %v, %v", enabled, err)
	}
}

func TestExperimentalMenuListsAndEnablesHosting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "haven.service.d", "90-haven-hosting-experimental.conf")
	run := func(args ...string) (string, error) {
		if args[0] == "show" {
			if _, err := os.Stat(path); err == nil {
				return "HAVEN_HOSTING_EXPERIMENTAL=1", nil
			}
			return "", nil
		}
		return "", nil
	}
	var out bytes.Buffer
	if err := runExperimentalMenu(strings.NewReader("1\ny\n"), &out, path, run, true); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Website and App Hosting [disabled]") ||
		!strings.Contains(text, "Experimental Hosting is enabled") {
		t.Fatalf("unexpected menu output:\n%s", text)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != hostingServiceConfig {
		t.Fatalf("menu did not enable Hosting: %q, %v", content, err)
	}
}

func TestExperimentalMenuRequiresRootToToggle(t *testing.T) {
	var out bytes.Buffer
	err := runExperimentalMenu(strings.NewReader("1\ny\n"), &out, filepath.Join(t.TempDir(), "drop-in"), func(args ...string) (string, error) {
		return "", nil
	}, false)
	if err == nil || !strings.Contains(err.Error(), "sudo haven experimental") {
		t.Fatalf("expected sudo guidance, got %v", err)
	}
}
