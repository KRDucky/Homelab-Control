// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package dockerbroker

import (
	"bytes"
	"encoding/json"
	"homelab-control/backend/internal/composediscovery"
	"io"
	"os"
	"regexp"
	"syscall"
)

func LoadRouting(path string) (composediscovery.Context, error) {
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return composediscovery.Context{}, ErrUnavailable
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0022 != 0 || st.Size() > 4096 {
		return composediscovery.Context{}, ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(f, 4097))
	if e != nil || len(b) > 4096 || !uniqueJSONKeys(b) {
		return composediscovery.Context{}, ErrUnavailable
	}
	var cfg struct {
		SchemaVersion int                      `json:"schemaVersion"`
		Routing       composediscovery.Context `json:"routing"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&cfg) != nil || cfg.SchemaVersion != 1 {
		return composediscovery.Context{}, ErrUnavailable
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return composediscovery.Context{}, ErrUnavailable
	}
	for i, list := range [][]string{cfg.Routing.Networks, cfg.Routing.Entrypoints, cfg.Routing.Resolvers} {
		if list == nil || len(list) > 32 {
			return composediscovery.Context{}, ErrUnavailable
		}
		seen := map[string]bool{}
		for _, v := range list {
			if !name.MatchString(v) || (i > 0 && !regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`).MatchString(v)) || seen[v] {
				return composediscovery.Context{}, ErrUnavailable
			}
			seen[v] = true
		}
	}
	return cfg.Routing, nil
}
