package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const backupFormat = 1

type BackupItem struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Volume string `json:"volume,omitempty"`
}

type BackupPlan struct {
	Items   []BackupItem `json:"items"`
	Omitted []string     `json:"omitted"`
	Images  []string     `json:"images"`
}

type BackupManifest struct {
	Format  int       `json:"format"`
	AppID   string    `json:"appId"`
	AppName string    `json:"appName"`
	Created time.Time `json:"created"`
	BackupPlan
}

type backupCompose struct {
	Services map[string]struct {
		Image   string
		Volumes []struct {
			Type     string
			Source   string
			Target   string
			ReadOnly bool `json:"read_only"`
		}
	}
	Volumes map[string]struct {
		Name       string
		External   bool
		Driver     string
		DriverOpts map[string]string `json:"driver_opts"`
	}
	Configs map[string]struct{ File string }
	Secrets map[string]struct{ File string }
}

type backupVolume struct {
	Name       string
	Mountpoint string
	Driver     string
	Scope      string
	Options    map[string]string
	Labels     map[string]string
}

var backupVolumeKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)

func directoryPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("enter an absolute server folder path")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a folder", path)
	}
	return real, nil
}

func (m *Manager) backupConfig(ctx context.Context, id, dir string) (backupCompose, error) {
	var cfg backupCompose
	out, err := m.docker.run(ctx, composeAt(id, dir, "config", "--format", "json")...)
	if err != nil {
		return cfg, fmt.Errorf("could not read the app's Compose configuration: %w", err)
	}
	return cfg, json.Unmarshal(out, &cfg)
}

func (m *Manager) backupVolume(ctx context.Context, volume, project, key string) (backupVolume, error) {
	var v backupVolume
	out, err := m.docker.run(ctx, "volume", "inspect", "--format", "{{json .}}", "--", volume)
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return v, err
	}
	if v.Name != volume || v.Driver != "local" || v.Scope != "local" || len(v.Options) != 0 ||
		v.Labels["com.docker.compose.project"] != project || v.Labels["com.docker.compose.volume"] != key {
		return v, errors.New("volume is shared, uses a custom driver, or is managed outside this app")
	}
	if !filepath.IsAbs(v.Mountpoint) {
		return v, errors.New("Docker did not report a local volume folder")
	}
	v.Mountpoint, err = directoryPath(v.Mountpoint)
	if err != nil {
		return v, fmt.Errorf("the Docker volume folder is not accessible from Haven: %w", err)
	}
	return v, nil
}

func (m *Manager) externalBackupFolder(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == "/" || within(m.dir, path) || within(path, m.dir) {
		return false
	}
	// System files and Docker's own storage are outside an app's backup scope.
	for _, root := range []string{"/etc", "/dev", "/proc", "/sys", "/run", "/boot", "/usr", "/var/lib", "/var/run", "/var/log", "/root"} {
		if within(root, path) || within(path, root) {
			return false
		}
	}
	return true
}

func (m *Manager) planBackup(ctx context.Context, id string) (BackupPlan, error) {
	plan := BackupPlan{Items: []BackupItem{}, Omitted: []string{}, Images: []string{}}
	if !appIDRe.MatchString(id) {
		return plan, errors.New("app not found")
	}
	if _, err := m.readMeta(id); err != nil {
		return plan, errors.New("app not found")
	}
	base, err := directoryPath(m.dir)
	if err != nil {
		return plan, err
	}
	configDir, err := directoryPath(m.appDir(id))
	if err != nil {
		return plan, err
	}
	if configDir != filepath.Join(base, "apps", id) {
		return plan, errors.New("Haven's app configuration points outside its expected location")
	}
	plan.Items = append(plan.Items, BackupItem{Kind: "config", Name: "Compose configuration", Path: configDir})
	dataDir, err := directoryPath(m.dataDir(id))
	if err == nil {
		if dataDir != filepath.Join(base, "appdata", id) {
			return plan, errors.New("Haven's app data folder points outside its expected location")
		}
		plan.Items = append(plan.Items, BackupItem{Kind: "data", Name: "Haven app data", Path: dataDir})
	} else if !errors.Is(err, os.ErrNotExist) {
		return plan, err
	}
	cfg, err := m.backupConfig(ctx, id, configDir)
	if err != nil {
		return plan, err
	}

	folders := map[string]bool{}
	volumes := map[string]bool{}
	images := map[string]bool{}
	omitted := map[string]bool{}
	managed := func(path string) bool {
		return within(m.appDir(id), path) || within(m.dataDir(id), path) ||
			within(configDir, path) || within(filepath.Join(base, "appdata", id), path)
	}
	for name, config := range cfg.Configs {
		if !managed(config.File) {
			omitted["Compose config "+name] = true
		}
	}
	for name, secret := range cfg.Secrets {
		if !managed(secret.File) {
			omitted["Compose secret "+name] = true
		}
	}
	for name, svc := range cfg.Services {
		if svc.Image != "" {
			images[svc.Image] = true
		}
		for _, mount := range svc.Volumes {
			switch mount.Type {
			case "bind":
				path := filepath.Clean(mount.Source)
				if managed(path) {
					continue
				}
				real, err := directoryPath(path)
				if !mount.ReadOnly && err == nil && m.externalBackupFolder(path) && m.externalBackupFolder(real) && path == real {
					folders[path] = true
				} else {
					omitted[fmt.Sprintf("Host mount %s (%s)", path, name)] = true
				}
			case "volume":
				if mount.Source == "" {
					omitted[fmt.Sprintf("Anonymous volume at %s (%s)", mount.Target, name)] = true
				} else {
					volumes[mount.Source] = true
				}
			default:
				omitted[fmt.Sprintf("%s mount at %s (%s)", mount.Type, mount.Target, name)] = true
			}
		}
	}
	for _, key := range sortedBackupKeys(volumes) {
		volume, ok := cfg.Volumes[key]
		if !ok || volume.External || !backupVolumeKey.MatchString(key) || !backupVolumeKey.MatchString(volume.Name) {
			omitted["External or unsupported volume "+key] = true
			continue
		}
		v, err := m.backupVolume(ctx, volume.Name, projectName(id), key)
		if err != nil {
			omitted[fmt.Sprintf("Volume %s (%v)", key, err)] = true
			continue
		}
		plan.Items = append(plan.Items, BackupItem{Kind: "volume", Name: key, Path: v.Mountpoint, Volume: v.Name})
	}
	var outerFolders []string
	for _, folder := range sortedBackupKeys(folders) {
		covered := false
		for _, root := range outerFolders {
			if within(root, folder) {
				covered = true
				break
			}
		}
		if !covered {
			outerFolders = append(outerFolders, folder)
			plan.Items = append(plan.Items, BackupItem{Kind: "folder", Name: folder, Path: folder})
		}
	}
	plan.Images = sortedBackupKeys(images)
	plan.Omitted = sortedBackupKeys(omitted)
	return plan, nil
}

func sortedBackupKeys(keys map[string]bool) []string {
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (m *Manager) startBackup(id, destination string, folders []string) (*Job, error) {
	if !appIDRe.MatchString(id) || !exists(filepath.Join(m.appDir(id), "haven.json")) {
		return nil, errors.New("app not found")
	}
	dest, err := directoryPath(destination)
	if err != nil {
		return nil, err
	}
	if err := m.lock(id, "backup"); err != nil {
		return nil, err
	}
	return m.jobs.Start(id, "backup", 2*time.Hour, func(ctx context.Context, j *Job) error {
		defer m.unlock(id)
		return m.backupApp(ctx, j, id, dest, folders)
	}), nil
}

func (m *Manager) backupApp(ctx context.Context, j *Job, id, dest string, folders []string) (result error) {
	defer func() { m.recordBackupResult(id, result) }()
	meta, err := m.readMeta(id)
	if err != nil {
		return err
	}
	plan, err := m.planBackup(ctx, id)
	if err != nil {
		return err
	}
	requested := map[string]bool{}
	for _, folder := range folders {
		requested[folder] = true
	}
	manifest := BackupManifest{Format: backupFormat, AppID: id, AppName: meta.Name, Created: time.Now().UTC(), BackupPlan: plan}
	manifest.Items = make([]BackupItem, 0, len(plan.Items))
	for _, item := range plan.Items {
		if item.Kind == "folder" && !requested[item.Path] {
			manifest.Omitted = append(manifest.Omitted, "Host folder "+item.Path)
			continue
		}
		if within(item.Path, dest) {
			return fmt.Errorf("choose a backup destination outside %s", item.Path)
		}
		delete(requested, item.Path)
		manifest.Items = append(manifest.Items, item)
	}
	if len(requested) != 0 {
		return errors.New("the selected host folders changed. Review the backup contents and try again")
	}
	for _, item := range manifest.Omitted {
		j.Logf("Excluded from backup: %s", item)
	}
	staging, err := os.MkdirTemp(dest, ".haven-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	containers, err := m.docker.ListContainers(ctx)
	if err != nil {
		return err
	}
	var resume []string
	for _, c := range containers {
		if c.Project != projectName(id) {
			continue
		}
		switch c.State {
		case "paused":
			return errors.New("unpause the app's containers before backing it up")
		case "running", "restarting":
			resume = append(resume, c.ID)
		}
	}
	if len(resume) > 0 {
		defer func() {
			restartCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			j.Logf("Restarting the containers that were running")
			if err := m.docker.stream(restartCtx, j, append([]string{"start"}, resume...)...); err != nil {
				result = errors.Join(result, fmt.Errorf("could not restart the original containers: %w", err))
			}
		}()
		j.Logf("Stopping the app while its files are copied")
		if err := m.docker.stream(ctx, j, append([]string{"stop"}, resume...)...); err != nil {
			return fmt.Errorf("could not stop the app for backup: %w", err)
		}
		exitCodes, err := m.docker.run(ctx, append([]string{"inspect", "--format", "{{.State.ExitCode}}"}, resume...)...)
		if err != nil {
			return fmt.Errorf("could not verify that the app stopped cleanly: %w", err)
		}
		codes := strings.Fields(string(exitCodes))
		if len(codes) != len(resume) {
			return errors.New("could not verify the shutdown result for each container")
		}
		for i, code := range codes {
			if code == "137" {
				return fmt.Errorf("container %s was killed during shutdown. Increase its stop grace period before backing it up", resume[i])
			}
		}
	}

	for i, item := range manifest.Items {
		if item.Kind == "volume" {
			out, err := m.docker.run(ctx, "ps", "-q", "--filter", "volume="+item.Volume)
			if err != nil {
				return err
			}
			if len(strings.Fields(string(out))) != 0 {
				return fmt.Errorf("volume %s is still in use by a running container", item.Name)
			}
		}
		j.Logf("Copying %s", item.Name)
		if err := copyBackupTree(ctx, item.Path, filepath.Join(staging, strconv.Itoa(i))); err != nil {
			return fmt.Errorf("could not copy %s: %w", item.Name, err)
		}
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(staging, "manifest.json"), b, 0o600); err != nil {
		return err
	}
	final := filepath.Join(dest, id+"-"+manifest.Created.Format("20060102-150405")+"-"+j.ID+".haven-backup")
	if err := os.Rename(staging, final); err != nil {
		return err
	}
	j.Logf("Backup saved to %s", final)
	return nil
}

func readBackupManifest(path string) (BackupManifest, error) {
	var b BackupManifest
	root, err := os.OpenRoot(path)
	if err != nil {
		return b, err
	}
	defer root.Close()
	info, err := root.Lstat("manifest.json")
	if err != nil {
		return b, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return b, errors.New("invalid backup manifest")
	}
	data, err := root.ReadFile("manifest.json")
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return b, fmt.Errorf("invalid backup manifest: %w", err)
	}
	if b.Format != backupFormat || !appIDRe.MatchString(b.AppID) || len(b.Items) == 0 || b.Items[0].Kind != "config" {
		return b, errors.New("this is not a supported Haven app backup")
	}
	seen := map[BackupItem]bool{}
	for i, item := range b.Items {
		switch item.Kind {
		case "config":
			if i != 0 {
				return b, errors.New("duplicate Compose configuration in the backup")
			}
		case "data":
			if i != 1 {
				return b, errors.New("invalid app data entry in the backup")
			}
		case "volume":
			if !backupVolumeKey.MatchString(item.Name) {
				return b, errors.New("invalid Docker volume entry in the backup")
			}
		case "folder":
			if !filepath.IsAbs(item.Path) || filepath.Clean(item.Path) != item.Path {
				return b, errors.New("invalid host folder entry in the backup")
			}
		default:
			return b, errors.New("unknown storage entry in the backup")
		}
		key := BackupItem{Kind: item.Kind, Name: item.Name}
		if item.Kind == "folder" {
			key = BackupItem{Kind: item.Kind, Path: item.Path}
		}
		if seen[key] {
			return b, errors.New("duplicate storage entry in the backup")
		}
		seen[key] = true
		part, err := root.Lstat(strconv.Itoa(i))
		if err != nil || !part.IsDir() {
			return b, fmt.Errorf("backup folder for %s is missing", item.Name)
		}
	}
	return b, nil
}

func (m *Manager) startRestore(path, expectedID string, restoreFolders bool) (*Job, error) {
	source, err := directoryPath(path)
	if err != nil {
		return nil, err
	}
	b, err := readBackupManifest(source)
	if err != nil {
		return nil, err
	}
	if b.AppID != expectedID {
		return nil, errors.New("the backup changed after it was previewed")
	}
	if err := m.lock(b.AppID, "restore"); err != nil {
		return nil, err
	}
	return m.jobs.Start(b.AppID, "restore", 2*time.Hour, func(ctx context.Context, j *Job) error {
		defer m.unlock(b.AppID)
		return m.restoreApp(ctx, j, source, b, restoreFolders)
	}), nil
}

func unusedRestorePath(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%s already exists. Restore needs a new app and unused storage", path)
}

func (m *Manager) restoreApp(ctx context.Context, j *Job, source string, b BackupManifest, restoreFolders bool) (result error) {
	id := b.AppID
	if err := unusedRestorePath(m.appDir(id)); err != nil {
		return err
	}
	if err := unusedRestorePath(m.dataDir(id)); err != nil {
		return err
	}
	containers, err := m.docker.run(ctx, "ps", "-aq", "--filter", "label=com.docker.compose.project="+projectName(id))
	if err != nil {
		return err
	}
	if len(strings.Fields(string(containers))) != 0 {
		return fmt.Errorf("Docker project %s already has containers. Restore needs an unused project", projectName(id))
	}
	for _, item := range b.Items {
		if item.Kind != "folder" || !restoreFolders {
			continue
		}
		parent, err := directoryPath(filepath.Dir(item.Path))
		if err != nil {
			return err
		}
		if filepath.Join(parent, filepath.Base(item.Path)) != item.Path || !m.externalBackupFolder(item.Path) {
			return fmt.Errorf("cannot restore host folder %s to this server", item.Path)
		}
		if err := unusedRestorePath(item.Path); err != nil {
			return err
		}
	}

	var createdFolders, createdVolumes []string
	defer func() {
		if result == nil {
			return
		}
		for i := len(createdFolders) - 1; i >= 0; i-- {
			result = errors.Join(result, os.RemoveAll(createdFolders[i]))
		}
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, volume := range createdVolumes {
			if _, err := m.docker.run(cleanup, "volume", "rm", "--", volume); err != nil {
				result = errors.Join(result, fmt.Errorf("could not clean up volume %s: %w", volume, err))
			}
		}
	}()

	// Keep the app outside the managed app list until all of its files are in place.
	staging, err := os.MkdirTemp(filepath.Dir(m.appDir(id)), ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := copyBackupTree(ctx, filepath.Join(source, "0"), staging); err != nil {
		return err
	}
	for _, name := range []string{"compose.yml", ".env", "haven.json"} {
		info, err := os.Lstat(filepath.Join(staging, name))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("backup is missing a regular %s file", name)
		}
	}
	rawMeta, err := os.ReadFile(filepath.Join(staging, "haven.json"))
	if err != nil {
		return err
	}
	var meta AppMeta
	if err := json.Unmarshal(rawMeta, &meta); err != nil || meta.ID != id {
		return errors.New("app metadata does not match the backup")
	}
	envPath := filepath.Join(staging, ".env")
	env, err := readEnv(envPath)
	if err != nil {
		return err
	}
	env.set("APP_DATA", m.dataDir(id))
	if err := os.Remove(envPath + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := env.write(envPath); err != nil {
		return err
	}
	cfg, err := m.backupConfig(ctx, id, staging)
	if err != nil {
		return err
	}
	if err := os.Mkdir(m.dataDir(id), 0o755); err != nil {
		return err
	}
	createdFolders = append(createdFolders, m.dataDir(id))
	for i, item := range b.Items[1:] {
		var target string
		switch item.Kind {
		case "data":
			target = m.dataDir(id)
		case "folder":
			if !restoreFolders {
				j.Logf("Skipped host folder %s", item.Path)
				continue
			}
			if err := os.Mkdir(item.Path, 0o700); err != nil {
				return err
			}
			createdFolders = append(createdFolders, item.Path)
			target = item.Path
		case "volume":
			volume, ok := cfg.Volumes[item.Name]
			if !ok || volume.External || (volume.Driver != "" && volume.Driver != "local") ||
				len(volume.DriverOpts) != 0 || !backupVolumeKey.MatchString(volume.Name) {
				return fmt.Errorf("Compose does not define a restorable volume for %s", item.Name)
			}
			out, err := m.docker.run(ctx, "volume", "ls", "--format", "{{.Name}}")
			if err != nil {
				return err
			}
			for _, name := range strings.Fields(string(out)) {
				if name == volume.Name {
					return fmt.Errorf("volume %s already exists. Restore needs unused storage", name)
				}
			}
			if _, err := m.docker.run(ctx, "volume", "create", "--label", "com.docker.compose.project="+projectName(id),
				"--label", "com.docker.compose.volume="+item.Name, "--label", "com.haven.backup.restore="+j.ID, "--", volume.Name); err != nil {
				return err
			}
			v, err := m.backupVolume(ctx, volume.Name, projectName(id), item.Name)
			if v.Labels["com.haven.backup.restore"] != j.ID {
				if err != nil {
					return fmt.Errorf("could not verify the new volume %s: %w", volume.Name, err)
				}
				return fmt.Errorf("volume %s was created outside this restore", volume.Name)
			}
			createdVolumes = append(createdVolumes, volume.Name)
			if err != nil {
				return err
			}
			target = v.Mountpoint
		}
		j.Logf("Restoring %s", item.Name)
		if err := copyBackupTree(ctx, filepath.Join(source, strconv.Itoa(i+1)), target); err != nil {
			return fmt.Errorf("could not restore %s: %w", item.Name, err)
		}
	}
	if err := unusedRestorePath(m.appDir(id)); err != nil {
		return err
	}
	if err := os.Rename(staging, m.appDir(id)); err != nil {
		return err
	}
	m.usageChanged(id, false)
	j.Logf("Restored %s. Review its settings and start it from Apps", meta.Name)
	return nil
}

// Copy into a private, empty destination. Roots constrain path traversal and
// links are copied as links, never followed into another storage location.
func copyBackupTree(ctx context.Context, source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a plain directory", source)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	src, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer dst.Close()

	type dirInfo struct {
		name string
		info fs.FileInfo
	}
	var dirs []dirInfo
	err = fs.WalkDir(src.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.FromSlash(path)
		info, err := src.Lstat(name)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if path != "." {
				if err := dst.Mkdir(name, 0o700); err != nil {
					return err
				}
			}
			dirs = append(dirs, dirInfo{name, info})
			return nil
		case info.Mode().IsRegular():
			in, err := src.Open(name)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := dst.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, backupReader{ctx: ctx, Reader: in})
			if err == nil {
				err = out.Sync()
			}
			if err := errors.Join(err, out.Close()); err != nil {
				return err
			}
			return preserveBackupMetadata(dst, name, info)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := src.Readlink(name)
			if err != nil {
				return err
			}
			return dst.Symlink(link, name)
		case info.Mode()&os.ModeSocket != 0:
			return nil // Runtime socket; the app recreates it on start.
		default:
			return fmt.Errorf("unsupported file type at %s", name)
		}
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := preserveBackupMetadata(dst, dirs[i].name, dirs[i].info); err != nil {
			return err
		}
	}
	return nil
}

type backupReader struct {
	ctx context.Context
	io.Reader
}

func (r backupReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func preserveBackupMetadata(root *os.Root, name string, info fs.FileInfo) error {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
		if err := root.Chown(name, int(stat.Uid), int(stat.Gid)); err != nil {
			return err
		}
	}
	if err := root.Chmod(name, info.Mode()&(fs.ModePerm|fs.ModeSetgid|fs.ModeSticky)); err != nil {
		return err
	}
	return root.Chtimes(name, info.ModTime(), info.ModTime())
}
