// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/filesinfo"
)

// File-manager read/write slice (owner authorization 2026-11-20, ADR 0012
// amendment). Read and download keep files.read; content write, upload,
// create and rename need files.write; delete needs files.destroy. hc-api does
// not spawn and there is no broker file operation: the verbs run in-process
// against the confined jail with openat + O_NOFOLLOW (see internal/filesinfo).

type filesEntryResult struct {
	Root  string          `json:"root"`
	Path  string          `json:"path"`
	Entry filesinfo.Entry `json:"entry"`
}

type filesDeletePlanResponse struct {
	Token string `json:"token"`
	Root  string `json:"root"`
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Size  int64  `json:"size"`
}

type filesDeleteApplyRequest struct {
	Token string `json:"token"`
}

// filesCreateRequest is a single create verb (a file or a directory).
type filesCreateRequest struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file" or "dir"
}

// filesRenameRequest renames path to to inside the same root.
type filesRenameRequest struct {
	To string `json:"to"`
}

// filesWriteGuard fails closed when the audit logger is unavailable, so no
// write runs that cannot be audited.
func (s *Server) filesWriteGuard(w http.ResponseWriter) bool {
	if s.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "File changes require an access audit logger.")
		return false
	}
	return true
}

// filesAuditDetail bounds the audit detail for a root + path pair. A short
// ASCII path stays readable; other or deep paths use a labelled SHA-256
// digest. It never records contents or the host jail path.
func filesAuditDetail(root, p string) string {
	return filesReadDetail(root, p)
}

// filesTwoPathDetail bounds the audit detail for a rename (root + source +
// destination).
func filesTwoPathDetail(root, from, to string) string {
	d := "root=" + root + " from=" + from + " to=" + to
	if len(d) <= 128 && auditPathLiteralSafe(from) && auditPathLiteralSafe(to) {
		return d
	}
	fs := sha256.Sum256([]byte(from))
	ts := sha256.Sum256([]byte(to))
	return "root=" + root + " from_sha256=" + hex.EncodeToString(fs[:16]) + " to_sha256=" + hex.EncodeToString(ts[:16])
}

// filesRWError maps the read/write slice sentinels to stable API errors.
func filesRWError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, filesinfo.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "files_unavailable", "No browseable file roots are configured on this server.")
	case errors.Is(err, filesinfo.ErrUnknownRoot):
		writeError(w, http.StatusNotFound, "not_found", "Unknown file root or path.")
	case errors.Is(err, filesinfo.ErrBadPath):
		writeError(w, http.StatusBadRequest, "bad_request", "Path must be a relative path inside the confined root.")
	case errors.Is(err, filesinfo.ErrEscape):
		writeError(w, http.StatusForbidden, "forbidden", "Path is outside the confined root.")
	case errors.Is(err, filesinfo.ErrNotDir):
		writeError(w, http.StatusBadRequest, "not_a_directory", "Path is not a directory.")
	case errors.Is(err, filesinfo.ErrNotRegular):
		writeError(w, http.StatusBadRequest, "not_a_regular_file", "Path is not a regular file.")
	case errors.Is(err, filesinfo.ErrBadName):
		writeError(w, http.StatusBadRequest, "invalid_name", "Name must be a single path segment without separators.")
	case errors.Is(err, filesinfo.ErrExists):
		writeError(w, http.StatusConflict, "already_exists", "An entry with that name already exists.")
	case errors.Is(err, filesinfo.ErrNotEmpty):
		writeError(w, http.StatusConflict, "not_empty", "The directory is not empty.")
	case errors.Is(err, filesinfo.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "The content exceeds the size limit.")
	case errors.Is(err, filesinfo.ErrPermission):
		writeError(w, http.StatusForbidden, "not_permitted", "The path could not be changed.")
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "The request could not be completed.")
	}
}

func fileEncoding(data []byte) string {
	if utf8.Valid(data) && !containsNUL(data) {
		return "utf8"
	}
	return "binary"
}

func containsNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

// contentDisposition builds a quoted attachment filename from only the base
// name, so a host path can never leak into the header.
func contentDisposition(name string) string {
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.ReplaceAll(name, "\"", "")
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	if b.String() == "" {
		name = "download"
	} else {
		name = b.String()
	}
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

// handleFilesContent reads one regular file's bytes (capped). It returns raw
// bytes with an encoding hint and the stat size; the session CSRF token is
// required (filesProtectedGET), and the audit records root id + path only.
func (s *Server) handleFilesContent(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.filesProtectedGET(w, r, sess) {
		return
	}
	id := r.URL.Query().Get("root")
	rel := r.URL.Query().Get("path")
	cleaned, err := filesinfo.RelPath(rel)
	if err != nil {
		filesRWError(w, err)
		return
	}
	content, err := s.files.ReadContent(id, rel)
	if err != nil {
		filesRWError(w, err)
		return
	}
	if err := s.auditLog("files.content.read", sess.Username, r, "returned", filesAuditDetail(id, cleaned)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "File content cannot be returned because its access could not be audited.")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Size", strconv.FormatInt(content.Size, 10))
	w.Header().Set("X-Content-Truncated", strconv.FormatBool(content.Truncated))
	w.Header().Set("X-File-Encoding", fileEncoding(content.Bytes))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content.Bytes)
}

// handleFilesDownload returns the same capped bytes as read with a
// Content-Disposition attachment; the filename is the base name only.
func (s *Server) handleFilesDownload(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.filesProtectedGET(w, r, sess) {
		return
	}
	id := r.URL.Query().Get("root")
	rel := r.URL.Query().Get("path")
	cleaned, err := filesinfo.RelPath(rel)
	if err != nil {
		filesRWError(w, err)
		return
	}
	content, err := s.files.ReadContent(id, rel)
	if err != nil {
		filesRWError(w, err)
		return
	}
	if err := s.auditLog("files.download", sess.Username, r, "returned", filesAuditDetail(id, cleaned)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "File content cannot be returned because its access could not be audited.")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Size", strconv.FormatInt(content.Size, 10))
	w.Header().Set("X-Content-Truncated", strconv.FormatBool(content.Truncated))
	w.Header().Set("Content-Disposition", contentDisposition(path.Base(cleaned)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content.Bytes)
}

// handleFilesContentWrite replaces an existing regular file's content. The
// body is the raw bytes; there is no create.
func (s *Server) handleFilesContentWrite(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.filesWriteGuard(w) {
		return
	}
	id := r.URL.Query().Get("root")
	rel := r.URL.Query().Get("path")
	cleaned, err := filesinfo.RelPath(rel)
	if err != nil {
		filesRWError(w, err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, filesinfo.MaxContentBytes+1))
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "The content exceeds the size limit.")
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", "Could not read the request body.")
		return
	}
	if len(data) > filesinfo.MaxContentBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "The content exceeds the size limit.")
		return
	}
	if err := s.auditLog("files.content.write", sess.Username, r, "requested", filesAuditDetail(id, cleaned)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "File content cannot be changed because the change could not be audited.")
		return
	}
	entry, err := s.files.WriteContent(id, rel, data)
	if err != nil {
		filesRWError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, filesEntryResult{Root: id, Path: cleaned, Entry: entry})
}

// handleFilesUpload creates a new file from a multipart upload under path
// (the destination directory). The multipart file's base name is validated as
// a single segment so it cannot escape the jail; O_EXCL never overwrites.
func (s *Server) handleFilesUpload(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.filesWriteGuard(w) {
		return
	}
	id := r.URL.Query().Get("root")
	rel := r.URL.Query().Get("path")
	dir, err := filesinfo.RelPath(rel)
	if err != nil {
		filesRWError(w, err)
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Expected a multipart upload.")
		return
	}
	var name string
	var data []byte
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "Malformed multipart upload.")
			return
		}
		if part.FormName() != "file" {
			continue
		}
		name = part.FileName()
		if err := filesinfo.ValidName(name); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_name", "The uploaded filename must be a single path segment.")
			return
		}
		buf, err := io.ReadAll(io.LimitReader(part, filesinfo.MaxUploadBytes+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "Could not read the upload.")
			return
		}
		if len(buf) > filesinfo.MaxUploadBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "The upload exceeds the size limit.")
			return
		}
		data = buf
		break
	}
	if name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "The upload is missing the file part.")
		return
	}
	dest := path.Join(dir, name)
	if err := s.auditLog("files.upload", sess.Username, r, "requested", filesAuditDetail(id, dest)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The upload could not be audited.")
		return
	}
	entry, err := s.files.Create(id, dir, name, false)
	if err != nil {
		filesRWError(w, err)
		return
	}
	if _, err := s.files.WriteContent(id, dest, data); err != nil {
		filesRWError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, filesEntryResult{Root: id, Path: dest, Entry: entry})
}

// handleFilesCreate makes a new file or empty directory under path.
func (s *Server) handleFilesCreate(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.filesWriteGuard(w) {
		return
	}
	id := r.URL.Query().Get("root")
	rel := r.URL.Query().Get("path")
	parent, err := filesinfo.RelPath(rel)
	if err != nil {
		filesRWError(w, err)
		return
	}
	var req filesCreateRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if err := filesinfo.ValidName(req.Name); err != nil {
		filesRWError(w, err)
		return
	}
	var asDir bool
	switch req.Type {
	case "file":
		asDir = false
	case "dir":
		asDir = true
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "Type must be file or dir.")
		return
	}
	dest := path.Join(parent, req.Name)
	if err := s.auditLog("files.create", sess.Username, r, "requested", filesAuditDetail(id, dest)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The entry could not be created because it could not be audited.")
		return
	}
	entry, err := s.files.Create(id, parent, req.Name, asDir)
	if err != nil {
		filesRWError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, filesEntryResult{Root: id, Path: dest, Entry: entry})
}

// handleFilesRename moves path to to inside the same root.
func (s *Server) handleFilesRename(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.filesWriteGuard(w) {
		return
	}
	id := r.URL.Query().Get("root")
	rel := r.URL.Query().Get("path")
	src, err := filesinfo.RelPath(rel)
	if err != nil {
		filesRWError(w, err)
		return
	}
	var req filesRenameRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	dst, err := filesinfo.RelPath(req.To)
	if err != nil {
		filesRWError(w, err)
		return
	}
	if err := s.auditLog("files.rename", sess.Username, r, "requested", filesTwoPathDetail(id, src, dst)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The entry could not be renamed because it could not be audited.")
		return
	}
	entry, err := s.files.Rename(id, src, dst)
	if err != nil {
		filesRWError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, filesEntryResult{Root: id, Path: dst, Entry: entry})
}

// ---- delete: plan → confirm (ADR 0011 pattern, no broker op) --------------

const filePlanTTL = 120 * time.Second
const filePlanMax = 256

// filePlanPayload is the signed, single-use delete-plan token body. It is
// bound to actor, session, root, path and the target's observed identity so a
// replayed or substituted token can never delete something else.
type filePlanPayload struct {
	ID      string `json:"id"`
	Root    string `json:"root"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Size    int64  `json:"size"`
	Mtime   string `json:"mtime"`
	User    string `json:"user"`
	Session string `json:"session"`
	Exp     int64  `json:"exp"`
}

type filePlanStore struct {
	key  [32]byte
	mu   sync.Mutex
	live map[string]filePlanPayload
	now  func() time.Time
}

func newFilePlanStore() *filePlanStore {
	s := &filePlanStore{live: map[string]filePlanPayload{}, now: time.Now}
	_, _ = rand.Read(s.key[:])
	return s
}

func (s *filePlanStore) hmac(raw []byte) []byte {
	m := hmac.New(sha256.New, s.key[:])
	m.Write(raw)
	return m.Sum(nil)
}

func (s *filePlanStore) mint(root, pathStr, kind string, size int64, mtime, user, session string) string {
	var idb [16]byte
	_, _ = rand.Read(idb[:])
	p := filePlanPayload{
		ID: hex.EncodeToString(idb[:]), Root: root, Path: pathStr, Kind: kind,
		Size: size, Mtime: mtime, User: user, Session: session,
		Exp: s.now().Add(filePlanTTL).Unix(),
	}
	raw, _ := json.Marshal(p)
	s.mu.Lock()
	s.live[p.ID] = p
	if len(s.live) > filePlanMax {
		// Drop the oldest outstanding plan (by expiry) to stay bounded.
		var oldest string
		var oldestExp int64 = 1 << 62
		for id, v := range s.live {
			if v.Exp < oldestExp {
				oldest, oldestExp = id, v.Exp
			}
		}
		delete(s.live, oldest)
	}
	s.mu.Unlock()
	return "hcfp1." + base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(s.hmac(raw))
}

func (s *filePlanStore) parse(token string) (filePlanPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "hcfp1" {
		return filePlanPayload{}, errors.New("invalid plan token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return filePlanPayload{}, errors.New("invalid plan token")
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return filePlanPayload{}, errors.New("invalid plan token")
	}
	if !hmac.Equal(mac, s.hmac(raw)) {
		return filePlanPayload{}, errors.New("invalid plan token")
	}
	var p filePlanPayload
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return filePlanPayload{}, errors.New("invalid plan token")
	}
	if p.Exp < s.now().Unix() {
		return filePlanPayload{}, errors.New("expired plan token")
	}
	return p, nil
}

// consume atomically deletes the plan, returning false when it was already
// used (single use).
func (s *filePlanStore) consume(p filePlanPayload) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.live[p.ID]; !ok {
		return false
	}
	delete(s.live, p.ID)
	return true
}

// handleFilesDeletePlan returns a bound, single-use plan token after
// describing the target (no change). Scope files.destroy.
func (s *Server) handleFilesDeletePlan(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.filesWriteGuard(w) {
		return
	}
	id := r.URL.Query().Get("root")
	rel := r.URL.Query().Get("path")
	cleaned, err := filesinfo.RelPath(rel)
	if err != nil {
		filesRWError(w, err)
		return
	}
	target, err := s.files.StatTarget(id, rel)
	if err != nil {
		filesRWError(w, err)
		return
	}
	switch target.Kind {
	case "file", "dir", "symlink":
	default:
		writeError(w, http.StatusBadRequest, "not_deletable", "Only files, empty directories and symbolic links can be deleted.")
		return
	}
	if err := s.auditLog("files.delete.plan", sess.Username, r, "returned", filesAuditDetail(id, cleaned)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The delete could not be planned because it could not be audited.")
		return
	}
	token := s.filePlans.mint(id, cleaned, target.Kind, target.Size, target.Mtime, sess.Username, sess.Key)
	writeJSON(w, http.StatusOK, filesDeletePlanResponse{Token: token, Root: id, Path: cleaned, Kind: target.Kind, Size: target.Size})
}

// handleFilesDeleteApply consumes the plan token, requires step-up (a
// destructive change), revalidates the target, then deletes it. A changed or
// missing target is a conflict and nothing runs.
func (s *Server) handleFilesDeleteApply(w http.ResponseWriter, r *http.Request, sess *auth.Session, _ string) {
	if !s.filesWriteGuard(w) {
		return
	}
	var req filesDeleteApplyRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	p, err := s.filePlans.parse(req.Token)
	if err != nil {
		s.auditLog("files.delete.apply", sess.Username, r, "invalid_token", "")
		writeError(w, http.StatusForbidden, "invalid_token", "The delete plan token is invalid or expired.")
		return
	}
	if p.User != sess.Username || p.Session != sess.Key {
		s.auditLog("files.delete.apply", sess.Username, r, "invalid_token", "")
		writeError(w, http.StatusForbidden, "invalid_token", "The delete plan token is invalid or expired.")
		return
	}
	// Destructive: a password re-entry is required (ADR 0011 §3). A missing
	// step-up does not consume the token so the user can confirm after re-entry.
	if _, stepped := s.stepups.At(sess.Key, sess.Username); !stepped {
		s.auditLog("files.delete.apply", sess.Username, r, "step_up_required", filesAuditDetail(p.Root, p.Path))
		writeError(w, http.StatusForbidden, "step_up_required", "Re-enter your password to confirm this delete.")
		return
	}
	if !s.filePlans.consume(p) {
		s.auditLog("files.delete.apply", sess.Username, r, "invalid_token", "")
		writeError(w, http.StatusForbidden, "invalid_token", "The delete plan token is invalid or expired.")
		return
	}
	if err := s.auditLog("files.delete.apply", sess.Username, r, "requested", filesAuditDetail(p.Root, p.Path)); err != nil {
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "The delete could not be audited.")
		return
	}
	cur, err := s.files.StatTarget(p.Root, p.Path)
	if err != nil {
		filesRWError(w, err)
		return
	}
	if cur.Kind != p.Kind || cur.Size != p.Size || cur.Mtime != p.Mtime {
		s.auditLog("files.delete.apply", sess.Username, r, "conflict", filesAuditDetail(p.Root, p.Path))
		writeError(w, http.StatusConflict, "delete_conflict", "The target changed since the plan; nothing was deleted.")
		return
	}
	if err := s.files.Delete(p.Root, p.Path); err != nil {
		filesRWError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"root": p.Root, "path": p.Path, "status": "deleted"})
}
