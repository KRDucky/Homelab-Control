// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"errors"
	"os"
	"path/filepath"
)

// Official Traefik image used by the isolated MediaMouse live stack.
const OfficialImage = "docker.io/library/traefik@sha256:f4bb143d1135a579b630ed491e17ec50f6f7c9c6a30585ff3d385471d2a86f5a"

// SetupHTTPPort is the host HTTP port for easy-setup Traefik. 80/443 stay free
// for NPM, Caddy, SWAG, or another reverse proxy.
const SetupHTTPPort = "8880"

var ErrSetup = errors.New("invalid Traefik easy-setup directory")

const officialCompose = `# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
#
# HomeControl easy-setup. Official Traefik image only. Host 80/443 stay free.
# CORE does not mount docker.sock and does not start this stack.
name: homelab-traefik
services:
  traefik:
    image: ` + OfficialImage + `
    pull_policy: missing
    container_name: homelab-traefik
    user: '0:0'
    restart: 'no'
    network_mode: bridge
    ports:
      - '` + SetupHTTPPort + `:8080'
    volumes:
      - type: bind
        source: ./traefik.yml
        target: /etc/traefik/traefik.yml
        read_only: true
      - type: bind
        source: ./dynamic
        target: /etc/traefik/dynamic
        read_only: false
      - type: bind
        source: ${DOCKER_SOCKET:-/run/user/1000/docker.sock}
        target: /var/run/docker.sock
        read_only: true
    command: [--configFile=/etc/traefik/traefik.yml]
    healthcheck:
      test: [CMD, traefik, healthcheck, --configFile=/etc/traefik/traefik.yml]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 10s
`

const officialStatic = `# SPDX-FileCopyrightText: 2026 The Homelab Control Authors
# SPDX-License-Identifier: GPL-3.0-or-later
global:
  checkNewVersion: false
  sendAnonymousUsage: false
entryPoints:
  web:
    address: ':8080'
api:
  dashboard: true
  insecure: false
ping: {}
providers:
  docker:
    endpoint: unix:///var/run/docker.sock
    exposedByDefault: false
    watch: true
  file:
    directory: /etc/traefik/dynamic
    watch: true
`

// WriteOfficialSetup writes official Traefik compose + static config. It does
// not start containers and does not bind host 80/443.
func WriteOfficialSetup(dir string) ([]string, error) {
	abs, err := confinedDir(dir)
	if err != nil {
		return nil, ErrSetup
	}
	dynamic := filepath.Join(abs, "dynamic")
	if err = os.MkdirAll(dynamic, 0755); err != nil {
		return nil, ErrSetup
	}
	wrote := []string{}
	for _, item := range []struct {
		name, body string
	}{
		{"compose.yaml", officialCompose},
		{"traefik.yml", officialStatic},
	} {
		if err = writeSetupFile(abs, item.name, item.body); err != nil {
			return nil, err
		}
		wrote = append(wrote, item.name)
	}
	return wrote, nil
}

func writeSetupFile(dir, name, body string) error {
	dest := filepath.Join(dir, name)
	if filepath.Dir(dest) != dir {
		return ErrSetup
	}
	tmp, err := os.CreateTemp(dir, name+".*.tmp")
	if err != nil {
		return ErrSetup
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.WriteString(body); err != nil {
		return ErrSetup
	}
	if err = tmp.Chmod(0644); err != nil {
		return ErrSetup
	}
	if err = tmp.Close(); err != nil {
		return ErrSetup
	}
	if err = os.Rename(tmpName, dest); err != nil {
		return ErrSetup
	}
	ok = true
	return nil
}
