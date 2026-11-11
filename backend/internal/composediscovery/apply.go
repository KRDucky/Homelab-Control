// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"errors"
	"homelab-control/backend/internal/traefikclient"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var applyName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// WriteYAML writes CORE-generated file-provider YAML as <name>.yml under dir.
// dynamic.yml is reserved for the Traefik overlay. The path cannot escape dir.
func WriteYAML(dir, name, yaml string) (string, error) {
	if yaml == "" || !applyName.MatchString(name) || name == "dynamic" {
		return "", errors.New("invalid Traefik file apply")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", errors.New("invalid Traefik file apply")
	}
	filename := name + ".yml"
	dest := filepath.Join(abs, filename)
	if filepath.Dir(dest) != abs {
		return "", errors.New("invalid Traefik file apply")
	}
	tmp, err := os.CreateTemp(abs, name+".*.yml.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.WriteString(yaml); err != nil {
		return "", err
	}
	if err = tmp.Chmod(0644); err != nil {
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(tmpName, dest); err != nil {
		return "", err
	}
	ok = true
	return filename, nil
}

// FileApplyReady is true when the only remaining blockers are Docker-provider
// absence or an update of the same file-provider router/hostname.
func FileApplyReady(blockers []Issue, name, host string, inventory traefikclient.Inventory) bool {
	if name == "" || host == "" {
		return false
	}
	for _, b := range blockers {
		switch b.Code {
		case "provider_disabled":
			continue
		case "router_duplicate":
			if fileRouter(inventory, name) {
				continue
			}
			return false
		case "hostname_duplicate":
			if fileHostOwned(inventory, name, host) {
				continue
			}
			return false
		default:
			return false
		}
	}
	return true
}

func fileRouter(inventory traefikclient.Inventory, name string) bool {
	for _, r := range inventory.Routers {
		if r.Provider == "file" && strings.TrimSuffix(r.Name, "@"+r.Provider) == name {
			return true
		}
	}
	return false
}

func fileHostOwned(inventory traefikclient.Inventory, name, host string) bool {
	owned := false
	for _, r := range inventory.Routers {
		if !containsHost(traefikclient.ComparableHosts(r.Rule), host) {
			continue
		}
		if r.Provider != "file" || strings.TrimSuffix(r.Name, "@"+r.Provider) != name {
			return false
		}
		owned = true
	}
	return owned
}

func containsHost(hosts []string, host string) bool {
	for _, h := range hosts {
		if h == host {
			return true
		}
	}
	return false
}
