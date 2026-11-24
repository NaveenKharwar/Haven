package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupHealthRecordsAndPermissions(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, nil, nil, NewJobs())
	if err := os.MkdirAll(m.appDir("demo"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.writeMeta(&AppMeta{ID: "demo", Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{apps: m}
	read := func() backupHealth {
		t.Helper()
		w := httptest.NewRecorder()
		s.getBackupHealth(w, httptest.NewRequest("GET", "/api/backup-health", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var got []backupHealth
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got) != 1 {
			t.Fatalf("response %s: %v", w.Body.String(), err)
		}
		return got[0]
	}
	if item := read(); item.LastSuccess != nil || item.Failed {
		t.Fatalf("unknown: %+v", item)
	}
	m.recordBackupResult("demo", nil)
	success := read()
	if success.LastSuccess == nil || success.LastAttempt == nil || success.Failed {
		t.Fatalf("success: %+v", success)
	}
	m.recordBackupResult("demo", errors.New("failed"))
	failed := read()
	if !failed.Failed || failed.LastSuccess == nil || !failed.LastSuccess.Equal(*success.LastSuccess) {
		t.Fatalf("failure lost previous success: %+v", failed)
	}
	if err := os.WriteFile(filepath.Join(m.appDir("demo"), ".backup-health.json"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.getBackupHealth(w, httptest.NewRequest("GET", "/api/backup-health", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt record: %d", w.Code)
	}
	hs, clients, _ := roleServer(t)
	for name, want := range map[string]int{"boss": 200, "mia": 403, "moe": 403, "gus": 403} {
		if got := call(t, clients[name], "GET", hs.URL+"/api/backup-health", ""); got != want {
			t.Errorf("%s: got %d, want %d", name, got, want)
		}
	}
}
