package main

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRootCommandShowsHelpInsteadOfServing(t *testing.T) {
	var out bytes.Buffer
	served := false
	cmd := newRootCommand(strings.NewReader(""), &out, &out, func(string, string) { served = true })
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if served {
		t.Fatal("bare haven started the server")
	}
	text := out.String()
	for _, expected := range []string{"SERVICE", "restart", "experimental", "serve", "Haven already running?"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("help missing %q:\n%s", expected, text)
		}
	}
}

func TestServeCommandAndLegacyServerFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"serve command", []string{"serve", "--addr", ":9000", "--data", "/srv/haven"}},
		{"legacy flags", []string{"--addr", ":9000", "--data", "/srv/haven"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			cmd := newRootCommand(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, func(addr, data string) {
				got = []string{addr, data}
			})
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if want := []string{":9000", "/srv/haven"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("server arguments = %v, want %v", got, want)
			}
		})
	}
}

func TestExperimentalAndLogsHelp(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"experimental", "--help"}, []string{"HAVEN EXPERIMENTAL", "sudo haven experimental"}},
		{[]string{"logs", "--help"}, []string{"HAVEN LOGS", "--follow", "--lines"}},
		{[]string{"hosting", "--help"}, []string{"enable", "disable", "status"}},
	} {
		var out bytes.Buffer
		cmd := newRootCommand(strings.NewReader(""), &out, &out, func(string, string) {})
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, want := range tc.want {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("%v help missing %q:\n%s", tc.args, want, out.String())
			}
		}
	}
}

func TestServiceActionRequiresRootAndRunsSystemctl(t *testing.T) {
	if err := runHavenServiceAction("restart", &bytes.Buffer{}, func(...string) (string, error) {
		t.Fatal("systemctl called without root")
		return "", nil
	}, false); err == nil || !strings.Contains(err.Error(), "sudo haven restart") {
		t.Fatalf("unexpected non-root error: %v", err)
	}
	var calls []string
	var out bytes.Buffer
	err := runHavenServiceAction("restart", &out, func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}, true)
	if err != nil || !reflect.DeepEqual(calls, []string{"restart haven.service"}) || !strings.Contains(out.String(), "restarted") {
		t.Fatalf("restart result: err=%v calls=%v output=%q", err, calls, out.String())
	}
}

func TestServiceStatusIncludesHostingAndPort(t *testing.T) {
	values := map[string]string{
		"ActiveState": "active", "SubState": "running", "UnitFileState": "enabled", "MainPID": "1234",
		"ExecStart":   "{ path=/usr/local/bin/haven ; argv[]=/usr/local/bin/haven serve --addr :8484 --data /opt/haven ; }",
		"Environment": "HAVEN_HOSTING_EXPERIMENTAL=1",
	}
	run := func(args ...string) (string, error) {
		for name, value := range values {
			if strings.Contains(strings.Join(args, " "), "--property="+name) {
				return value, nil
			}
		}
		return "", nil
	}
	var out bytes.Buffer
	if err := printHavenServiceStatus(&out, run); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"active (running)", "enabled", "1234", "8484", "enabled (experimental)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("status missing %q:\n%s", want, text)
		}
	}
}

func TestNormalizeLegacyCLIArgs(t *testing.T) {
	got := normalizeLegacyCLIArgs([]string{"-addr", ":9000", "-data", "/srv/haven", "-version"})
	want := []string{"--addr", ":9000", "--data", "/srv/haven", "--version"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized args = %v, want %v", got, want)
	}
}

func TestInstallerUsesExplicitServeCommand(t *testing.T) {
	content, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "ExecStart=/usr/local/bin/haven serve --addr :$PORT --data $DATA") {
		t.Fatal("installer service does not use the explicit serve command")
	}
}
