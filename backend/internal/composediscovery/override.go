// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrComposeOverrideUnmanaged means the Compose override file that Docker
// Compose would load was not written by Homelab Control. It is never changed.
var ErrComposeOverrideUnmanaged = errors.New("compose override file is not managed by Homelab Control")

// overrideNames is Docker Compose's own override search order: without -f,
// Compose merges the first of these that exists in the project directory.
var overrideNames = []string{
	"compose.override.yml",
	"compose.override.yaml",
	"docker-compose.override.yml",
	"docker-compose.override.yaml",
}

const overrideHeader = "# Managed by Homelab Control: Traefik labels for this Compose project.\n" +
	"# Generated from the state line below; edits to this file are replaced on the next apply.\n" +
	"# Docker Compose merges it with the project's Compose file when run without -f.\n"

// overrideState is the first line after the header: the labels per service as
// JSON. The YAML below it is generated from this line and never parsed back.
const overrideState = "# homelab-control-labels: "

// LabelsOverridePath returns the override file Docker Compose would merge for
// the Compose file at composePath, and whether it already exists. A file that
// exists and is not Homelab Control's own is ErrComposeOverrideUnmanaged.
func LabelsOverridePath(composePath string) (string, bool, error) {
	dir := filepath.Dir(composePath)
	for _, n := range overrideNames {
		p := filepath.Join(dir, n)
		fi, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !fi.Mode().IsRegular() {
			return "", false, ErrComposeOverrideUnmanaged
		}
		if _, err := readOverrideState(p); err != nil {
			return "", false, err
		}
		return p, true, nil
	}
	return filepath.Join(dir, overrideFor(filepath.Base(composePath))), false, nil
}

// overrideFor names a new override after the Compose file's own style.
func overrideFor(base string) string {
	stem, ext := strings.TrimSuffix(base, filepath.Ext(base)), filepath.Ext(base)
	return stem + ".override" + ext
}

func readOverrideState(path string) (map[string]map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > maxComposeBytes {
		return nil, ErrComposeOverrideUnmanaged
	}
	text := string(raw)
	if !strings.HasPrefix(text, overrideHeader+overrideState) {
		return nil, ErrComposeOverrideUnmanaged
	}
	line, _, _ := strings.Cut(strings.TrimPrefix(text, overrideHeader+overrideState), "\n")
	state := map[string]map[string]string{}
	if json.Unmarshal([]byte(line), &state) != nil {
		return nil, ErrComposeOverrideUnmanaged
	}
	// The YAML is generated from this state, so it must pass the same checks
	// as a new apply; anything else is treated as not ours.
	for svc, labels := range state {
		if !dockerName.MatchString(svc) || len(labels) == 0 || len(SafeLabels(labels)) != len(labels) {
			return nil, ErrComposeOverrideUnmanaged
		}
	}
	return state, nil
}

// WriteLabelsOverride writes SafeLabels for service into the Compose override
// file next to composePath instead of editing the Compose file. The Compose
// file is only read: a label it already sets to another value is
// ErrComposeLabelConflict, as with the in-place patch. It returns the override
// file's name and how many labels were new or changed.
func WriteLabelsOverride(composePath, service string, labels map[string]string) (string, int, error) {
	raw, err := os.ReadFile(composePath)
	if err != nil {
		return "", 0, ErrComposeApply
	}
	// The in-memory patch is the conflict check against the Compose file; its
	// output is discarded and the Compose file is not written.
	if _, _, err := PatchLabelsText(string(raw), service, labels); err != nil {
		return "", 0, err
	}
	path, exists, err := LabelsOverridePath(composePath)
	if err != nil {
		return "", 0, err
	}
	state := map[string]map[string]string{}
	if exists {
		if state, err = readOverrideState(path); err != nil {
			return "", 0, err
		}
	}
	if state[service] == nil {
		state[service] = map[string]string{}
	}
	changed := 0
	for k, v := range SafeLabels(labels) {
		if old, ok := state[service][k]; !ok || old != v {
			changed++
		}
		state[service][k] = v
	}
	if exists && changed == 0 {
		return filepath.Base(path), 0, nil
	}
	if err := writeAtomic(path, renderOverride(state)); err != nil {
		return "", 0, err
	}
	return filepath.Base(path), changed, nil
}

func renderOverride(state map[string]map[string]string) string {
	js, _ := json.Marshal(state) // map keys are sorted, so the line is stable
	var b strings.Builder
	b.WriteString(overrideHeader + overrideState + string(js) + "\nservices:\n")
	for _, svc := range slices.Sorted(maps.Keys(state)) {
		b.WriteString("  " + svc + ":\n    labels:\n")
		for _, k := range slices.Sorted(maps.Keys(state[svc])) {
			b.WriteString("      " + k + ": " + yamlQuote(state[svc][k]) + "\n")
		}
	}
	return b.String()
}
