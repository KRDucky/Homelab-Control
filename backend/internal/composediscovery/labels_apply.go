// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package composediscovery

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const maxComposeBytes = 1 << 20
const maxComposeFiles = 64
const maxComposeDepth = 4

var (
	ErrComposeApply         = errors.New("invalid Traefik compose label apply")
	ErrComposeNotFound      = errors.New("compose file for the selected project was not found")
	ErrComposeAmbiguous     = errors.New("multiple compose files match the selected project")
	ErrComposeLabelsForm    = errors.New("service labels must be a YAML mapping")
	ErrComposeLabelConflict = errors.New("existing compose label differs")
)

var composeFileName = map[string]bool{
	"compose.yaml":        true,
	"compose.yml":         true,
	"docker-compose.yaml": true,
	"docker-compose.yml":  true,
}
var composeKeyLine = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*:\s*(.*)$`)

// FindComposeFile returns the allowlisted compose file whose root name matches
// project and which defines service. Paths never leave root.
func FindComposeFile(root, project, service string) (string, error) {
	if !dockerName.MatchString(project) || !dockerName.MatchString(service) {
		return "", ErrComposeApply
	}
	abs, err := confinedDir(root)
	if err != nil {
		return "", err
	}
	matches := []string{}
	seen := 0
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return ErrComposeApply
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return ErrComposeApply
		}
		depth := 0
		if rel != "." {
			depth = 1 + strings.Count(rel, string(os.PathSeparator))
		}
		if d.IsDir() {
			base := d.Name()
			if path != abs && (strings.HasPrefix(base, ".") || base == "node_modules") {
				return fs.SkipDir
			}
			if depth > maxComposeDepth {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !composeFileName[d.Name()] {
			return nil
		}
		seen++
		if seen > maxComposeFiles {
			return ErrComposeApply
		}
		resolved, err := confinedFile(abs, path)
		if err != nil {
			return nil
		}
		name, services, err := composeIdentity(resolved)
		if err != nil || name != project || !containsString(services, service) {
			return nil
		}
		matches = append(matches, resolved)
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", ErrComposeNotFound
	}
	if len(matches) > 1 {
		return "", ErrComposeAmbiguous
	}
	return matches[0], nil
}

// PatchServiceLabels inserts or updates SafeLabels on services.<service>.labels.
// Other keys are left untouched. List or flow-style labels are rejected.
func PatchServiceLabels(path, service string, labels map[string]string) (int, error) {
	if !dockerName.MatchString(service) {
		return 0, ErrComposeApply
	}
	safe := SafeLabels(labels)
	if len(safe) == 0 || len(safe) != len(labels) {
		return 0, ErrComposeApply
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > maxComposeBytes || strings.ContainsRune(string(raw), 0) {
		return 0, ErrComposeApply
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	svcIdx, svcIndent, err := findServiceLine(lines, service)
	if err != nil {
		return 0, err
	}
	childIndent := svcIndent + 2
	labelIdx, labelEnd, labelKeyIndent, form, existing, err := findLabelsBlock(lines, svcIdx, svcIndent, childIndent)
	if err != nil {
		return 0, err
	}
	if form != "map" && form != "missing" {
		return 0, ErrComposeLabelsForm
	}
	added := 0
	keys := make([]string, 0, len(safe))
	for k := range safe {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	insert := []string{}
	for _, k := range keys {
		v := safe[k]
		if prior, ok := existing[k]; ok {
			if unquoteYAML(prior) != v {
				return 0, ErrComposeLabelConflict
			}
			continue
		}
		added++
		insert = append(insert, strings.Repeat(" ", labelKeyIndent)+k+": "+yamlQuote(v))
	}
	if added == 0 {
		return 0, nil
	}
	var out []string
	if form == "missing" {
		block := []string{strings.Repeat(" ", childIndent) + "labels:"}
		block = append(block, insert...)
		out = append(out, lines[:svcIdx+1]...)
		out = append(out, block...)
		out = append(out, lines[svcIdx+1:]...)
	} else {
		out = append(out, lines[:labelEnd]...)
		out = append(out, insert...)
		out = append(out, lines[labelEnd:]...)
		_ = labelIdx
	}
	body := strings.Join(out, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err = writeAtomic(path, body); err != nil {
		return 0, err
	}
	return added, nil
}

func composeIdentity(path string) (string, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > maxComposeBytes {
		return "", nil, ErrComposeApply
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	name := ""
	inServices := false
	servicesIndent := -1
	var services []string
	for _, line := range lines {
		if strings.ContainsRune(line, '\t') {
			return "", nil, ErrComposeApply
		}
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, _, ok := mappingKey(trim)
		if !ok {
			continue
		}
		if indent == 0 && key == "name" && name == "" {
			name = unquoteYAML(strings.TrimSpace(strings.TrimPrefix(trim, "name:")))
		}
		if indent == 0 && key == "services" {
			inServices = true
			servicesIndent = indent
			continue
		}
		if inServices && indent <= servicesIndent {
			inServices = false
		}
		if inServices && indent == servicesIndent+2 {
			services = append(services, key)
		}
	}
	if !dockerName.MatchString(name) || len(services) == 0 {
		return "", nil, ErrComposeApply
	}
	return name, services, nil
}

func findServiceLine(lines []string, service string) (int, int, error) {
	inServices := false
	servicesIndent := -1
	for i, line := range lines {
		if strings.ContainsRune(line, '\t') {
			return 0, 0, ErrComposeApply
		}
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, _, ok := mappingKey(trim)
		if !ok {
			continue
		}
		if indent == 0 && key == "services" {
			inServices = true
			servicesIndent = indent
			continue
		}
		if inServices && indent <= servicesIndent {
			inServices = false
		}
		if inServices && indent == servicesIndent+2 && key == service {
			return i, indent, nil
		}
	}
	return 0, 0, ErrComposeNotFound
}

func findLabelsBlock(lines []string, svcIdx, svcIndent, childIndent int) (start, end, keyIndent int, form string, existing map[string]string, err error) {
	existing = map[string]string{}
	start, end, form = -1, -1, "missing"
	keyIndent = childIndent + 2
	for i := svcIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.ContainsRune(line, '\t') {
			return 0, 0, 0, "", nil, ErrComposeApply
		}
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent <= svcIndent {
			break
		}
		key, rest, ok := mappingKey(trim)
		if indent == childIndent && ok && key == "labels" {
			start = i
			rest = strings.TrimSpace(rest)
			if rest == "{}" || rest == "" {
				form = "map"
				end = i + 1
				if rest == "{}" {
					return 0, 0, 0, "", nil, ErrComposeLabelsForm
				}
				for j := i + 1; j < len(lines); j++ {
					t := strings.TrimSpace(lines[j])
					if t == "" || strings.HasPrefix(t, "#") {
						continue
					}
					ji := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
					if ji <= childIndent {
						end = j
						return start, end, keyIndent, form, existing, nil
					}
					if strings.HasPrefix(t, "-") {
						return 0, 0, 0, "", nil, ErrComposeLabelsForm
					}
					lk, lv, lok := mappingKey(t)
					if !lok {
						return 0, 0, 0, "", nil, ErrComposeLabelsForm
					}
					keyIndent = ji
					existing[unquoteYAML(lk)] = strings.TrimSpace(lv)
					end = j + 1
				}
				return start, end, keyIndent, form, existing, nil
			}
			if strings.HasPrefix(rest, "{") {
				return 0, 0, 0, "", nil, ErrComposeLabelsForm
			}
			return 0, 0, 0, "", nil, ErrComposeLabelsForm
		}
		if indent == childIndent && ok && start == -1 && end == -1 {
			// still looking for labels among siblings
			continue
		}
	}
	return start, svcIdx + 1, keyIndent, "missing", existing, nil
}

func mappingKey(trim string) (key, rest string, ok bool) {
	m := composeKeyLine.FindStringSubmatch(trim)
	if m == nil {
		return "", "", false
	}
	return unquoteYAML(m[1]), m[2], true
}

func unquoteYAML(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `\"`, `"`)
	}
	return s
}

func yamlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func confinedDir(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", ErrComposeApply
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", ErrComposeApply
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", ErrComposeApply
	}
	return resolved, nil
}

func confinedFile(root, path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", ErrComposeApply
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", ErrComposeApply
	}
	return resolved, nil
}

func writeAtomic(path, body string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return ErrComposeApply
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
		return ErrComposeApply
	}
	if err = tmp.Chmod(0644); err != nil {
		return ErrComposeApply
	}
	if err = tmp.Close(); err != nil {
		return ErrComposeApply
	}
	if err = os.Rename(tmpName, path); err != nil {
		return ErrComposeApply
	}
	ok = true
	return nil
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
