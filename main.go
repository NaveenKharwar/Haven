package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const AppName = "Haven"

// Version is overridden at build time with -ldflags "-X main.Version=...".
var Version = "0.1.0-dev"

//go:embed all:web/dist
var webFS embed.FS

func main() {
	addr := flag.String("addr", ":8484", "address to listen on")
	dataDir := flag.String("data", "/opt/haven", "directory for Haven's state and apps")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("haven %s\n", Version)
		return
	}

	dir, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("bad data dir: %v", err)
	}
	for _, sub := range []string{"apps", "appdata"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			log.Fatalf("cannot create data dir: %v", err)
		}
	}

	// After a self-update, a new version that keeps failing is rolled back here.
	if StartupGuard(dir) {
		os.Exit(1)
	}

	store, err := LoadStore(dir)
	if err != nil {
		log.Fatalf("cannot load state: %v", err)
	}
	catalog, err := LoadCatalog()
	if err != nil {
		log.Fatalf("app catalog is invalid: %v", err)
	}

	docker := &Docker{}
	if v, err := docker.Version(context.Background()); err != nil {
		log.Printf("WARNING: Docker is not reachable: %v", err)
	} else {
		log.Printf("Docker %s detected", v)
	}

	if store.NeedsSetup() {
		token, err := store.EnsureSetupToken()
		if err != nil {
			log.Fatalf("cannot create setup token: %v", err)
		}
		log.Printf("First run: open the web UI and use this setup code: %s", token)
		log.Printf("(also saved in %s)", filepath.Join(dir, "setup-token"))
	}

	sys := NewSystemMonitor(dir, docker)
	go sys.Run(context.Background())

	jobs := NewJobsAt(filepath.Join(dir, "jobs.json"))
	apps := NewManager(dir, docker, catalog, jobs)
	apps.usage = NewUsageMonitor(dir, docker)
	go apps.usage.Run(context.Background())

	dist, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	sessions := NewSessions(filepath.Join(dir, "sessions.json"))
	srv := NewServer(store, sessions, apps, catalog, sys, jobs, docker, dist)
	_, port, _ := net.SplitHostPort(*addr)
	if port == "" {
		port = "8484"
	}
	srv.port80 = &ExtraListener{addr: ":80"}
	srv.localName = &LocalName{store: store, jobs: jobs, port80: srv.port80, port: port}
	srv.remote = &Remote{jobs: jobs, port: port, port80: srv.port80}
	srv.widgets = NewWidgets()
	srv.network = &Network{jobs: jobs}
	srv.files = NewFiles(store, dir)
	srv.cleaner = NewCleaner(docker, jobs)
	srv.notes = NewNotifier(filepath.Join(dir, "notifications.json"))
	srv.notes.NoteVersion(Version)
	srv.checks = NewStatusChecker(store, srv.notes.Add)
	go srv.checks.Run(context.Background())
	jobs.OnDone = srv.notes.jobDone
	srv.limiter.OnLock = func(ip string) {
		srv.notes.Add("warn", "Too many failed sign-in attempts", "Someone at "+ip+" entered a wrong password 5 times and is blocked for 5 minutes.", fmt.Sprintf("lock:%s:%d", ip, time.Now().Unix()/3600), "")
	}
	srv.self = NewSelfUpdate(dir, jobs, srv.notes.Add)
	go srv.self.Run(context.Background())
	go ConfirmUpdate(dir, nil) // the "Haven was updated" note comes from NoteVersion
	srv.updates = NewUpdateChecker(apps)
	srv.updates.notify = srv.notes.Add
	apps.onUpdated = srv.updates.Recheck
	go srv.updates.Run(context.Background())
	go NewWatcher(srv.notes, apps, sys, srv.remote).Run(context.Background())
	srv.access = NewAccessLog()
	go srv.access.Run()
	srv.devices = NewDevices(dir, srv.access, srv.notes.Add)
	go srv.devices.Watch(context.Background())
	routes := srv.Routes()
	handler := srv.access.Wrap(routes, port)
	srv.port80.handler = srv.access.Wrap(routes, "80")
	srv.port80.conn = srv.access.ConnState("80")

	// Re-apply server options chosen in the UI.
	settings := store.Settings()
	if settings.Port80 {
		if err := srv.port80.Set(true); err != nil {
			log.Printf("could not listen on port 80: %v", err)
		}
	}
	if settings.ScreenOff && isLinux() {
		if err := SetScreenOff(true); err != nil {
			log.Printf("could not turn the screen off: %v", err)
		}
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ConnState:         srv.access.ConnState(port),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("%s %s listening on %s (data: %s)", AppName, Version, *addr, dir)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Printf("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}
