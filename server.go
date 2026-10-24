package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	store       *Store
	sessions    *Sessions
	limiter     *LoginLimiter
	pairs       *Pairings
	pairLimiter *LoginLimiter
	apps        *Manager
	catalog     *Catalog
	sys         *SystemMonitor
	jobs        *Jobs
	docker      *Docker
	static      fs.FS

	remote    *Remote
	localName *LocalName
	port80    *ExtraListener
	widgets   *Widgets
	network   *Network
	access    *AccessLog
	notes     *Notifier
	updates   *UpdateChecker
	files     *Files
	checks    *StatusChecker
	self      *SelfUpdate
	cleaner   *Cleaner
	devices   *Devices
}

func NewServer(st *Store, sessions *Sessions, apps *Manager, cat *Catalog, sys *SystemMonitor, jobs *Jobs, d *Docker, static fs.FS) *Server {
	return &Server{store: st, sessions: sessions, limiter: NewLoginLimiter(), pairs: NewPairings(), pairLimiter: NewLoginLimiter(),
		apps: apps, catalog: cat, sys: sys, jobs: jobs, docker: d, static: static}
}

type ctxKey string

const userKey ctxKey = "user"

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /api/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/auth/setup", s.setup)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("POST /api/auth/tv/start", s.tvStart)
	mux.HandleFunc("POST /api/auth/tv/poll", s.tvPoll)

	// Signed in
	auth := func(h http.HandlerFunc) http.Handler { return s.requireAuth(h) }
	mux.Handle("POST /api/auth/password", auth(s.changePassword))
	mux.Handle("POST /api/auth/tv/approve", auth(s.tvApprove))
	mux.Handle("GET /api/system", auth(s.system))
	mux.Handle("GET /api/catalog", s.need(RoleManager, s.listCatalog))
	mux.Handle("POST /api/preflight", s.need(RoleManager, s.preflight))
	mux.Handle("GET /api/apps", auth(s.listApps))
	mux.Handle("POST /api/apps", s.need(RoleManager, s.installApp))
	mux.Handle("POST /api/apps/custom", s.need(RoleAdmin, s.installCustom))
	mux.Handle("GET /api/apps/{id}", s.need(RoleManager, s.getApp))
	mux.Handle("PUT /api/apps/{id}/settings", s.need(RoleManager, s.reconfigure))
	mux.Handle("GET /api/apps/{id}/logs", s.need(RoleManager, s.appLogs))
	mux.Handle("POST /api/apps/{id}/{action}", s.need(RoleMember, s.appAction))
	mux.Handle("DELETE /api/apps/{id}", s.need(RoleManager, s.uninstall))
	mux.Handle("POST /api/containers/{id}/{action}", s.need(RoleAdmin, s.containerAction))
	mux.Handle("GET /api/containers/{id}/logs", s.need(RoleAdmin, s.containerLogs))
	mux.Handle("GET /api/jobs", s.need(RoleManager, s.listJobs))
	mux.Handle("GET /api/jobs/{id}", s.need(RoleMember, s.getJob))
	mux.Handle("POST /api/apps/{id}/diagnose", s.need(RoleManager, s.diagnoseApp))
	mux.Handle("GET /api/diagnose", s.need(RoleAdmin, s.diagnoseServer))
	mux.Handle("GET /api/host/power", s.need(RoleAdmin, s.getPower))
	mux.Handle("POST /api/host/power", s.need(RoleAdmin, s.setPower))
	mux.Handle("GET /api/host/name", s.need(RoleAdmin, s.getLocalName))
	mux.Handle("POST /api/host/name", s.need(RoleAdmin, s.setLocalName))
	mux.Handle("POST /api/host/port80", s.need(RoleAdmin, s.setPort80))
	mux.Handle("GET /api/remote", s.need(RoleAdmin, s.getRemote))
	mux.Handle("POST /api/remote/{action}", s.need(RoleAdmin, s.remoteAction))
	mux.Handle("GET /api/logs/access", s.need(RoleAdmin, s.getAccessLog))
	mux.Handle("GET /api/logs/avahi", s.need(RoleAdmin, s.getAvahiLog))
	mux.Handle("GET /api/host/name/test", s.need(RoleAdmin, s.testLocalName))
	mux.Handle("POST /api/host/name/lan-only", s.need(RoleAdmin, s.lanOnly))
	mux.Handle("GET /api/updates", s.need(RoleManager, s.getUpdates))
	mux.Handle("POST /api/updates/check", s.need(RoleManager, s.checkUpdates))
	mux.Handle("POST /api/updates/apply", s.need(RoleManager, s.applyUpdates))
	mux.Handle("GET /api/notifications", s.need(RoleManager, s.getNotifications))
	mux.Handle("POST /api/notifications/read", s.need(RoleManager, s.readNotifications))
	mux.Handle("POST /api/notifications/clear", s.need(RoleManager, s.clearNotifications))
	mux.Handle("GET /api/network", s.need(RoleAdmin, s.getNetwork))
	mux.Handle("GET /api/network/wifi", s.need(RoleAdmin, s.scanWifi))
	mux.Handle("POST /api/network/wifi/connect", s.need(RoleAdmin, s.connectWifi))
	mux.Handle("POST /api/network/wifi/forget", s.need(RoleAdmin, s.forgetWifi))
	mux.Handle("POST /api/network/static", s.need(RoleAdmin, s.setStatic))
	mux.Handle("POST /api/network/setup", s.need(RoleAdmin, s.setupNetwork))
	mux.Handle("GET /api/insights", s.need(RoleAdmin, s.getInsights))
	mux.Handle("POST /api/insights/public", s.need(RoleAdmin, s.getPublicAddress))
	mux.Handle("GET /api/insights/devices", s.need(RoleAdmin, s.listDevices))
	mux.Handle("POST /api/insights/devices/scan", s.need(RoleAdmin, s.scanDevices))
	mux.Handle("PUT /api/insights/devices", s.need(RoleAdmin, s.labelDevice))
	mux.Handle("GET /api/files/info", s.need(RoleManager, s.filesInfo))
	mux.Handle("GET /api/files/list", s.need(RoleManager, s.filesList))
	mux.Handle("GET /api/files/download", s.need(RoleManager, s.filesDownload))
	mux.Handle("GET /api/files/read", s.need(RoleManager, s.filesRead))
	mux.Handle("PUT /api/files/write", s.need(RoleManager, s.filesWrite))
	mux.Handle("POST /api/files/mkdir", s.need(RoleManager, s.filesMkdir))
	mux.Handle("POST /api/files/rename", s.need(RoleManager, s.filesRename))
	mux.Handle("POST /api/files/move", s.need(RoleManager, s.filesMove))
	mux.Handle("POST /api/files/delete", s.need(RoleManager, s.filesDelete))
	mux.Handle("POST /api/files/upload", s.need(RoleManager, s.filesUpload))
	mux.Handle("POST /api/files/access", s.need(RoleAdmin, s.filesAccess))
	mux.Handle("GET /api/cleanup", s.need(RoleAdmin, s.cleanupScan))
	mux.Handle("POST /api/cleanup/run", s.need(RoleAdmin, s.cleanupRun))
	mux.Handle("GET /api/cleanup/sizes", s.need(RoleAdmin, s.cleanupSizes))
	mux.Handle("GET /api/haven/update", s.need(RoleAdmin, s.selfStatus))
	mux.Handle("POST /api/haven/update/check", s.need(RoleAdmin, s.selfCheck))
	mux.Handle("POST /api/haven/update/apply", s.need(RoleAdmin, s.selfApply))
	mux.Handle("GET /api/users", s.need(RoleAdmin, s.listPeople))
	mux.Handle("POST /api/users", s.need(RoleAdmin, s.addPerson))
	mux.Handle("POST /api/users/transfer", s.need(RoleAdmin, s.transferAdmin))
	mux.Handle("PUT /api/users/{name}", s.need(RoleAdmin, s.updatePerson))
	mux.Handle("DELETE /api/users/{name}", s.need(RoleAdmin, s.deletePerson))
	mux.Handle("POST /api/users/{name}/password", s.need(RoleAdmin, s.resetPerson))
	mux.Handle("GET /api/ui", auth(s.getUI))
	mux.Handle("PUT /api/ui", s.need(RoleAdmin, s.putUI))
	mux.Handle("GET /api/widgets", auth(s.getWidgets))
	mux.Handle("GET /api/widgets/test", s.need(RoleAdmin, s.widgetTest))
	mux.Handle("GET /api/widgets/favicon", s.need(RoleManager, s.widgetFavicon))
	mux.Handle("GET /api/widgets/places", s.need(RoleAdmin, s.findPlaces))
	mux.Handle("POST /api/widgets/feed-check", s.need(RoleAdmin, s.checkFeed))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { jsonError(w, http.StatusNotFound, "not found") })
	mux.Handle("/", s.spa())

	return s.secure(mux)
}

// secure adds safety headers and blocks cross-site requests. Every request
// that changes something must carry the X-Haven header, which a malicious
// website cannot add to a request it sends to your server.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https: data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-Haven") != "1" {
				jsonError(w, http.StatusForbidden, "missing X-Haven header")
				return
			}
			if r.URL.Path != "/api/files/upload" {
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "please sign in")
			return
		}
		user, ok := s.sessions.Get(c.Value)
		if !ok {
			jsonError(w, http.StatusUnauthorized, "your session has expired, please sign in again")
			return
		}
		// The role is read fresh on every request, so changing or disabling
		// someone takes effect at once.
		p, found := s.store.Person(user)
		if !found || p.Disabled {
			s.sessions.Delete(c.Value)
			jsonError(w, http.StatusUnauthorized, "this account is no longer active")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, p.Username)
		ctx = context.WithValue(ctx, personKey, p)
		next(w, r.WithContext(ctx))
	})
}

const personKey ctxKey = "person"

func person(r *http.Request) User {
	p, _ := r.Context().Value(personKey).(User)
	return p
}

// allowedApp tells whether this person may see and use an app.
func allowedApp(p User, id string) bool {
	if !limitedRole(p.Role) {
		return rankOf(p.Role) >= rankOf(RoleManager)
	}
	for _, a := range p.Apps {
		if a == id {
			return true
		}
	}
	return false
}

// need wraps a handler so only people at or above a role can use it. The
// check is here, on the server, not only in the UI.
func (s *Server) need(role string, h http.HandlerFunc) http.Handler {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if rankOf(person(r).Role) < rankOf(role) {
			jsonError(w, http.StatusForbidden, "your account isn't allowed to do this")
			return
		}
		h(w, r)
	})
}

func (s *Server) spa() http.Handler {
	files := http.FileServer(http.FS(s.static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := s.static.Open(p); err == nil {
				f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		// Unknown paths get the app shell so client-side routes work on refresh.
		b, err := fs.ReadFile(s.static, "index.html")
		if err != nil {
			http.Error(w, "web UI not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(b)
	})
}

// --- helpers --------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid request body")
	}
	return nil
}

func currentUser(r *http.Request) string {
	u, _ := r.Context().Value(userKey).(string)
	return u
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, tok string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
	})
}

// --- auth handlers --------------------------------------------------------

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	res := map[string]any{"setupRequired": s.store.NeedsSetup(), "authenticated": false, "version": Version, "repo": safeRepo(UpdateRepo), "author": Author, "authorUrl": AuthorURL}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if u, ok := s.sessions.Get(c.Value); ok {
			if p, found := s.store.Person(u); found && !p.Disabled {
				res["authenticated"] = true
				res["user"] = p.Username
				res["role"] = p.Role
				res["apps"] = p.Apps
			}
		}
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if blocked, left := s.limiter.Blocked(ip); blocked {
		jsonError(w, http.StatusTooManyRequests, fmt.Sprintf("too many attempts, try again in %d seconds", int(left.Seconds())+1))
		return
	}
	var req struct{ Token, Username, Password string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.Setup(req.Token, strings.TrimSpace(req.Username), req.Password); err != nil {
		if strings.Contains(err.Error(), "setup code") {
			s.limiter.Fail(ip)
		}
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.limiter.Success(ip)
	setSessionCookie(w, r, s.sessions.Create(strings.TrimSpace(req.Username)), int(sessionTTL.Seconds()))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if blocked, left := s.limiter.Blocked(ip); blocked {
		jsonError(w, http.StatusTooManyRequests, fmt.Sprintf("too many failed attempts, try again in %d seconds", int(left.Seconds())+1))
		return
	}
	var req struct{ Username, Password string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := strings.TrimSpace(req.Username)
	if !s.store.CheckPassword(user, req.Password) {
		s.limiter.Fail(ip)
		jsonError(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	s.limiter.Success(ip)
	setSessionCookie(w, r, s.sessions.Create(user), int(sessionTTL.Seconds()))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.Delete(c.Value)
	}
	setSessionCookie(w, r, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct{ Current, New string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := currentUser(r)
	if err := s.store.ChangePassword(user, req.Current, req.New); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, _ := r.Cookie(sessionCookie)
	s.sessions.DeleteUserExcept(user, c.Value) // sign out other devices
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- TV pairing -----------------------------------------------------------

func (s *Server) tvStart(w http.ResponseWriter, r *http.Request) {
	if s.store.NeedsSetup() {
		jsonError(w, http.StatusBadRequest, "create your admin account on a phone or computer first")
		return
	}
	code, id, err := s.pairs.Start()
	if err != nil {
		jsonError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "id": id, "expiresIn": int(pairTTL.Seconds())})
}

func (s *Server) tvPoll(w http.ResponseWriter, r *http.Request) {
	var req struct{ ID string }
	if err := readJSON(r, &req); err != nil || len(req.ID) != 64 {
		jsonError(w, http.StatusBadRequest, "invalid pairing")
		return
	}
	status, user := s.pairs.Poll(req.ID)
	if status == "approved" {
		setSessionCookie(w, r, s.sessions.CreateTV(user), int(tvSessionTTL.Seconds()))
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) tvApprove(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if blocked, left := s.pairLimiter.Blocked(ip); blocked {
		jsonError(w, http.StatusTooManyRequests, fmt.Sprintf("too many wrong codes, try again in %d seconds", int(left.Seconds())+1))
		return
	}
	var req struct{ Code string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.pairs.Approve(req.Code, currentUser(r)) {
		s.pairLimiter.Fail(ip)
		jsonError(w, http.StatusBadRequest, "that code is wrong or has expired; check the code on your TV")
		return
	}
	s.pairLimiter.Success(ip)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- system & catalog -----------------------------------------------------

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.sys.Snapshot())
}

func (s *Server) listCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.catalog.All())
}

func (s *Server) preflight(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CatalogID string            `json:"catalogId"`
		ID        string            `json:"id"`
		Values    map[string]string `json:"values"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	app, ok := s.catalog.Get(req.CatalogID)
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown app")
		return
	}
	if req.ID == "" {
		req.ID = app.ID
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, map[string]any{"checks": s.apps.Preflight(ctx, app, req.ID, req.Values)})
}

// --- apps -----------------------------------------------------------------

func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	apps, other, err := s.apps.List(ctx)
	if p := person(r); limitedRole(p.Role) {
		var mine []InstalledApp
		for _, a := range apps {
			if allowedApp(p, a.ID) {
				mine = append(mine, a)
			}
		}
		apps, other = mine, nil
	}
	res := map[string]any{"apps": apps, "containers": other}
	if err != nil {
		res["dockerError"] = err.Error()
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) installApp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CatalogID string            `json:"catalogId"`
		ID        string            `json:"id"`
		Values    map[string]string `json:"values"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.checkFolders(r, req.CatalogID, req.Values); err != nil {
		jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	job, err := s.apps.Install(ctx, req.CatalogID, req.ID, req.Values)
	if err != nil {
		var ie *InstallError
		if errors.As(err, &ie) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": ie.Error(), "checks": ie.Checks})
			return
		}
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func (s *Server) installCustom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Compose string `json:"compose"`
		WebPort string `json:"webPort"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := s.apps.InstallCustom(r.Context(), strings.TrimSpace(req.ID), strings.TrimSpace(req.Name), req.Compose, strings.TrimSpace(req.WebPort))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func (s *Server) getApp(w http.ResponseWriter, r *http.Request) {
	meta, compose, env, err := s.apps.Get(r.PathValue("id"))
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	res := map[string]any{"meta": meta, "compose": compose, "env": env}
	if ca, ok := s.catalog.Get(meta.CatalogID); ok && !meta.Custom {
		res["settings"] = ca.Settings
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) reconfigure(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Values map[string]string `json:"values"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if meta, _, _, gerr := s.apps.Get(r.PathValue("id")); gerr == nil {
		if err := s.checkFolders(r, meta.CatalogID, req.Values); err != nil {
			jsonError(w, http.StatusForbidden, err.Error())
			return
		}
	}
	job, err := s.apps.Reconfigure(r.Context(), r.PathValue("id"), req.Values)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func (s *Server) appAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	if p := person(r); rankOf(p.Role) < rankOf(RoleManager) && (action != "restart" || !allowedApp(p, id)) {
		jsonError(w, http.StatusForbidden, "your account can only restart the apps shared with you")
		return
	}
	var job *Job
	var err error
	if action == "update" {
		job, err = s.apps.Update(id)
	} else {
		job, err = s.apps.Action(id, action)
	}
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func (s *Server) uninstall(w http.ResponseWriter, r *http.Request) {
	job, err := s.apps.Uninstall(r.PathValue("id"), r.URL.Query().Get("removeData") == "1")
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func tailParam(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("tail"))
	if err != nil || n <= 0 {
		return 300
	}
	if n > 5000 {
		return 5000
	}
	return n
}

func (s *Server) appLogs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	out, err := s.apps.Logs(ctx, r.PathValue("id"), tailParam(r))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": out})
}

var containerIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func (s *Server) containerAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	if !containerIDRe.MatchString(id) {
		jsonError(w, http.StatusBadRequest, "invalid container id")
		return
	}
	if action != "start" && action != "stop" && action != "restart" {
		jsonError(w, http.StatusBadRequest, "unknown action")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if _, err := s.docker.run(ctx, action, id); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) containerLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !containerIDRe.MatchString(id) {
		jsonError(w, http.StatusBadRequest, "invalid container id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	out, err := s.docker.run(ctx, "logs", "--tail", strconv.Itoa(tailParam(r)), "--timestamps", id)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": string(out)})
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.jobs.List())
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.jobs.Get(r.PathValue("id"))
	if !ok {
		jsonError(w, http.StatusNotFound, "job not found")
		return
	}
	if p := person(r); rankOf(p.Role) < rankOf(RoleManager) && !allowedApp(p, j.App) {
		jsonError(w, http.StatusForbidden, "your account isn't allowed to do this")
		return
	}
	writeJSON(w, http.StatusOK, j.View(true))
}

// --- server features: diagnosis, power, local name, remote access ---------

func (s *Server) diagnoseApp(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	d, err := s.apps.Diagnose(ctx, r.PathValue("id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) diagnoseServer(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.DiagnoseServer(ctx))
}

func (s *Server) getPower(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, PowerState(r.Context()))
}

func (s *Server) setPower(w http.ResponseWriter, r *http.Request) {
	var req struct {
		KeepAwake *bool `json:"keepAwake"`
		ScreenOff *bool `json:"screenOff"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if req.ScreenOff != nil {
		if err := SetScreenOff(*req.ScreenOff); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		_ = s.store.UpdateSettings(func(h *HostSettings) { h.ScreenOff = *req.ScreenOff })
	}
	if req.KeepAwake != nil {
		st, err := SetKeepAwake(ctx, *req.KeepAwake)
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, st)
		return
	}
	writeJSON(w, http.StatusOK, PowerState(ctx))
}

func (s *Server) getLocalName(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.localName.Status(r.Context()))
}

func (s *Server) setLocalName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Port80 bool   `json:"port80"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := s.localName.Setup(req.Name, req.Port80)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func (s *Server) setPort80(w http.ResponseWriter, r *http.Request) {
	var req struct{ On bool }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.localName.SetPort80(req.On); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.localName.Status(r.Context()))
}

func (s *Server) getRemote(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.remote.Status(r.Context()))
}

func (s *Server) remoteAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	var job *Job
	var err error
	if action == "setup" {
		job, err = s.remote.Setup()
	} else {
		job, err = s.remote.Action(r.Context(), action)
	}
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if job != nil {
		writeJSON(w, http.StatusAccepted, job.View(false))
		return
	}
	writeJSON(w, http.StatusOK, s.remote.Status(r.Context()))
}

// --- widgets & backgrounds -------------------------------------------------

func (s *Server) getUI(w http.ResponseWriter, r *http.Request) {
	u := s.store.UI()
	if limitedRole(person(r).Role) { // see getWidgets: these can name home-network addresses
		u.Widgets.Notes, u.Widgets.Links, u.Widgets.Checks = "", []QuickLink{}, []StatusCheck{}
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) putUI(w http.ResponseWriter, r *http.Request) {
	var u UISettings
	if err := readJSON(r, &u); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := cleanUI(&u); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SetUI(u); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.checks.Kick()
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) getWidgets(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	settings := s.store.UI().Widgets
	d := s.widgets.All(ctx, settings)
	switch {
	case hidden(settings, "status"):
		d.Status.Off = true
	default:
		res := s.checks.Results(settings.Checks)
		d.Status.Data = &res
	}
	// The note, links and status checks can name addresses inside the home
	// network, so only managers and the admin see them.
	if limitedRole(person(r).Role) {
		d.Settings.Notes, d.Settings.Links, d.Settings.Checks = "", []QuickLink{}, []StatusCheck{}
		d.Status = WidgetResult[CheckSummary]{Off: true}
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) findPlaces(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	places, err := s.widgets.Places(ctx, r.URL.Query().Get("q"))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, places)
}

// --- network and Wi-Fi -------------------------------------------------------

func (s *Server) getNetwork(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.network.Status(ctx))
}

func (s *Server) scanWifi(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	nets, err := s.network.Scan(ctx)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nets)
}

func (s *Server) connectWifi(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
		Hidden   bool   `json:"hidden"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := s.network.ConnectWifi(req.SSID, req.Password, req.Hidden)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func (s *Server) forgetWifi(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UUID string `json:"uuid"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.network.ForgetWifi(r.Context(), req.UUID); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) setStatic(w http.ResponseWriter, r *http.Request) {
	var req StaticIP
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := s.network.SetStatic(req)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func (s *Server) setupNetwork(w http.ResponseWriter, r *http.Request) {
	job, err := s.network.Setup()
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

// --- access log and local-name checks ---------------------------------------------

func (s *Server) getAccessLog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.access.Snapshot(200))
}

func (s *Server) getAvahiLog(w http.ResponseWriter, r *http.Request) {
	if !isLinux() || !hasCmd("journalctl") {
		writeJSON(w, http.StatusOK, map[string]string{"log": ""})
		return
	}
	out, _ := runHost(r.Context(), 8*time.Second, "journalctl", "-u", "avahi-daemon", "-n", "60", "--no-pager", "-o", "short-iso")
	writeJSON(w, http.StatusOK, map[string]string{"log": out})
}

func (s *Server) testLocalName(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.localName.Test(ctx, s.access))
}

func (s *Server) lanOnly(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	ifs, err := s.localName.AnnounceOnlyLAN(ctx)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interfaces": ifs})
}

func (s *Server) checkFeed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := s.widgets.CheckFeed(ctx, req.URL)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// --- updates and notifications ---------------------------------------------------------

func (s *Server) getUpdates(w http.ResponseWriter, r *http.Request) {
	snap := s.updates.Snapshot()
	if snap.Checked.IsZero() && !snap.Checking {
		s.updates.CheckNow() // first visit: check right away
		snap.Checking = true
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) checkUpdates(w http.ResponseWriter, r *http.Request) {
	s.updates.CheckNow()
	writeJSON(w, http.StatusAccepted, map[string]bool{"checking": true})
}

func (s *Server) applyUpdates(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs   []string `json:"ids"`
		Prune bool     `json:"prune"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := s.apps.UpdateMany(req.IDs, req.Prune)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func (s *Server) getNotifications(w http.ResponseWriter, r *http.Request) {
	items, unread := s.notes.List()
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
}

func (s *Server) readNotifications(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []uint64 `json:"ids"`
		All bool     `json:"all"`
	}
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.notes.MarkRead(req.IDs, req.All)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) clearNotifications(w http.ResponseWriter, r *http.Request) {
	var req struct {
		All bool `json:"all"`
	}
	_ = readJSON(r, &req)
	s.notes.Clear(req.All)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
