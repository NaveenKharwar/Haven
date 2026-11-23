package main

import (
	"context"
	"net/http"
	"strings"
	"time"
)

func backupJob(action string) bool {
	return action == "backup" || action == "restore" || action == "update-with-backup"
}

func (s *Server) getBackupPlan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	plan, err := s.apps.planBackup(ctx, r.PathValue("id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Destination string
		Folders     []string
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := s.apps.startBackup(r.PathValue("id"), strings.TrimSpace(req.Destination), req.Folders)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func (s *Server) inspectBackup(w http.ResponseWriter, r *http.Request) {
	var req struct{ Path string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	path, err := directoryPath(strings.TrimSpace(req.Path))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	manifest, err := readBackupManifest(path)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path           string
		AppID          string
		RestoreFolders bool
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := s.apps.startRestore(strings.TrimSpace(req.Path), req.AppID, req.RestoreFolders)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}
