// Package server accepts broker connections, authenticates the peer with
// SO_PEERCRED, enforces framing limits and dispatches to the closed op
// registry (ADR 0008).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/broker/ops"
	"homelab-control/backend/internal/broker/proto"
)

// Server is the broker.
type Server struct {
	AllowedUIDs []uint32
	Env         *ops.Env
	Audit       *audit.Logger
	MaxInFlight int
	registry    map[string]ops.Op
	sem         chan struct{}
	wg          sync.WaitGroup
}

// New creates a server.
func New(env *ops.Env, al *audit.Logger, allowed ...uint32) *Server {
	s := &Server{AllowedUIDs: allowed, Env: env, Audit: al, MaxInFlight: 8, registry: ops.Registry()}
	s.sem = make(chan struct{}, s.MaxInFlight)
	return s
}

// Serve accepts connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, l *net.UnixListener) error {
	go func() { <-ctx.Done(); l.Close() }()
	for {
		c, err := l.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				s.wg.Wait()
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(ctx, c)
		}()
	}
}

// PeerCred returns the kernel-verified credentials of a Unix socket peer.
func PeerCred(c *net.UnixConn) (*syscall.Ucred, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cred *syscall.Ucred
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		cred, cerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	return cred, cerr
}

func (s *Server) log(e audit.Event) {
	if s.Audit != nil {
		_ = s.Audit.Log(e)
	}
}

func (s *Server) reply(c *net.UnixConn, resp proto.Response) {
	resp.V = proto.Version
	_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := proto.WriteFrame(c, resp, proto.MaxResponse); err != nil {
		_ = proto.WriteFrame(c, proto.Response{V: proto.Version, Error: &proto.Error{Code: proto.CodeInternal, Message: "response too large"}}, proto.MaxResponse)
	}
}

func fail(code, msg string) proto.Response {
	return proto.Response{Error: &proto.Error{Code: code, Message: msg}}
}

func (s *Server) handle(ctx context.Context, c *net.UnixConn) {
	defer c.Close()
	cred, err := PeerCred(c)
	if err != nil {
		s.log(audit.Event{Event: "broker.peer", Outcome: "denied", Detail: "no peer credentials"})
		return
	}
	peer := "uid=" + strconv.Itoa(int(cred.Uid)) + " pid=" + strconv.Itoa(int(cred.Pid))
	if !slices.Contains(s.AllowedUIDs, cred.Uid) {
		s.log(audit.Event{Event: "broker.peer", Remote: peer, Outcome: "denied", Detail: "uid not allowed"})
		s.reply(c, fail(proto.CodeForbidden, "peer not allowed"))
		return
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var req proto.Request
	if err := proto.ReadFrame(c, &req, proto.MaxRequest); err != nil {
		s.log(audit.Event{Event: "broker.request", Remote: peer, Outcome: "rejected", Detail: "malformed frame"})
		s.reply(c, fail(proto.CodeBadRequest, "malformed request"))
		return
	}
	ev := audit.Event{Event: "broker.op", User: req.Actor.User, Remote: peer, Detail: req.Op}
	if req.V != proto.Version {
		ev.Outcome = "rejected"
		s.log(ev)
		s.reply(c, fail(proto.CodeBadRequest, "unsupported protocol version"))
		return
	}
	op, ok := s.registry[req.Op]
	if !ok {
		ev.Outcome = "unknown_op"
		s.log(ev)
		s.reply(c, fail(proto.CodeUnknownOp, "no such operation"))
		return
	}
	// Defence in depth: the API already checked the scope. Root peers
	// (hlctl diagnostics) are still subject to the op's scope.
	if op.Scope != "" && !slices.Contains(req.Actor.Scopes, op.Scope) {
		ev.Outcome = "forbidden"
		s.log(ev)
		s.reply(c, fail(proto.CodeForbidden, "actor lacks scope "+op.Scope))
		return
	}
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		ev.Outcome = "busy"
		s.log(ev)
		s.reply(c, fail(proto.CodeBusy, "broker is busy"))
		return
	}
	octx, cancel := context.WithTimeout(ctx, op.Timeout)
	defer cancel()
	start := time.Now()
	res, err := s.run(octx, op, req.Args)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		var pe *proto.Error
		if !errors.As(err, &pe) {
			pe = &proto.Error{Code: proto.CodeInternal, Message: "operation failed"}
		}
		ev.Outcome = pe.Code
		ev.Detail = fmt.Sprintf("%s %s", req.Op, dur)
		s.log(ev)
		s.reply(c, proto.Response{Error: pe})
		return
	}
	b, err := json.Marshal(res)
	if err != nil {
		s.reply(c, fail(proto.CodeInternal, "could not encode result"))
		return
	}
	ev.Outcome = "ok"
	ev.Detail = fmt.Sprintf("%s %s", req.Op, dur)
	s.log(ev)
	s.reply(c, proto.Response{OK: true, Result: b})
}

// run invokes the handler, converting panics into internal errors so one bad
// op cannot take the broker down.
func (s *Server) run(ctx context.Context, op ops.Op, args json.RawMessage) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &proto.Error{Code: proto.CodeInternal, Message: "operation panicked"}
		}
	}()
	return op.Handle(ctx, s.Env, args)
}

// Listener returns the systemd-activated socket (LISTEN_FDS) if present,
// otherwise creates path itself (development/tests) with mode 0660.
func Listener(path string) (*net.UnixListener, bool, error) {
	if os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()) && os.Getenv("LISTEN_FDS") == "1" {
		f := os.NewFile(3, "systemd-socket")
		l, err := net.FileListener(f)
		f.Close()
		if err != nil {
			return nil, false, err
		}
		ul, ok := l.(*net.UnixListener)
		if !ok {
			return nil, false, errors.New("activated socket is not a Unix stream socket")
		}
		os.Unsetenv("LISTEN_PID")
		os.Unsetenv("LISTEN_FDS")
		os.Unsetenv("LISTEN_FDNAMES")
		return ul, true, nil
	}
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, false, fmt.Errorf("%s exists and is not a socket", path)
		}
		_ = os.Remove(path)
	}
	old := syscall.Umask(0o117)
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	syscall.Umask(old)
	if err != nil {
		return nil, false, err
	}
	l.SetUnlinkOnClose(true)
	return l, false, nil
}
