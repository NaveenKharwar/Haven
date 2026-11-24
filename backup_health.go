package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Backup records are separate from job history, which is intentionally pruned.
// They describe attempts made by Haven, not whether a backup still exists or
// can be restored. Older backups made before recording was added are unknown.
type backupRecord struct {
	LastSuccess *time.Time `json:"lastSuccess,omitempty"`
	LastAttempt *time.Time `json:"lastAttempt,omitempty"`
	Failed      bool       `json:"failed,omitempty"`
}
type backupHealth struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	backupRecord
}

var backupRecordsMu sync.Mutex

func (m *Manager) recordBackupResult(id string, result error) {
	backupRecordsMu.Lock()
	defer backupRecordsMu.Unlock()
	path := filepath.Join(m.appDir(id), ".backup-health.json")
	var rec backupRecord
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &rec)
	}
	now := time.Now().UTC()
	rec.LastAttempt = &now
	rec.Failed = result != nil
	if result == nil {
		rec.LastSuccess = &now
	}
	b, err := json.Marshal(rec)
	if err == nil {
		err = writeFileAtomic(path, b, 0600)
	}
	if err != nil {
		log.Printf("could not record backup result for %s: %v", id, err)
	}
}

func (s *Server) getBackupHealth(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(filepath.Join(s.apps.dir, "apps"))
	if err != nil && !os.IsNotExist(err) {
		jsonError(w, http.StatusInternalServerError, "Could not read installed apps")
		return
	}
	out := make([]backupHealth, 0, len(entries))
	backupRecordsMu.Lock()
	defer backupRecordsMu.Unlock()
	for _, e := range entries {
		if !e.IsDir() || !appIDRe.MatchString(e.Name()) {
			continue
		}
		meta, err := s.apps.readMeta(e.Name())
		if err != nil {
			continue
		}
		item := backupHealth{ID: meta.ID, Name: meta.Name}
		b, err := os.ReadFile(filepath.Join(s.apps.appDir(meta.ID), ".backup-health.json"))
		if err == nil {
			if json.Unmarshal(b, &item.backupRecord) != nil {
				jsonError(w, http.StatusInternalServerError, "Could not read backup records")
				return
			}
		} else if !os.IsNotExist(err) {
			jsonError(w, http.StatusInternalServerError, "Could not read backup records")
			return
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	writeJSON(w, http.StatusOK, out)
}
