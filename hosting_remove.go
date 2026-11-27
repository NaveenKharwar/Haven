package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type hostingHostRunner func(context.Context, *Job, ...string) error

func runHostingHostCommand(ctx context.Context, j *Job, args ...string) error {
	if len(args) == 0 {
		return errors.New("No host command was provided.")
	}
	return hostStream(ctx, j, nil, args[0], args[1:]...)
}

func (h *Hosting) disconnectCoolify(st hostingState, keys hostingSecrets) error {
	if len(st.Sites) > 0 {
		return errors.New("Remove every Haven-managed website before disconnecting Coolify.")
	}
	st.Coolify = hostingConnection{}
	keys.Coolify, keys.CoolifyOrigin = "", ""
	if err := h.write("secrets.json", keys); err != nil {
		return err
	}
	return h.save(st)
}

func (h *Hosting) removeSite(ctx context.Context, j *Job, st hostingState, keys hostingSecrets, site *hostingSite) error {
	if site == nil || !hostingID.MatchString(site.ID) {
		return errors.New("This website has no valid Haven record to remove.")
	}
	if site.Publication != nil {
		return errors.New("Unpublish this website before removing it.")
	}
	if st.Coolify.URL == "" || keys.Coolify == "" {
		return errors.New("Connect Coolify before removing this website.")
	}
	client := h.coolify(st, keys)
	j.Logf("Removing %s and its service data from Coolify.", site.Name)
	if err := client.remove(ctx, *site); err != nil {
		return err
	}
	for i := range st.Sites {
		if st.Sites[i].ID == site.ID {
			st.Sites = append(st.Sites[:i], st.Sites[i+1:]...)
			break
		}
	}
	if err := h.save(st); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Dir(h.staticSitePath(site.ID))); err != nil {
		j.Logf("The Coolify service was removed, but Haven could not remove its saved local source: %v", err)
	}
	return nil
}

func coolifyComposeArgs(root string) ([]string, error) {
	clean := filepath.Clean(root)
	if root != clean || !filepath.IsAbs(clean) || clean == string(filepath.Separator) || filepath.Base(clean) != "coolify" {
		return nil, errors.New("Coolify uninstall received an unsafe data path.")
	}
	rootInfo, err := os.Lstat(clean)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Coolify uninstall needs a real Coolify data directory.")
	}
	source := filepath.Join(root, "source")
	sourceInfo, err := os.Lstat(source)
	if err != nil || !sourceInfo.IsDir() || sourceInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Coolify uninstall needs its source directory.")
	}
	env := filepath.Join(source, ".env")
	base := filepath.Join(source, "docker-compose.yml")
	prod := filepath.Join(source, "docker-compose.prod.yml")
	for _, path := range []string{env, base, prod} {
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("Coolify uninstall needs %s", path)
		}
	}
	args := []string{"docker", "compose", "--env-file", env, "-f", base, "-f", prod}
	for _, name := range []string{"docker-compose.custom.yml", "docker-compose.postgres-upgrade.yml"} {
		path := filepath.Join(source, name)
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			args = append(args, "-f", path)
		}
	}
	return append(args, "down", "--volumes", "--remove-orphans"), nil
}

func (h *Hosting) uninstallCoolify(ctx context.Context, j *Job, st hostingState, keys hostingSecrets, root string, run hostingHostRunner) error {
	if len(st.Sites) > 0 {
		return errors.New("Remove every Haven-managed website before uninstalling Coolify.")
	}
	if st.Coolify.URL == "" || keys.Coolify == "" {
		return errors.New("Connect Coolify before uninstalling it so Haven can verify that no resources remain.")
	}
	client := h.coolify(st, keys)
	count, err := client.resourceCount(ctx)
	if err != nil {
		return fmt.Errorf("verify Coolify resources: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("Coolify still manages %d resource(s). Remove them before uninstalling Coolify", count)
	}
	args, err := coolifyComposeArgs(root)
	if err != nil {
		return err
	}
	j.Logf("Stopping Coolify and permanently removing its control data and Docker volumes.")
	if err := run(ctx, j, args...); err != nil {
		return fmt.Errorf("stop Coolify: %w", err)
	}
	j.Logf("Removing Coolify's shared Docker network.")
	if err := run(ctx, j, "docker", "network", "rm", "coolify"); err != nil {
		return fmt.Errorf("remove Coolify network: %w", err)
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove Coolify data: %w", err)
	}
	return h.disconnectCoolify(st, keys)
}
