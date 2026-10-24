package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// The file explorer. By default it only opens the folders where apps keep
// their data and media. Full-disk mode is an explicit choice, because Haven
// runs as root and could then read or change any file on the server.

type Files struct {
	store *Store
	dir   string // Haven's data directory, e.g. /opt/haven
	admin bool   // only the admin can use full-disk mode
}

// As returns the explorer as seen by the admin or by anyone else.
func (f *Files) As(admin bool) *Files {
	c := *f
	c.admin = admin
	return &c
}

func (f *Files) fullDisk() bool { return f.admin && f.store.Settings().FilesFullDisk }

func NewFiles(store *Store, dir string) *Files {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return &Files{store: store, dir: dir}
}

type FileRoot struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Extra bool   `json:"extra,omitempty"`
}

type FilesInfo struct {
	Full  bool       `json:"full"`
	Roots []FileRoot `json:"roots"`
}

// safeRoots are always allowed in safe mode. Haven's own state (accounts,
// sessions) is deliberately outside them.
func (f *Files) safeRoots() []FileRoot {
	roots := []FileRoot{{Name: "App data", Path: filepath.Join(f.dir, "appdata")}, {Name: "Media and shares (/srv)", Path: "/srv"}}
	for _, p := range f.store.Settings().FilesExtraRoots {
		roots = append(roots, FileRoot{Name: p, Path: p, Extra: true})
	}
	return roots
}

func (f *Files) Info() FilesInfo {
	full := f.fullDisk()
	roots := f.safeRoots()
	if full {
		roots = append([]FileRoot{{Name: "Whole disk (/)", Path: "/"}}, roots...)
	}
	return FilesInfo{Full: full, Roots: roots}
}

func within(root, p string) bool {
	if root == "/" {
		return true
	}
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// Resolve cleans a requested path, follows symlinks and checks the result is
// inside an allowed folder, so a link can't lead out of it. A path that
// doesn't exist yet (a new file) is checked through its parent.
func (f *Files) Resolve(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) || strings.ContainsRune(p, 0) {
		return "", errors.New("invalid path")
	}
	p = filepath.Clean(p)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(p))
		if err != nil {
			return "", err
		}
		real = filepath.Join(parent, filepath.Base(p))
	}
	if f.fullDisk() {
		return real, nil
	}
	for _, r := range f.safeRoots() {
		root, err := filepath.EvalSymlinks(r.Path)
		if err != nil {
			continue
		}
		if within(root, real) {
			return real, nil
		}
	}
	return "", errors.New("that folder is outside the folders Haven's file explorer may open. Turn on Advanced access to open it")
}

// lexicallyAllowed checks a path that may not exist yet against the safe roots.
func (f *Files) lexicallyAllowed(p string) bool {
	for _, r := range f.safeRoots() {
		if within(r.Path, p) {
			return true
		}
		if real, err := filepath.EvalSymlinks(r.Path); err == nil && within(real, p) {
			return true
		}
	}
	return false
}

// protectedPaths can't be deleted, moved or renamed even in full-disk mode:
// removing them breaks the server.
func (f *Files) protected(p string) bool {
	fixed := []string{"/", "/bin", "/boot", "/dev", "/etc", "/home", "/lib", "/lib32", "/lib64", "/opt", "/proc", "/root", "/run", "/sbin", "/srv", "/sys", "/tmp", "/usr", "/var", "/mnt", "/media", "/snap",
		"/var/lib", "/var/log", "/var/cache", "/usr/bin", "/usr/lib", "/usr/sbin", "/usr/share", "/usr/local", f.dir, filepath.Join(f.dir, "appdata")}
	for _, x := range fixed {
		if p == x {
			return true
		}
		// /bin is a link to /usr/bin on many systems, so compare the real path too.
		if real, err := filepath.EvalSymlinks(x); err == nil && p == real {
			return true
		}
	}
	// Anything directly under /, such as /private on a Mac or /lost+found.
	if filepath.Dir(p) == "/" {
		return true
	}
	if strings.HasPrefix(p, f.dir+string(filepath.Separator)) {
		rest := strings.TrimPrefix(p, f.dir+string(filepath.Separator))
		switch rest {
		case "state.json", "sessions.json", "setup-token", "notifications.json", "jobs.json":
			return true
		}
	}
	return false
}

type FileEntry struct {
	Name string    `json:"name"`
	Path string    `json:"path"`
	Dir  bool      `json:"dir"`
	Size int64     `json:"size"`
	Mod  time.Time `json:"mod"`
	Mode string    `json:"mode"`
	Link bool      `json:"link,omitempty"`
}

type FileListing struct {
	Path    string      `json:"path"`
	Parent  string      `json:"parent,omitempty"`
	Entries []FileEntry `json:"entries"`
	Total   int         `json:"total"`
}

const maxListing = 2000

func (f *Files) List(p string) (*FileListing, error) {
	real, err := f.Resolve(p)
	if err != nil {
		return nil, err
	}
	des, err := os.ReadDir(real)
	if err != nil {
		return nil, err
	}
	out := &FileListing{Path: real, Total: len(des)}
	if real != "/" {
		parent := filepath.Dir(real)
		if _, perr := f.Resolve(parent); perr == nil {
			out.Parent = parent
		}
	}
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			continue
		}
		e := FileEntry{Name: de.Name(), Path: filepath.Join(real, de.Name()), Dir: de.IsDir(), Size: info.Size(), Mod: info.ModTime(), Mode: info.Mode().Perm().String()}
		if info.Mode()&os.ModeSymlink != 0 {
			e.Link = true
			if st, err := os.Stat(e.Path); err == nil {
				e.Dir = st.IsDir()
				e.Size = st.Size()
			}
		}
		out.Entries = append(out.Entries, e)
	}
	sort.Slice(out.Entries, func(a, b int) bool {
		x, y := out.Entries[a], out.Entries[b]
		if x.Dir != y.Dir {
			return x.Dir
		}
		return strings.ToLower(x.Name) < strings.ToLower(y.Name)
	})
	if len(out.Entries) > maxListing {
		out.Entries = out.Entries[:maxListing]
	}
	return out, nil
}

func validName(n string) error {
	if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\x00") || len(n) > 255 {
		return errors.New("that name isn't allowed")
	}
	return nil
}

// chownLike gives a new file the same owner as the folder it is in, so files
// uploaded through Haven stay usable by the apps and the user.
func chownLike(path string) {
	st, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(path, int(sys.Uid), int(sys.Gid))
	}
}

func (f *Files) Mkdir(p string) error {
	real, err := f.Resolve(p)
	if err != nil {
		return err
	}
	if err := validName(filepath.Base(real)); err != nil {
		return err
	}
	if err := os.Mkdir(real, 0o755); err != nil {
		return err
	}
	chownLike(real)
	return nil
}

func (f *Files) Rename(p, name string) error {
	if err := validName(name); err != nil {
		return err
	}
	real, err := f.Resolve(p)
	if err != nil {
		return err
	}
	if f.protected(real) {
		return errors.New("this is a system folder and can't be renamed")
	}
	dst := filepath.Join(filepath.Dir(real), name)
	if _, err := os.Lstat(dst); err == nil {
		return errors.New("something with that name already exists")
	}
	return os.Rename(real, dst)
}

func (f *Files) Delete(paths []string) error {
	var errs []string
	for _, p := range paths {
		real, err := f.Resolve(p)
		if err == nil && f.protected(real) {
			err = errors.New("this is a system folder and can't be deleted")
		}
		if err == nil {
			err = os.RemoveAll(real)
		}
		if err != nil {
			errs = append(errs, filepath.Base(p)+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Move moves or copies items into a folder.
func (f *Files) Move(paths []string, to string, copy bool) error {
	dstDir, err := f.Resolve(to)
	if err != nil {
		return err
	}
	if st, err := os.Stat(dstDir); err != nil || !st.IsDir() {
		return errors.New("the destination is not a folder")
	}
	for _, p := range paths {
		src, err := f.Resolve(p)
		if err != nil {
			return err
		}
		if !copy && f.protected(src) {
			return errors.New(filepath.Base(src) + " is a system folder and can't be moved")
		}
		if within(src, dstDir) {
			return errors.New("can't put a folder inside itself")
		}
		dst := filepath.Join(dstDir, filepath.Base(src))
		if _, err := os.Lstat(dst); err == nil {
			return errors.New(filepath.Base(src) + " already exists there")
		}
		if copy {
			err = copyTree(src, dst)
		} else {
			err = os.Rename(src, dst)
			if err != nil { // across disks
				if err = copyTree(src, dst); err == nil {
					err = os.RemoveAll(src)
				}
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case st.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case st.IsDir():
		if err := os.Mkdir(dst, st.Mode().Perm()); err != nil {
			return err
		}
		copyOwner(src, dst)
		des, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, de := range des {
			if err := copyTree(filepath.Join(src, de.Name()), filepath.Join(dst, de.Name())); err != nil {
				return err
			}
		}
		return nil
	case st.Mode().IsRegular():
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		copyOwner(src, dst)
		return out.Close()
	}
	return nil // sockets, devices: skipped
}

func copyOwner(src, dst string) {
	if st, err := os.Stat(src); err == nil {
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			_ = os.Chown(dst, int(sys.Uid), int(sys.Gid))
		}
	}
}

const maxEditBytes = 2 << 20

// ReadText returns a text file for the editor. Binary or large files are refused.
func (f *Files) ReadText(p string) (string, error) {
	real, err := f.Resolve(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", errors.New("that is a folder")
	}
	if st.Size() > maxEditBytes {
		return "", errors.New("this file is too large to edit here (over 2 MB); download it instead")
	}
	b, err := os.ReadFile(real)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return "", errors.New("this doesn't look like a text file; download it instead")
	}
	return string(b), nil
}

func (f *Files) WriteText(p, content string) error {
	real, err := f.Resolve(p)
	if err != nil {
		return err
	}
	if len(content) > maxEditBytes {
		return errors.New("the text is too large")
	}
	if st, err := os.Stat(real); err == nil {
		if st.IsDir() {
			return errors.New("that is a folder")
		}
		return writeKeepingMode(real, []byte(content), st.Mode().Perm())
	}
	if err := writeFileAtomic(real, []byte(content), 0o644); err != nil {
		return err
	}
	chownLike(real)
	return nil
}

func writeKeepingMode(path string, b []byte, perm os.FileMode) error {
	st, _ := os.Stat(path)
	if err := writeFileAtomic(path, b, perm); err != nil {
		return err
	}
	if st != nil {
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			_ = os.Chown(path, int(sys.Uid), int(sys.Gid))
		}
	}
	return nil
}

// --- HTTP ---------------------------------------------------------------------

func (s *Server) filesFor(r *http.Request) *Files {
	return s.files.As(person(r).Role == RoleAdmin)
}

func (s *Server) audit(r *http.Request, format string, args ...any) {
	log.Printf("files: %s %s", currentUser(r), fmt.Sprintf(format, args...))
}

func (s *Server) filesInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.filesFor(r).Info())
}

func (s *Server) filesList(w http.ResponseWriter, r *http.Request) {
	l, err := s.filesFor(r).List(r.URL.Query().Get("path"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func friendlyFSError(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "that file or folder doesn't exist"
	case errors.Is(err, os.ErrPermission):
		return "permission denied"
	}
	return err.Error()
}

// Types the browser may show in place. Everything else downloads. Video,
// audio and PDF use the browser's own players; ServeContent handles seeking.
var inlineTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml", ".bmp": "image/bmp",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".ogv": "video/ogg", ".mov": "video/quicktime", ".mkv": "video/x-matroska",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".wav": "audio/wav", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".flac": "audio/flac", ".opus": "audio/ogg",
	".pdf": "application/pdf",
}

func (s *Server) filesDownload(w http.ResponseWriter, r *http.Request) {
	real, err := s.filesFor(r).Resolve(r.URL.Query().Get("path"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	fh, err := os.Open(real)
	if err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil || st.IsDir() {
		jsonError(w, http.StatusBadRequest, "that is a folder")
		return
	}
	ext := strings.ToLower(filepath.Ext(real))
	if ct, ok := inlineTypes[ext]; ok && r.URL.Query().Get("inline") == "1" {
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition", "inline")
		if ct == "application/pdf" {
			w.Header().Set("X-Frame-Options", "SAMEORIGIN") // shown in a frame inside Haven
			w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
		} else {
			w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		}
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(real)}))
	}
	if _, inline := inlineTypes[ext]; !inline || r.URL.Query().Get("inline") != "1" {
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	}
	s.audit(r, "opened %s", real)
	http.ServeContent(w, r, filepath.Base(real), st.ModTime(), fh)
}

func (s *Server) filesRead(w http.ResponseWriter, r *http.Request) {
	t, err := s.filesFor(r).ReadText(r.URL.Query().Get("path"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": t})
}

func (s *Server) filesWrite(w http.ResponseWriter, r *http.Request) {
	var req struct{ Path, Content string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.filesFor(r).WriteText(req.Path, req.Content); err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	s.audit(r, "edited %s", req.Path)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) filesMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct{ Path string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.filesFor(r).Mkdir(req.Path); err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	s.audit(r, "created folder %s", req.Path)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) filesRename(w http.ResponseWriter, r *http.Request) {
	var req struct{ Path, Name string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.filesFor(r).Rename(req.Path, req.Name); err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	s.audit(r, "renamed %s to %s", req.Path, req.Name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) filesMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string
		To    string
		Copy  bool
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.filesFor(r).Move(req.Paths, req.To, req.Copy); err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	s.audit(r, "moved/copied (copy=%v) %v to %s", req.Copy, req.Paths, req.To)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) filesDelete(w http.ResponseWriter, r *http.Request) {
	var req struct{ Paths []string }
	if err := readJSON(r, &req); err != nil || len(req.Paths) == 0 {
		jsonError(w, http.StatusBadRequest, "nothing to delete")
		return
	}
	s.audit(r, "deleting %v", req.Paths)
	if err := s.filesFor(r).Delete(req.Paths); err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// filesUpload streams files straight to disk, so big uploads don't use memory.
func (s *Server) filesUpload(w http.ResponseWriter, r *http.Request) {
	dir, err := s.filesFor(r).Resolve(r.URL.Query().Get("path"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	n := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			jsonError(w, http.StatusBadRequest, "upload interrupted")
			return
		}
		if err := s.saveUpload(dir, part); err != nil {
			jsonError(w, http.StatusBadRequest, friendlyFSError(err))
			return
		}
		n++
	}
	s.audit(r, "uploaded %d file(s) to %s", n, dir)
	writeJSON(w, http.StatusOK, map[string]int{"files": n})
}

func (s *Server) saveUpload(dir string, part *multipart.Part) error {
	defer part.Close()
	name := filepath.Base(part.FileName())
	if part.FileName() == "" {
		return nil
	}
	if err := validName(name); err != nil {
		return err
	}
	dst := filepath.Join(dir, name)
	if _, err := os.Lstat(dst); err == nil {
		return errors.New(name + " already exists in this folder")
	}
	tmp := dst + ".haven-upload"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, part); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	chownLike(tmp)
	return os.Rename(tmp, dst)
}

// filesAccess changes the access level. Full-disk needs accept=true, which
// the UI sends only after the person has read and accepted the warning.
func (s *Server) filesAccess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Full   *bool  `json:"full"`
		Accept bool   `json:"accept"`
		Add    string `json:"add"`
		Remove string `json:"remove"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Full != nil {
		if *req.Full && !req.Accept {
			jsonError(w, http.StatusBadRequest, "you must accept the risk to turn on Advanced access")
			return
		}
		_ = s.store.UpdateSettings(func(h *HostSettings) { h.FilesFullDisk = *req.Full })
		s.audit(r, "full-disk file access set to %v", *req.Full)
	}
	if req.Add != "" {
		p := filepath.Clean(req.Add)
		if !filepath.IsAbs(p) || p == "/" {
			jsonError(w, http.StatusBadRequest, "enter a full folder path such as /mnt/usb")
			return
		}
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			jsonError(w, http.StatusBadRequest, "that folder doesn't exist on the server")
			return
		}
		_ = s.store.UpdateSettings(func(h *HostSettings) {
			for _, x := range h.FilesExtraRoots {
				if x == p {
					return
				}
			}
			h.FilesExtraRoots = append(h.FilesExtraRoots, p)
		})
		s.audit(r, "added file folder %s", p)
	}
	if req.Remove != "" {
		_ = s.store.UpdateSettings(func(h *HostSettings) {
			keep := h.FilesExtraRoots[:0]
			for _, x := range h.FilesExtraRoots {
				if x != req.Remove {
					keep = append(keep, x)
				}
			}
			h.FilesExtraRoots = keep
		})
	}
	writeJSON(w, http.StatusOK, s.filesFor(r).Info())
}

var _ = strconv.Itoa
