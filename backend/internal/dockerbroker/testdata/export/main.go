// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
// Controlled UI fixture export ONLY. Executed inside the isolated build, never
// against MediaMouse Docker or a host broker. Synthetic responses are labeled.
package main

import (
	"context"
	"encoding/json"
	"homelab-control/backend/internal/composediscovery"
	"homelab-control/backend/internal/dockerbroker"
	"homelab-control/backend/internal/traefikclient"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	dir, e := os.MkdirTemp("", "hc-broker-ui-")
	must(e)
	defer os.RemoveAll(dir)
	fixture, e := os.ReadFile("internal/composediscovery/testdata/docker-containers.json")
	must(e)
	engineSocket := filepath.Join(dir, "engine.sock")
	engineListener, e := net.Listen("unix", engineSocket)
	must(e)
	must(os.Chmod(engineSocket, 0600))
	engine := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.RequestURI() != "/v1.53/containers/json?all=true&size=false" {
			http.Error(w, "refused", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture)
	})}
	go engine.Serve(engineListener)
	defer engine.Close()
	provider, e := composediscovery.NewDocker(engineSocket, uint32(os.Getuid()), composediscovery.Context{})
	must(e)
	socket := filepath.Join(dir, "broker.sock")
	listener, e := dockerbroker.Listen(socket)
	must(e)
	token := "isolated-ui-fixture-token-0123456789abcdef"
	server, e := dockerbroker.NewServer(provider, token, uint32(os.Getuid()))
	must(e)
	go server.Serve(listener)
	defer server.Close()
	disabled := false
	client, e := dockerbroker.NewClient(socket, token, uint32(os.Getuid()), composediscovery.Context{Networks: []string{"hc-traefik-test"}, Entrypoints: []string{"web"}, Resolvers: []string{}, DockerProvider: &disabled})
	must(e)
	snapshot, e := client.Snapshot(context.Background())
	must(e)
	// Explicitly identify the controlled dataset AFTER validating the broker wire.
	snapshot.Source = "controlled-broker-fixture"
	snapshot.Fixture = true
	model := traefikclient.Model{Name: "proxy-host", Host: "phase7.example.com", Port: 8080, Entrypoint: "web", Protocol: "http", UpstreamScheme: "http"}
	request := composediscovery.Request{ServiceID: snapshot.Services[0].ID, Network: "hc-traefik-test", Model: model}
	plain, e := composediscovery.Build(snapshot, traefikclient.Inventory{}, request)
	must(e)
	request.Model.Protocol = "https"
	request.Model.TLS = &traefikclient.TLS{}
	tls, e := composediscovery.Build(snapshot, traefikclient.Inventory{}, request)
	must(e)
	result := struct {
		Catalog composediscovery.Catalog  `json:"catalog"`
		HTTP    composediscovery.Proposal `json:"http"`
		HTTPS   composediscovery.Proposal `json:"https"`
	}{composediscovery.View(snapshot), plain, tls}
	must(json.NewEncoder(os.Stdout).Encode(result))
}
func must(e error) {
	if e != nil {
		log.Fatal("controlled fixture export failed")
	}
}
