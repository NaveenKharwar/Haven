package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	uploadStateDir = ".file-uploads"
	maxUploadChunk = 4 << 20
	uploadLifetime = 24 * time.Hour
)

type fileUploads struct {
	dir       string
	locks     [32]sync.Mutex
	sweepMu   sync.Mutex
	lastSweep time.Time
}

type uploadProgress struct {
	Offset int64 `json:"offset"`
	Done   bool  `json:"done"`
}

func (u *fileUploads) lock(key string) *sync.Mutex {
	return &u.locks[key[0]%byte(len(u.locks))]
}

func (u *fileUploads) sweep(now time.Time) {
	u.sweepMu.Lock()
	defer u.sweepMu.Unlock()
	if now.Sub(u.lastSweep) < time.Hour {
		return
	}
	u.lastSweep = now
	entries, _ := os.ReadDir(u.dir)
	for _, entry := range entries {
		key, _, _ := strings.Cut(entry.Name(), ".")
		if len(key) != sha256.Size*2 || entry.IsDir() {
			continue
		}
		mu := u.lock(key)
		if !mu.TryLock() {
			continue
		}
		path := filepath.Join(u.dir, entry.Name())
		if st, err := os.Lstat(path); err == nil && now.Sub(st.ModTime()) > uploadLifetime {
			_ = os.Remove(path)
		}
		mu.Unlock()
	}
}

func parseUploadSize(value string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0, errors.New("invalid upload size or offset")
	}
	return n, nil
}

func (s *Server) filesUploadChunk(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	id, idErr := hex.DecodeString(query.Get("id"))
	name := query.Get("name")
	size, sizeErr := parseUploadSize(query.Get("size"))
	if idErr != nil || len(id) != 16 || validName(name) != nil || sizeErr != nil {
		jsonError(w, http.StatusBadRequest, "invalid upload details")
		return
	}
	var offset int64
	if r.Method == http.MethodPut {
		var err error
		offset, err = parseUploadSize(query.Get("offset"))
		if err != nil || offset > size || r.ContentLength < 0 || r.ContentLength > maxUploadChunk || r.ContentLength > size-offset ||
			(offset < size && r.ContentLength == 0) {
			jsonError(w, http.StatusBadRequest, "invalid upload chunk")
			return
		}
	}
	files := s.filesFor(r)
	dir, err := files.Resolve(query.Get("path"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	defer root.Close()
	if _, err := files.Resolve(filepath.Join(dir, name)); err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}

	u := files.uploads
	u.sweep(time.Now())
	digest := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%s\x00%d\x00%x", currentUser(r), dir, name, size, id))
	key := hex.EncodeToString(digest[:])
	mu := u.lock(key)
	mu.Lock()
	defer mu.Unlock()

	result, err := u.progress(key, size)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, friendlyFSError(err))
		return
	}
	if r.Method == http.MethodGet || result.Done {
		writeJSON(w, http.StatusOK, result)
		return
	}
	if result.Offset != offset {
		writeJSON(w, http.StatusConflict, result)
		return
	}
	if offset == 0 {
		if err := checkUploadDestination(root, name); err != nil {
			// A completed empty file can also be recovering a lost commit response.
			if _, prepared := os.Stat(filepath.Join(u.dir, key+".ready")); size != 0 || prepared != nil {
				jsonError(w, http.StatusConflict, friendlyFSError(err))
				return
			}
		}
	}
	if err := os.MkdirAll(u.dir, 0o700); err != nil {
		jsonError(w, http.StatusInternalServerError, friendlyFSError(err))
		return
	}
	stage := filepath.Join(u.dir, key+".part")
	if offset < size {
		if err := appendUploadChunk(stage, offset, r.ContentLength, r.Body); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				status = http.StatusServiceUnavailable
			}
			jsonError(w, status, friendlyFSError(err))
			return
		}
		result.Offset += r.ContentLength
	} else {
		if err := finishUpload(root, name, stage, size); err != nil {
			jsonError(w, http.StatusBadRequest, friendlyFSError(err))
			return
		}
		result.Done = true
		s.audit(r, "uploaded %s to %s", name, dir)
	}
	writeJSON(w, http.StatusOK, result)
}

func (u *fileUploads) progress(key string, size int64) (uploadProgress, error) {
	if _, err := os.Stat(filepath.Join(u.dir, key+".done")); err == nil {
		return uploadProgress{Offset: size, Done: true}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return uploadProgress{}, err
	}
	st, err := os.Stat(filepath.Join(u.dir, key+".part"))
	if errors.Is(err, os.ErrNotExist) {
		return uploadProgress{}, nil
	}
	if err != nil {
		return uploadProgress{}, err
	}
	if st.Size() > size {
		return uploadProgress{}, errors.New("saved upload is larger than the selected file")
	}
	return uploadProgress{Offset: st.Size()}, nil
}

func appendUploadChunk(path string, offset, size int64, in io.Reader) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	// Keep bytes actually received, even if the connection ends mid-chunk.
	// The next request reads the offset from the staging file.
	_, copyErr := io.CopyN(file, in, size)
	return errors.Join(copyErr, file.Sync())
}

func finishUpload(root *os.Root, name, stage string, size int64) error {
	base := strings.TrimSuffix(stage, ".part")
	ready, done := base+".ready", base+".done"
	var in io.Reader = strings.NewReader("")
	if size > 0 {
		f, err := os.Open(stage)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	// Record the bytes before publishing so a lost reply or interrupted commit
	// can be recognized without overwriting a destination created by someone else.
	if expected, err := os.ReadFile(ready); err == nil {
		if existingMatchesUpload(root, name, size, expected) {
			if err := os.Rename(ready, done); err != nil {
				return err
			}
			_ = os.Remove(stage)
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	hash := sha256.New()
	err := publishUpload(root, name, io.TeeReader(in, hash), func() error {
		return writeFileAtomic(ready, hash.Sum(nil), 0o600)
	})
	if err != nil {
		return err
	}
	if err := os.Rename(ready, done); err != nil {
		return err
	}
	_ = os.Remove(stage)
	return nil
}

func existingMatchesUpload(root *os.Root, name string, size int64, expected []byte) bool {
	st, err := root.Lstat(name)
	if err != nil || !st.Mode().IsRegular() || st.Size() != size {
		return false
	}
	file, err := root.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, size))
	return err == nil && n == size && bytes.Equal(hash.Sum(nil), expected)
}
