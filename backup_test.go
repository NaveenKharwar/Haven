package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomAppBackupRestore(t *testing.T) {
	ctx := t.Context()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const id = "unknown-app"
	dir := filepath.Join(root, "original")
	newDir := filepath.Join(root, "restored")
	hostFolder := filepath.Join(root, "outside")
	sourceVolume := filepath.Join(root, "volume-before")
	restoredVolume := filepath.Join(root, "volume-after")
	destination := filepath.Join(root, "backups")
	m := NewManager(dir, &Docker{}, nil, NewJobs())
	fresh := NewManager(newDir, &Docker{}, nil, NewJobs())
	for _, path := range []string{m.appDir(id), m.dataDir(id), filepath.Join(newDir, "apps"),
		filepath.Join(newDir, "appdata"), hostFolder, sourceVolume, destination} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	for path, content := range map[string]string{
		filepath.Join(m.appDir(id), "compose.yml"): "services:\n  app:\n    image: example/app:1\n",
		filepath.Join(m.appDir(id), ".env"):        "APP_DATA='" + m.dataDir(id) + "'\nPASSWORD='example-value'\n",
		filepath.Join(m.dataDir(id), "document"):   "existing paper",
		filepath.Join(hostFolder, "media"):         "existing media",
		filepath.Join(sourceVolume, "database"):    "existing records",
	} {
		if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("document", filepath.Join(m.dataDir(id), "latest")); err != nil {
		t.Fatal(err)
	}
	if err := m.writeMeta(&AppMeta{ID: id, Name: "Unknown app", Custom: true}); err != nil {
		t.Fatal(err)
	}

	docker := `#!/bin/sh
set -eu
case "$1" in
  compose)
    shift 11
    case "$1" in
      config) printf '%s\n' "$BACKUP_TEST_CONFIG" ;;
      *) exit 1 ;;
    esac
    ;;
  ps)
    if [ "$2" = "-a" ] && [ "$BACKUP_TEST_MODE" = "backup" ]; then
      echo '{"ID":"1234","State":"running","Labels":"com.docker.compose.project=haven-unknown-app"}'
    fi
    ;;
  start)
    [ "$2" = "1234" ]
    echo resume >> "$BACKUP_TEST_TRACE"
    ;;
  stop)
    [ "$2" = "1234" ]
    echo stop >> "$BACKUP_TEST_TRACE"
    ;;
  inspect) echo "$BACKUP_TEST_EXIT_CODE" ;;
  volume)
    case "$2" in
      inspect)
        [ "$6" = "haven-unknown-app_database" ]
        if [ "$BACKUP_TEST_MODE" = "backup" ]; then
          echo "$BACKUP_TEST_VOLUME"
        else
          echo "$BACKUP_TEST_RESTORED_VOLUME"
        fi
        ;;
      ls) ;;
      create) mkdir "$BACKUP_TEST_RESTORED_PATH" ;;
      rm) rm -r "$BACKUP_TEST_RESTORED_PATH" ;;
      *) exit 1 ;;
    esac
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(root, "docker"), []byte(docker), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BACKUP_TEST_CONFIG", `{"services":{"app":{"image":"example/app:1","volumes":[{"type":"bind","source":`+
		quoteBackupTestPath(m.dataDir(id))+`,"target":"/data"},{"type":"bind","source":`+
		quoteBackupTestPath(hostFolder)+`,"target":"/media"},{"type":"volume","source":"database","target":"/db"},`+
		`{"type":"volume","source":"remote","target":"/remote"},{"type":"volume","target":"/anonymous"}]}},"volumes":{`+
		`"database":{"name":"haven-unknown-app_database"},"remote":{"name":"remote","external":true}}}`)
	for key, mountpoint := range map[string]string{"BACKUP_TEST_VOLUME": sourceVolume, "BACKUP_TEST_RESTORED_VOLUME": restoredVolume} {
		data, err := json.Marshal(backupVolume{
			Name: "haven-unknown-app_database", Mountpoint: mountpoint, Driver: "local", Scope: "local",
			Labels: map[string]string{"com.docker.compose.project": "haven-unknown-app", "com.docker.compose.volume": "database", "com.haven.backup.restore": "test-restore"},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, string(data))
	}
	t.Setenv("BACKUP_TEST_RESTORED_PATH", restoredVolume)
	trace := filepath.Join(root, "trace")
	t.Setenv("BACKUP_TEST_TRACE", trace)
	t.Setenv("BACKUP_TEST_MODE", "backup")

	plan, err := m.planBackup(ctx, id)
	if err != nil || len(plan.Items) != 4 || len(plan.Omitted) != 2 {
		t.Fatalf("unexpected backup plan %+v (%v)", plan, err)
	}
	t.Setenv("BACKUP_TEST_EXIT_CODE", "137")
	if err := m.backupApp(ctx, &Job{ID: "failed"}, id, destination, nil); err == nil || !strings.Contains(err.Error(), "was killed") {
		t.Fatalf("expected a forced stop to abort the backup, got %v", err)
	}
	if entries, err := os.ReadDir(destination); err != nil || len(entries) != 0 {
		t.Fatalf("failed backup left behind files: %v (%v)", entries, err)
	}
	t.Setenv("BACKUP_TEST_EXIT_CODE", "0")
	if err := m.backupApp(ctx, &Job{ID: "test"}, id, destination, []string{hostFolder}); err != nil {
		t.Fatal(err)
	}
	if calls, err := os.ReadFile(trace); err != nil || string(calls) != "stop\nresume\nstop\nresume\n" {
		t.Fatalf("container state was not restored: %q (%v)", calls, err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one complete backup, got %v (%v)", entries, err)
	}
	backup := filepath.Join(destination, entries[0].Name())
	manifest, err := readBackupManifest(backup)
	if err != nil || manifest.AppID != id || manifest.AppName != "Unknown app" || len(manifest.Items) != 4 {
		t.Fatalf("unexpected manifest %+v (%v)", manifest, err)
	}

	t.Setenv("BACKUP_TEST_MODE", "restore")
	if err := fresh.restoreApp(ctx, &Job{}, backup, manifest, true); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected restore to refuse overwriting an existing host folder, got %v", err)
	}
	if err := os.Rename(hostFolder, hostFolder+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := fresh.restoreApp(ctx, &Job{ID: "test-restore"}, backup, manifest, true); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(fresh.dataDir(id), "document"): "existing paper",
		filepath.Join(fresh.dataDir(id), "latest"):   "existing paper",
		filepath.Join(hostFolder, "media"):           "existing media",
		filepath.Join(restoredVolume, "database"):    "existing records",
	} {
		if data, err := os.ReadFile(path); err != nil || string(data) != want {
			t.Errorf("restored %s = %q, want %q (%v)", path, data, want, err)
		}
	}
	env, err := readEnv(filepath.Join(fresh.appDir(id), ".env"))
	if err != nil || env.vals["APP_DATA"] != fresh.dataDir(id) || env.vals["PASSWORD"] != "example-value" {
		t.Errorf("unexpected environment after restore (%v)", err)
	}
	if meta, err := fresh.readMeta(id); err != nil || !meta.Custom {
		t.Errorf("custom app was not preserved: %+v (%v)", meta, err)
	}
	if err := fresh.restoreApp(ctx, &Job{}, backup, manifest, false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected restore to refuse overwriting an existing app, got %v", err)
	}
}

func quoteBackupTestPath(path string) string {
	b, _ := json.Marshal(path)
	return string(b)
}
