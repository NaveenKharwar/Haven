package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestResumableUploadContinuesAfterInterruptedChunk(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "appdata")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(data)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{files: NewFiles(store, data)}
	q := url.Values{
		"id": {"0123456789abcdef0123456789abcdef"}, "path": {dir},
		"name": {"photo.jpg"}, "size": {"9"},
	}
	upload := func(method, user, body string, offset int, promised int64, wantStatus int) uploadProgress {
		t.Helper()
		q.Set("offset", strconv.Itoa(offset))
		request := httptest.NewRequest(method, "/api/files/upload?"+q.Encode(), strings.NewReader(body))
		request.ContentLength = promised
		request = request.WithContext(context.WithValue(request.Context(), userKey, user))
		rec := httptest.NewRecorder()
		s.filesUploadChunk(rec, request)
		if rec.Code != wantStatus {
			t.Fatalf("%s offset %d returned %d, want %d: %s", method, offset, rec.Code, wantStatus, rec.Body)
		}
		var progress uploadProgress
		if (rec.Code == http.StatusOK || rec.Code == http.StatusConflict) && json.Unmarshal(rec.Body.Bytes(), &progress) != nil {
			t.Fatalf("invalid response: %s", rec.Body)
		}
		return progress
	}
	upload("PUT", "manager", "abc", 0, 5, http.StatusServiceUnavailable)
	if _, err := os.Stat(filepath.Join(dir, "photo.jpg")); !os.IsNotExist(err) {
		t.Fatalf("incomplete upload was published: %v", err)
	}
	s.files = NewFiles(store, data) // Simulate a Haven restart.
	if got := upload("GET", "manager", "", 0, 0, http.StatusOK); got.Offset != 3 || got.Done {
		t.Fatalf("unexpected resume status: %+v", got)
	}
	if got := upload("GET", "another-manager", "", 0, 0, http.StatusOK); got.Offset != 0 {
		t.Fatalf("progress was visible to another user: %+v", got)
	}
	if got := upload("PUT", "manager", "abcdef", 0, 6, http.StatusConflict); got.Offset != 3 {
		t.Fatalf("expected conflict with corrected offset: %+v", got)
	}
	if got := upload("PUT", "manager", "defghi", 3, 6, http.StatusOK); got.Offset != 9 || got.Done {
		t.Fatalf("unexpected progress: %+v", got)
	}
	if got := upload("PUT", "manager", "", 9, 0, http.StatusOK); !got.Done {
		t.Fatalf("commit was not recorded: %+v", got)
	}
	if got := upload("PUT", "manager", "", 9, 0, http.StatusOK); !got.Done {
		t.Fatalf("repeated commit was not recognized: %+v", got)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "photo.jpg")); string(content) != "abcdefghi" || err != nil {
		t.Fatalf("published file: %q, %v", content, err)
	}
	q.Set("id", "abcdef0123456789abcdef0123456789")
	upload("PUT", "manager", "overwrite", 0, 9, http.StatusConflict)
	if content, err := os.ReadFile(filepath.Join(dir, "photo.jpg")); string(content) != "abcdefghi" || err != nil {
		t.Fatalf("upload clobbered existing file: %q, %v", content, err)
	}
}

func TestFilesMoveValidatesWholeSelectionBeforeChangingFiles(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "appdata")
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	for _, name := range []string{first, second, filepath.Join(target, "second")} {
		if err := os.WriteFile(name, []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := LoadStore(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewFiles(store, data).Move([]string{first, second}, target, false); err == nil {
		t.Fatal("expected a destination conflict")
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("the first file was moved despite the conflict: %v", err)
	}
}

func TestFilesEntryOperationsKeepSymlinkTargetIntact(t *testing.T) {
	data := t.TempDir()
	dir := filepath.Join(data, "appdata")
	targetDir := filepath.Join(dir, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(target, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStore(data)
	if err != nil {
		t.Fatal(err)
	}
	files := NewFiles(store, data)
	if err := files.Rename(link, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := files.Move([]string{filepath.Join(dir, "renamed")}, targetDir, false); err != nil {
		t.Fatal(err)
	}
	if err := files.Delete([]string{filepath.Join(targetDir, "renamed")}); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "keep me" {
		t.Fatalf("symlink target changed: %q, %v", content, err)
	}
}
