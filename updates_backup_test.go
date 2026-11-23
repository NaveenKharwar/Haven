package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdatesWithBackup(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	docker := &Docker{}
	jobs := NewJobs()
	m := NewManager(root, docker, nil, jobs)
	for _, id := range []string{"skipped", "saved"} {
		for _, dir := range []string{m.appDir(id), m.dataDir(id)} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		for path, value := range map[string]string{
			filepath.Join(m.appDir(id), "compose.yml"): "services:\n  app:\n    image: example/app:1\n",
			filepath.Join(m.appDir(id), ".env"):        "APP_DATA='" + m.dataDir(id) + "'\n",
			filepath.Join(m.dataDir(id), "document"):   "data before updating",
		} {
			if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.writeMeta(&AppMeta{ID: id, Name: id, Custom: true}); err != nil {
			t.Fatal(err)
		}
	}
	// Backing up "skipped" would copy the destination into itself.
	dest := filepath.Join(m.dataDir("skipped"), "backups")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}

	dockerScript := `#!/bin/sh
set -eu
case "$1" in
  compose)
    project="$5"
    shift 11
    case "$1" in
      config) echo '{"services":{"app":{"image":"example/app:1"}}}' ;;
      pull)
        echo "pull $project" >> "$BACKUP_UPDATE_TRACE"
        if [ "$project" = "haven-saved" ]; then
          set -- "$BACKUP_UPDATE_DEST"/saved-*.haven-backup
          [ "$#" -eq 1 ]
          [ -f "$1/manifest.json" ]
          [ "$(cat "$1/1/document")" = "data before updating" ]
          printf 'data after updating' > "$BACKUP_UPDATE_DATA"
        fi
        ;;
      up) echo "up $project" >> "$BACKUP_UPDATE_TRACE" ;;
      logs) ;;
      *) exit 1 ;;
    esac
    ;;
  ps)
    if [ "$3" = "--no-trunc" ] && [ -f "$BACKUP_UPDATE_TRACE" ]; then
      for id in saved skipped; do
        if grep -qx "up haven-$id" "$BACKUP_UPDATE_TRACE"; then
          printf '{"Names":"app","State":"running","Labels":"com.docker.compose.project=haven-%s"}\n' "$id"
        fi
      done
    fi
    ;;
  image) echo prune >> "$BACKUP_UPDATE_TRACE" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte(dockerScript), 0o700); err != nil {
		t.Fatal(err)
	}
	tracePath := filepath.Join(root, "docker.log")
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BACKUP_UPDATE_TRACE", tracePath)
	t.Setenv("BACKUP_UPDATE_DEST", dest)
	t.Setenv("BACKUP_UPDATE_DATA", filepath.Join(m.dataDir("saved"), "document"))

	store, err := LoadStore(root)
	if err != nil {
		t.Fatal(err)
	}
	code, err := store.EnsureSetupToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Setup(code, "boss", "bosspass123"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddPerson("manager", "managerpass123", RoleManager, nil); err != nil {
		t.Fatal(err)
	}
	sessions := NewSessions("")
	adminSession, managerSession := sessions.Create("boss"), sessions.Create("manager")
	handler := NewServer(store, sessions, m, nil, nil, jobs, docker, nil).Routes()
	call := func(session, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
		req.Header.Set("X-Haven", "1")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	apply := func(session string, ids []string, destination *string, want int) JobView {
		t.Helper()
		payload := map[string]any{"ids": ids, "prune": destination != nil}
		if destination != nil {
			payload["backupDestination"] = *destination
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		res := call(session, http.MethodPost, "/api/updates/apply", string(body))
		if res.Code != want {
			t.Fatalf("apply updates returned %d, want %d: %s", res.Code, want, res.Body)
		}
		var job JobView
		if want == http.StatusAccepted {
			if err := json.Unmarshal(res.Body.Bytes(), &job); err != nil {
				t.Fatal(err)
			}
		}
		return job
	}
	wait := func(session, id string) JobView {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			res := call(session, http.MethodGet, "/api/jobs/"+id, "")
			var view JobView
			if res.Code != http.StatusOK {
				t.Fatalf("reading update job returned %d: %s", res.Code, res.Body)
			}
			if err := json.Unmarshal(res.Body.Bytes(), &view); err != nil {
				t.Fatal(err)
			}
			if view.Status != "running" {
				return view
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("update job did not finish")
		return JobView{}
	}

	empty := ""
	apply(managerSession, []string{"saved"}, &dest, http.StatusForbidden)
	apply(adminSession, []string{"saved"}, &empty, http.StatusBadRequest)

	private := apply(adminSession, []string{"skipped", "saved"}, &dest, http.StatusAccepted)
	view := wait(adminSession, private.ID)
	log := strings.Join(view.Log, "\n")
	if view.Status != "failed" || !strings.Contains(view.Error, "1 of 2") ||
		!strings.Contains(log, "skipped: backup failed") || !strings.Contains(log, "saved is up to date") {
		t.Fatalf("unexpected batch result %+v", view)
	}
	trace, err := os.ReadFile(tracePath)
	if err != nil || string(trace) != "pull haven-saved\nup haven-saved\nprune\n" {
		t.Fatalf("unexpected Docker operations %q (%v)", trace, err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one completed backup, got %v (%v)", entries, err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, entries[0].Name(), "1", "document")); err != nil || string(data) != "data before updating" {
		t.Fatalf("backup did not preserve the original document, got %q (%v)", data, err)
	}

	if res := call(managerSession, http.MethodGet, "/api/jobs/"+private.ID, ""); res.Code != http.StatusForbidden {
		t.Fatalf("manager could read private update job: %d %s", res.Code, res.Body)
	}
	if res := call(managerSession, http.MethodGet, "/api/jobs", ""); res.Code != http.StatusOK || strings.Contains(res.Body.String(), private.ID) {
		t.Fatalf("manager's jobs exposed the private update: %d %s", res.Code, res.Body)
	}
	allSkipped := apply(adminSession, []string{"skipped"}, &dest, http.StatusAccepted)
	if view := wait(adminSession, allSkipped.ID); view.Status != "failed" || !strings.Contains(view.Error, "1 of 1") {
		t.Fatalf("expected a failed backup to block a single update: %+v", view)
	}
	if after, err := os.ReadFile(tracePath); err != nil || string(after) != string(trace) {
		t.Fatalf("all-skipped update changed Docker state: %q (%v)", after, err)
	}

	plain := apply(managerSession, []string{"skipped"}, nil, http.StatusAccepted)
	if view := wait(managerSession, plain.ID); view.Status != "success" || view.Action != "update-all" {
		t.Fatalf("ordinary manager update changed behavior: %+v", view)
	}
}
