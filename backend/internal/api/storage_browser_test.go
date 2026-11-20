// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"homelab-control/backend/internal/audit"
	"homelab-control/backend/internal/auth"
	"homelab-control/backend/internal/hostinfo"
	"homelab-control/backend/internal/state"
	"homelab-control/backend/internal/storageinfo"
)

// TestStorageBrowserFixture is an opt-in real HTTP/API/database fixture server,
// never a production mode. Its only metadata sources are the labelled fixture
// tree and JSON report below. Credentials live in a private handoff file, never
// command arguments, evidence logs, browser exports or Git.
func TestStorageBrowserFixture(t *testing.T) {
	runtimeDir := os.Getenv("HC_STORAGE_BROWSER_DIR")
	if runtimeDir == "" {
		t.Skip("opt-in local browser fixture; no host data")
	}
	if os.Geteuid() == 0 {
		t.Fatal("browser fixture must run non-root")
	}
	info, e := os.Lstat(runtimeDir)
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("runtime directory must already exist with mode0700")
	}
	uiPath := os.Getenv("HC_STORAGE_BROWSER_UI")
	if _, e = os.Stat(filepath.Join(uiPath, "index.html")); e != nil {
		t.Fatal("build the frontend before the fixture")
	}
	fixture := filepath.Join(runtimeDir, "metadata")
	put := func(path, value string) {
		t.Helper()
		path = filepath.Join(fixture, path)
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, []byte(value), 0600); e != nil {
			t.Fatal(e)
		}
	}
	// Real-shaped sysfs layout: class/block entries link to device directories,
	// partitions are children of the disk directory. The disk sits behind a
	// synthetic SAS HBA (M2-5 Stage B) whose SES enclosure reports it in slot 0.
	link := func(name, target string) {
		t.Helper()
		p := filepath.Join(fixture, name)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink(target, p); e != nil {
			t.Fatal(e)
		}
	}
	const host = "sys/devices/pci0000:00/0000:00:01.0/0000:01:00.0/host0"
	const scsi = host + "/port-0:0/end_device-0:0:0/target0:0:0/0:0:0:0"
	const disk = scsi + "/block/sda"
	put(disk+"/dev", "8:0")
	put(disk+"/size", "2097152")
	// Synthetic SATA-style disk: serial in VPD page 0x80, NAA designator wwid.
	put(scsi+"/vpd_pg80", "\x00\x80\x00\x10EXAMPLE-SERIAL-A")
	put(scsi+"/wwid", "naa.5000000000000a01")
	link(disk+"/device", "../..")
	for _, part := range []struct{ name, dev string }{{"sda1", "8:1"}, {"sda2", "8:2"}} {
		put(disk+"/"+part.name+"/dev", part.dev)
		put(disk+"/"+part.name+"/size", "1048576")
		put(disk+"/"+part.name+"/partition", part.name[3:])
	}
	for _, name := range []string{"sda", "sda1", "sda2"} {
		target := "../../../" + disk
		if name != "sda" {
			target += "/" + name
		}
		link("sys/class/block/"+name, target)
	}
	put(host+"/scsi_host/host0/proc_name", "mpt3sas")
	put(host+"/scsi_host/host0/board_name", "Example-HBA (synthetic)")
	put(host+"/scsi_host/host0/host_sas_address", "0x500605b0000000aa")
	link("sys/class/scsi_host/host0", "../../../"+host+"/scsi_host/host0")
	put("sys/class/sas_device/end_device-0:0:0/phy_identifier", "4")
	const ses = host + "/port-0:0/end_device-0:0:1/target0:0:1/0:0:1:0"
	const enc = ses + "/enclosure/0:0:1:0"
	put(ses+"/vendor", "EXAMPLE")
	put(ses+"/model", "Synthetic JBOD")
	put(enc+"/id", "0x500a0b8000000001")
	link(enc+"/device", "../..")
	put(enc+"/components", "2")
	for i, element := range []string{"Slot 00", "Slot 01"} {
		put(enc+"/"+element+"/type", "array device")
		put(enc+"/"+element+"/slot", strconv.Itoa(i))
	}
	put(enc+"/Slot 00/status", "OK")
	put(enc+"/Slot 01/status", "not installed")
	link(enc+"/Slot 00/device", strings.Repeat("../", strings.Count(enc+"/Slot 00", "/")+1)+scsi)
	link("sys/class/enclosure/0:0:1:0", "../../../"+enc)
	put("proc/self/mountinfo", "1 0 8:2 / /example-data rw - ext4 /dev/sda2 rw\n")
	put("proc/sys/kernel/random/boot_id", "example-boot")
	dbdir := filepath.Join(runtimeDir, "state")
	if e = os.Mkdir(dbdir, 0700); e != nil {
		t.Fatal(e)
	}
	db, e := state.Open(filepath.Join(dbdir, "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	al, e := audit.OpenStore(db.Audit())
	if e != nil {
		t.Fatal(e)
	}
	defer al.Close()
	nonce := make([]byte, 24)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	password := hex.EncodeToString(nonce)
	hash, e := auth.HashPassword(password, auth.MinIterations)
	if e != nil {
		t.Fatal(e)
	}
	users := &auth.UsersFile{Version: 1, Users: []auth.User{{Username: "browser", PasswordHash: hash, Scopes: []string{auth.ScopeSystemRead, auth.ScopeStorageRead}}, {Username: "limited", PasswordHash: hash, Scopes: []string{auth.ScopeSystemRead}}, {Username: "bays", PasswordHash: hash, Scopes: []string{auth.ScopeSystemRead, auth.ScopeStorageRead, auth.ScopeLayoutManage}}}}
	collector := &hostinfo.Collector{Roots: hostinfo.Roots{Proc: filepath.Join(fixture, "proc"), Sys: filepath.Join(fixture, "sys"), Etc: filepath.Join(fixture, "etc")}}
	server := New(Config{}, auth.NewStore(users), al, collector, os.DirFS(uiPath)).WithSessionStore(db.Sessions()).WithStorageSnapshots(db.StorageSnapshots()).WithLayouts(db.Layouts())
	server.storageCollect = func(ctx context.Context) (storageinfo.Graph, error) {
		graph, e := storageinfo.Collect(ctx, fixture)
		if e != nil {
			return graph, e
		}
		graph.Notes = append([]string{"Example data — isolated browser fixture, not live storage or acceptance evidence."}, graph.Notes...)
		// Labelled synthetic report parsed in live mode so the UI's runtime columns
		// are exercised; the production collector supplies no LVM report at all.
		inv, e := storageinfo.ParseLVM(strings.NewReader(`{"report":[{"lv":[{"lv_uuid":"EXAMPLE-RAID","vg_uuid":"EXAMPLE-VG","vg_name":"example","lv_name":"raid","segtype":"raid1","lv_health_status":"partial","raid_sync_action":"recover","sync_percent":"52.5"},{"lv_uuid":"EXAMPLE-CACHE","vg_uuid":"EXAMPLE-VG","vg_name":"example","lv_name":"cached","segtype":"cache","cache_mode":"writeback","cache_policy":"smq","cache_dirty_blocks":"17"}]}]}`), storageinfo.ModeLive)
		if e != nil {
			return graph, e
		}
		inv.PhysicalVolumes, e = storageinfo.ParsePVS(strings.NewReader(`{"report":[{"pvseg":[{"pv_uuid":"EXAMPLE-PV","pv_name":"/dev/example-pv","vg_uuid":"EXAMPLE-VG","vg_name":"example","pv_major":"8","pv_minor":"1","pvseg_start":"0","pvseg_size":"10","lv_uuid":"EXAMPLE-RAID"}]}]}`), storageinfo.ModeLive)
		if e != nil {
			return graph, e
		}
		inv.Source = "Example LVM JSON fixture (synthetic live-mode report, not live storage)"
		storageinfo.AddLVM(&graph, inv)
		return graph, nil
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	httpServer := &http.Server{Handler: server, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	defer httpServer.Close()
	connection := filepath.Join(runtimeDir, "connection.json")
	defer os.Remove(connection)
	b, e := json.Marshal(map[string]string{"url": "http://" + listener.Addr().String(), "username": "browser", "limited_username": "limited", "layout_username": "bays", "password": password})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(connection, b, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log("labelled loopback fixture ready; credentials withheld")
	timer := time.NewTimer(150 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case e := <-done:
			if !errors.Is(e, http.ErrServerClosed) {
				t.Fatal(e)
			}
			return
		case <-timer.C:
			t.Fatal("browser fixture timed out awaiting stop marker")
		case <-ticker.C:
			if _, e = os.Stat(filepath.Join(runtimeDir, "stop")); e == nil {
				latest, ok, e := db.StorageSnapshots().Latest(context.Background())
				if e != nil || !ok || len(latest.Graph) == 0 {
					t.Fatal("browser never produced a persisted observation", e)
				}
				t.Log("fixture shutdown: persisted observations verified; no host data or privileged operations")
				return
			}
		}
	}
}
