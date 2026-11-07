// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package filesinfo lists directory entries under administrator-allow-listed
// confined roots (file-manager first checkpoint). hc-api stays unprivileged
// and does not spawn: each List opens the jail with O_NOFOLLOW|O_DIRECTORY
// and walks RelPath components with openat+O_NOFOLLOW. There is no broker
// operation (ADR 0008 forbids a generic path-taking file-read).
package filesinfo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// DefaultPath is the administrator's confined-root allow-list.
const DefaultPath = "/etc/homelab-control/files.json"

// MaxEntries is the directory listing cap. A directory with more entries is
// refused rather than truncated.
const MaxEntries = 4096

const maxFile = 16 << 10
const maxRoots = 32
const maxRel = 4096

// ErrUnsafe marks a setting file with unsafe ownership or permissions.
var ErrUnsafe = errors.New("file-roots setting is not root-owned or is group/world-writable")

// Sentinel errors mapped by the API.
var (
	ErrUnavailable = errors.New("no browseable file roots are configured")
	ErrUnknownRoot = errors.New("unknown file root")
	ErrBadPath     = errors.New("path is not a confined relative path")
	ErrEscape      = errors.New("path is outside the confined root")
	ErrPermission  = errors.New("path is not readable")
	ErrNotDir      = errors.New("path is not a directory")
	ErrTooMany     = errors.New("directory has too many entries")
)

// Root is one allow-listed jail. Path is the canonical host directory; it is
// never returned by the API.
type Root struct {
	ID    string
	Label string
	Path  string
}

// PublicRoot is what the API may return: identifiers only, never host paths.
type PublicRoot struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Entry is metadata of one directory member. It never includes file contents
// or a symlink target (an absolute target would leak a host path).
type Entry struct {
	Name  string `json:"name"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	Owner string `json:"owner"`
	Mode  string `json:"mode"`
	Mtime string `json:"mtime"`
}

// Set is the effective allow-list. The zero value has no roots (fail closed).
type Set struct {
	roots []Root
	// Reason explains a fail-closed state ("" when at least one root is usable).
	Reason string
}

// BrowseAvailable is true when the allow-list was readable and has at least
// one valid root.
func (s Set) BrowseAvailable() bool { return len(s.roots) > 0 }

// PublicRoots returns id and label only.
func (s Set) PublicRoots() []PublicRoot {
	out := make([]PublicRoot, len(s.roots))
	for i, r := range s.roots {
		out[i] = PublicRoot{ID: r.ID, Label: r.Label}
	}
	return out
}

type file struct {
	Version int        `json:"version"`
	Roots   []fileRoot `json:"roots"`
}

type fileRoot struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}

var idRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Load reads the setting. A missing file, empty roots, unreadable file,
// unsafe mode, or any invalid root is an error; callers then fail closed.
func Load(path string, requireRoot bool) (Set, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return Set{}, fmt.Errorf("%s: missing", path)
	}
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return Set{}, fmt.Errorf("%s: refusing to follow a symlink", path)
		}
		return Set{}, fmt.Errorf("%s: %w", path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Set{}, err
	}
	if !fi.Mode().IsRegular() {
		return Set{}, fmt.Errorf("%s: not a regular file", path)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return Set{}, fmt.Errorf("%s: %w", path, ErrUnsafe)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && requireRoot && st.Uid != 0 {
		return Set{}, fmt.Errorf("%s: %w", path, ErrUnsafe)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return Set{}, err
	}
	if len(b) > maxFile {
		return Set{}, fmt.Errorf("%s: larger than %d bytes", path, maxFile)
	}
	return Parse(b)
}

// Parse strictly decodes {"version":1,"roots":[{"id","label","path"},...]}.
func Parse(b []byte) (Set, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		return Set{}, fmt.Errorf("file-roots setting: %w", err)
	}
	// More() is the array/object iterator and does not reject a second
	// top-level JSON value. A second Decode must be io.EOF.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Set{}, errors.New("file-roots setting: trailing data")
	}
	if f.Version != 1 {
		return Set{}, fmt.Errorf("file-roots setting: unsupported version %d", f.Version)
	}
	if len(f.Roots) == 0 {
		return Set{}, errors.New("file-roots setting: no roots")
	}
	if len(f.Roots) > maxRoots {
		return Set{}, fmt.Errorf("file-roots setting: more than %d roots", maxRoots)
	}
	seen := map[string]bool{}
	var roots []Root
	for _, r := range f.Roots {
		if !idRE.MatchString(r.ID) {
			return Set{}, fmt.Errorf("file-roots setting: invalid root id %q", r.ID)
		}
		if seen[r.ID] {
			return Set{}, fmt.Errorf("file-roots setting: duplicate root id %q", r.ID)
		}
		seen[r.ID] = true
		if err := validLabel(r.Label); err != nil {
			return Set{}, err
		}
		resolved, err := canonicalizeRoot(r.Path)
		if err != nil {
			return Set{}, err
		}
		roots = append(roots, Root{ID: r.ID, Label: r.Label, Path: resolved})
	}
	return Set{roots: roots}, nil
}

func validLabel(s string) error {
	if s == "" || len(s) > 64 {
		return errors.New("file-roots setting: label must be 1 to 64 characters")
	}
	if !utf8.ValidString(s) {
		return errors.New("file-roots setting: label is not valid UTF-8")
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return errors.New("file-roots setting: label must not contain control characters")
		}
	}
	return nil
}

// LoadOrFailClosed never returns an error: a missing, empty, unreadable or
// unsafe setting has no roots.
func LoadOrFailClosed(path string, requireRoot bool) Set {
	s, err := Load(path, requireRoot)
	if err != nil {
		return Set{Reason: "The file-roots setting " + path + " could not be used (" + err.Error() + "), so browsing is disabled."}
	}
	return s
}

func canonicalizeRoot(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) {
		return "", fmt.Errorf("file-roots setting: path %q is not absolute", p)
	}
	if strings.Contains(p, "\x00") {
		return "", errors.New("file-roots setting: path contains NUL")
	}
	trimmed := strings.TrimSuffix(p, string(filepath.Separator))
	if trimmed == "" {
		trimmed = p
	}
	cleaned := filepath.Clean(p)
	if cleaned != p && cleaned != trimmed {
		return "", fmt.Errorf("file-roots setting: path %q is not canonical", p)
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", fmt.Errorf("file-roots setting: path %q could not be canonicalized: %w", cleaned, err)
	}
	if !filepath.IsAbs(resolved) {
		return "", fmt.Errorf("file-roots setting: path %q resolved to a relative location", p)
	}
	if forbiddenPath(resolved) {
		return "", fmt.Errorf("file-roots setting: path %q is not an allowed root", cleaned)
	}
	f, err := os.OpenFile(resolved, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
	if err != nil {
		return "", fmt.Errorf("file-roots setting: path %q is not a confined directory: %w", resolved, err)
	}
	defer f.Close()
	return resolved, nil
}

func forbiddenPath(p string) bool {
	if p == "/" {
		return true
	}
	for _, prefix := range []string{"/etc", "/root", "/proc", "/sys", "/home"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

func (s Set) root(id string) (Root, bool) {
	for _, r := range s.roots {
		if r.ID == id {
			return r, true
		}
	}
	return Root{}, false
}

// RelPath validates a caller-supplied relative path inside a jail. Empty is
// the jail root. Extra slashes, "..", absolute paths and NUL are refused
// before anything is opened.
func RelPath(p string) (string, error) {
	if p == "" || p == "." {
		return ".", nil
	}
	if len(p) > maxRel || strings.Contains(p, "\x00") || strings.Contains(p, "\\") {
		return "", ErrBadPath
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.Contains(p, "//") {
		return "", ErrBadPath
	}
	for _, c := range strings.Split(strings.TrimSuffix(p, "/"), "/") {
		if c == ".." {
			return "", ErrEscape
		}
		if c == "" || c == "." {
			return "", ErrBadPath
		}
	}
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrEscape
	}
	if cleaned != p && cleaned+"/" != p {
		return "", ErrBadPath
	}
	return cleaned, nil
}

// List returns metadata for entries of root id at relative path. It never
// follows a symlink (O_NOFOLLOW) and never reads file contents. The jail is
// reopened each call with O_NOFOLLOW so a post-Load symlink swap is refused.
func (s Set) List(id, rel string) ([]Entry, error) {
	r, ok := s.root(id)
	if !ok {
		if !s.BrowseAvailable() {
			return nil, ErrUnavailable
		}
		return nil, ErrUnknownRoot
	}
	rel, err := RelPath(rel)
	if err != nil {
		return nil, err
	}
	jail, err := os.OpenFile(r.Path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, mapOpenErr(err)
	}
	defer jail.Close()
	dir := jail
	if rel != "." {
		d, err := walkRel(jail, rel)
		if err != nil {
			return nil, mapOpenErr(err)
		}
		defer d.Close()
		dir = d
	}
	ents, err := dir.ReadDir(MaxEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(ents) > MaxEntries {
		return nil, ErrTooMany
	}
	dfd := int(dir.Fd())
	out := make([]Entry, 0, len(ents))
	for _, de := range ents {
		name := de.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}
		e, err := statEntry(dfd, name)
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// walkRel opens each RelPath component from jail with openat+O_NOFOLLOW.
// A last-component symlink is never a directory. Intermediate names use
// the same flags, so they are not followed either.
func walkRel(jail *os.File, rel string) (*os.File, error) {
	cur := jail
	owned := false
	for _, name := range strings.Split(rel, "/") {
		next, err := openatDir(cur, name)
		if owned {
			_ = cur.Close()
		}
		if err != nil {
			return nil, err
		}
		cur = next
		owned = true
	}
	return cur, nil
}

func openatDir(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func mapOpenErr(err error) error {
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return ErrNotDir
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOENT) {
		return ErrUnknownRoot
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || errors.Is(err, fs.ErrPermission) {
		return ErrPermission
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		msg := pe.Err.Error()
		if strings.Contains(msg, "escaped") || strings.Contains(msg, "outside") {
			return ErrEscape
		}
	}
	// Unexpected errors must not look like a successful jail denial of "..".
	return err
}

func statEntry(dirfd int, name string) (Entry, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(dirfd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return Entry{}, err
	}
	return entryFromStat(name, &st), nil
}

func entryFromStat(name string, st *unix.Stat_t) Entry {
	mode := unixMode(uint32(st.Mode))
	sec, nsec := st.Mtim.Unix()
	e := Entry{
		Name:  name,
		Dir:   mode.IsDir(),
		Size:  int64(st.Size),
		Owner: fmt.Sprintf("%d:%d", st.Uid, st.Gid),
		Mode:  mode.String(),
		Mtime: time.Unix(sec, nsec).UTC().Format(time.RFC3339),
	}
	if mode&os.ModeSymlink != 0 {
		e.Dir = false
	}
	return e
}

func unixMode(m uint32) os.FileMode {
	mode := os.FileMode(m & 0o777)
	switch m & syscall.S_IFMT {
	case syscall.S_IFBLK:
		mode |= os.ModeDevice
	case syscall.S_IFCHR:
		mode |= os.ModeDevice | os.ModeCharDevice
	case syscall.S_IFDIR:
		mode |= os.ModeDir
	case syscall.S_IFIFO:
		mode |= os.ModeNamedPipe
	case syscall.S_IFLNK:
		mode |= os.ModeSymlink
	case syscall.S_IFSOCK:
		mode |= os.ModeSocket
	}
	if m&syscall.S_ISUID != 0 {
		mode |= os.ModeSetuid
	}
	if m&syscall.S_ISGID != 0 {
		mode |= os.ModeSetgid
	}
	if m&syscall.S_ISVTX != 0 {
		mode |= os.ModeSticky
	}
	return mode
}
