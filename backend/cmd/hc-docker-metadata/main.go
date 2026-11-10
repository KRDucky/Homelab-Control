// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"flag"
	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/dockerbroker"
	"homelab-control/backend/internal/traefikclient"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	listen := flag.String("listen-unix", "", "fixed pre-provisioned private Unix socket path")
	socket := flag.String("docker-socket", "", "explicit independently verified rootless Docker Unix socket")
	owner := flag.Uint("docker-owner-uid", 1000, "verified rootless daemon socket owner UID")
	client := flag.Uint("client-uid", 100999, "authorized CORE host-mapped peer UID")
	tokenfile := flag.String("token-file", "", "private broker service token file")
	flag.Parse()
	if flag.NArg() != 0 || *listen == "" || *socket == "" || *tokenfile == "" || os.Geteuid() == 0 || uint(os.Geteuid()) != *owner || *owner > 0xffffffff || *client > 0xffffffff {
		log.Fatal("invalid broker configuration or host identity")
	}
	syscall.Umask(0077)
	token, e := traefikclient.LoadToken(*tokenfile)
	if e != nil {
		log.Fatal("private broker credential unavailable")
	}
	provider, e := composediscovery.NewDocker(*socket, uint32(*owner), composediscovery.Context{})
	if e != nil {
		log.Fatal("rootless Docker transport unavailable")
	}
	server, e := dockerbroker.NewServer(provider, token, uint32(*client))
	if e != nil {
		log.Fatal("metadata service unavailable")
	}
	listener, e := dockerbroker.Listen(*listen)
	if e != nil {
		log.Fatal("private listener unavailable; existing paths are never replaced")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Print("metadata broker ready")
	if e = server.Serve(listener); e != nil && e != http.ErrServerClosed {
		log.Fatal("metadata broker stopped unexpectedly")
	}
	log.Print("metadata broker stopped")
}
