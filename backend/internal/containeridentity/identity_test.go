// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package containeridentity

import (
	"encoding/json"
	"strings"
	"testing"
)

func p32(v uint32) *uint32     { return &v }
func um(v Umask) *Umask        { return &v }
func gs(v ...uint32) *[]uint32 { return &v }
func envImage() ImageSupport {
	return ImageSupport{IdentityMethod: MethodEnv, UIDVariable: "PUID", GIDVariable: "PGID", UmaskMethod: UmaskEnv, UmaskVariable: "UMASK", SupplementalGroups: true}
}
func userImage() ImageSupport {
	return ImageSupport{IdentityMethod: MethodRuntimeUser, UmaskMethod: UmaskNone, SupplementalGroups: true}
}

// Platform fixtures use deliberately non-1000 IDs: nothing may assume 1000.
func dockerRootless() PlatformContext {
	return PlatformContext{ID: "docker-local", Kind: "docker", Mode: "rootless", MappingKnown: true,
		UIDMap: []IDRange{{0, 2001, 1}, {1, 300000, 65536}}, GIDMap: []IDRange{{0, 2001, 1}, {1, 300000, 65536}},
		Groups: []NamedGroup{{"docker", 970}, {"lxd", 120}, {"mediashare", 4100}}, Roles: map[string]uint32{"media": 4100}}
}
func dockerRootful() PlatformContext {
	return PlatformContext{ID: "docker-root", Kind: "docker", Mode: "rootful", MappingKnown: true, Groups: []NamedGroup{{"docker", 970}, {"video", 44}}}
}
func podman(mode string) PlatformContext {
	pc := dockerRootless()
	pc.ID, pc.Kind, pc.Mode = "podman-"+mode, "podman", mode
	if mode == "rootful" {
		pc.UIDMap, pc.GIDMap = nil, nil
	}
	return pc
}
func k8s(dist string) PlatformContext {
	return PlatformContext{ID: dist, Kind: "kubernetes", Mode: "cluster", MappingKnown: false, Roles: map[string]uint32{"media": 5000}}
}

func codes(p Preview, sev Severity) []string {
	var out []string
	for _, i := range p.Issues {
		if sev == "" || i.Severity == sev {
			out = append(out, i.Code)
		}
	}
	return out
}
func has(p Preview, sev Severity, code string) bool {
	for _, c := range codes(p, sev) {
		if c == code {
			return true
		}
	}
	return false
}
func blockers(p Preview) []string { return codes(p, SeverityBlocker) }

func TestUmaskParsingAndEffect(t *testing.T) {
	for in, want := range map[string]string{"002": "002", "0022": "022", "027": "027", "077": "077", "000": "000", "0777": "777"} {
		v, err := ParseUmask(in)
		if err != nil || v.String() != want {
			t.Fatal(in, v, err)
		}
	}
	for _, bad := range []string{"", "22", "8", "0888", "00022", "abc", "-022", "0x12", " 022", "1022"} {
		if _, err := ParseUmask(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	var u Umask
	if json.Unmarshal([]byte(`22`), &u) == nil || json.Unmarshal([]byte(`"09"`), &u) == nil {
		t.Fatal("non-string or invalid JSON umask accepted")
	}
	for m, want := range map[Umask][2]string{0o002: {"0664", "0775"}, 0o022: {"0644", "0755"}, 0o027: {"0640", "0750"}, 0o077: {"0600", "0700"}} {
		e := umaskEffect(m)
		if e.Files != want[0] || e.Directories != want[1] || !strings.Contains(e.Explanation, "requested creation mode") {
			t.Fatal(m, e)
		}
	}
	if !strings.Contains(umaskEffect(0o002).Explanation, "group write") || !strings.Contains(umaskEffect(0o077).Explanation, "only the owner") {
		t.Fatal("explanations")
	}
}

func TestImageDefaultsAreLeftUnchanged(t *testing.T) {
	img := envImage()
	img.DefaultUID, img.DefaultGID = p32(911), p32(911)
	pc := dockerRootless()
	pc.Defaults = Settings{UID: p32(3000), GID: p32(3000), Umask: um(0o022)}
	p := Resolve(Request{Mode: ModeImage, Platform: pc, Image: img, Override: Settings{UID: p32(5)}})
	if p.Compose != nil || p.Kubernetes != nil || p.Resolved.UID.Source != LayerImage || *p.Resolved.UID.Value != 911 || p.Resolved.Umask.Source != LayerUnset || len(blockers(p)) != 0 {
		t.Fatalf("%+v", p)
	}
	// No declared default: unset, never invented.
	p = Resolve(Request{Mode: ModeImage, Platform: pc, Image: envImage()})
	if p.Resolved.UID.Value != nil || p.Resolved.UID.Source != LayerUnset {
		t.Fatal(p.Resolved.UID)
	}
	if p.Applied || p.Deployable {
		t.Fatal("preview must never be applied or deployable")
	}
}

func TestLayerPrecedenceAndSources(t *testing.T) {
	pc := dockerRootless()
	pc.Defaults = Settings{UID: p32(3000), GID: p32(3000), Umask: um(0o022), SupplementalGroups: gs(4100)}
	req := Request{Mode: ModeInherit, Platform: pc, Image: envImage(), Application: Settings{Umask: um(0o027)}}
	p := Resolve(req)
	r := p.Resolved
	if r.UID.Source != LayerPlatform || r.Umask.Source != LayerApplication || r.Umask.Value.String() != "027" || r.SupplementalGroups.Source != LayerPlatform {
		t.Fatalf("%+v", r)
	}
	// Profile beats application; override beats profile; groups replace, never merge.
	req.Mode, req.Profile = ModeCustom, "media-application"
	req.Override = Settings{UID: p32(3500), SupplementalGroups: gs()}
	r = Resolve(req).Resolved
	if r.UID.Source != LayerOverride || *r.UID.Value != 3500 || r.GID.Source != LayerPlatform || r.Umask.Source != LayerProfile || r.Umask.Value.String() != "002" || r.SupplementalGroups.Source != LayerOverride || len(r.SupplementalGroups.Value) != 0 {
		t.Fatalf("%+v", r)
	}
	// Inherit mode refuses overrides instead of silently ignoring them.
	req.Mode = ModeInherit
	if p := Resolve(req); !has(p, SeverityBlocker, "override_requires_custom") {
		t.Fatal(codes(p, ""))
	}
	req.Mode = "bogus"
	if p := Resolve(req); !has(p, SeverityBlocker, "mode_invalid") {
		t.Fatal(codes(p, ""))
	}
}

func TestEnvAndRuntimeUserRendering(t *testing.T) {
	pc := dockerRootless()
	ov := Settings{UID: p32(3000), GID: p32(3001), Umask: um(0o002), SupplementalGroups: gs(4100, 4100, 33)}
	p := Resolve(Request{Mode: ModeCustom, Platform: pc, Image: envImage(), Override: ov})
	c := p.Compose
	if c == nil || c.User != "" || c.Environment["PUID"] != "3000" || c.Environment["PGID"] != "3001" || c.Environment["UMASK"] != "002" || strings.Join(c.GroupAdd, ",") != "33,4100" {
		t.Fatalf("%+v %v", c, codes(p, ""))
	}
	if !p.Mechanism.Declared || !has(p, SeverityInfo, "declared_only") || !has(p, SeverityInfo, "groups_deduplicated") || p.Verification != VerifyNotReported {
		t.Fatal(codes(p, ""), p.Verification)
	}
	// Rootless mapping: container 3000 -> host 300000+2999.
	if *p.Resolved.UID.Host != 302999 || p.Resolved.SupplementalGroups.Host[1] != 304099 {
		t.Fatal(p.Resolved)
	}
	// runtime-user: `user: uid:gid`, no env, UMASK not supported -> blocker, nothing injected.
	p = Resolve(Request{Mode: ModeCustom, Platform: pc, Image: userImage(), Override: ov})
	if !has(p, SeverityBlocker, "umask_unsupported") || p.Compose.User != "3000:3001" || p.Compose.Environment != nil {
		t.Fatalf("%+v %v", p.Compose, codes(p, ""))
	}
	// Inherited platform UMASK on an image without support is only a warning and is not rendered.
	pc.Defaults.Umask = um(0o022)
	p = Resolve(Request{Mode: ModeInherit, Platform: pc, Image: userImage()})
	if !has(p, SeverityWarning, "umask_unsupported") || len(blockers(p)) != 0 || p.Compose != nil {
		t.Fatalf("%+v %v", p.Compose, codes(p, ""))
	}
}

func TestInvalidPartialRootAndConflictingIdentities(t *testing.T) {
	pc := dockerRootful()
	for name, tc := range map[string]struct {
		img  ImageSupport
		ov   Settings
		code string
	}{
		"root uid":             {envImage(), Settings{UID: p32(0), GID: p32(100)}, "root_identity"},
		"root gid":             {userImage(), Settings{UID: p32(100), GID: p32(0)}, "root_identity"},
		"reserved -1":          {userImage(), Settings{UID: p32(4294967295), GID: p32(1)}, "id_out_of_range"},
		"partial runtime-user": {userImage(), Settings{UID: p32(3000)}, "partial_identity"},
		"unknown method":       {ImageSupport{IdentityMethod: MethodUnknown, UmaskMethod: UmaskUnknown}, Settings{UID: p32(3000), GID: p32(3000)}, "identity_unsupported"},
		"fixed identity":       {ImageSupport{IdentityMethod: MethodFixed, FixedUID: p32(65532), FixedGID: p32(65532), UmaskMethod: UmaskNone}, Settings{UID: p32(3000), GID: p32(3000)}, "fixed_identity"},
		"env without vars":     {ImageSupport{IdentityMethod: MethodEnv, UmaskMethod: UmaskNone}, Settings{}, "image_declaration_invalid"},
		"too many groups":      {envImage(), Settings{SupplementalGroups: gs(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33)}, "groups_too_many"},
	} {
		p := Resolve(Request{Mode: ModeCustom, Platform: pc, Image: tc.img, Override: tc.ov})
		if !has(p, SeverityBlocker, tc.code) {
			t.Fatal(name, codes(p, ""))
		}
		if tc.code != "fixed_identity" && tc.code != "groups_too_many" && p.Compose != nil && (p.Compose.User != "" || p.Compose.Environment["PUID"] != "") {
			t.Fatal(name, "blocked identity rendered", p.Compose)
		}
		if tc.code == "fixed_identity" && (p.Compose != nil || *p.Resolved.UID.Value != 65532) {
			t.Fatal(name, "fixed identity must stay fixed", p.Compose, p.Resolved.UID)
		}
	}
	// Partial env identity: allowed with a warning; only PUID is rendered.
	p := Resolve(Request{Mode: ModeCustom, Platform: pc, Image: envImage(), Override: Settings{UID: p32(3000)}})
	if !has(p, SeverityWarning, "partial_identity") || p.Compose.Environment["PUID"] != "3000" || p.Compose.Environment["PGID"] != "" {
		t.Fatal(codes(p, ""), p.Compose)
	}
	// Fixed identity that matches the request is accepted silently.
	fx := ImageSupport{IdentityMethod: MethodFixed, FixedUID: p32(65532), FixedGID: p32(65532), UmaskMethod: UmaskNone}
	if p := Resolve(Request{Mode: ModeCustom, Platform: pc, Image: fx, Override: Settings{UID: p32(65532)}}); len(blockers(p)) != 0 {
		t.Fatal(codes(p, ""))
	}
}

func TestSupplementalGroups(t *testing.T) {
	pc := dockerRootful()
	run := func(groups *[]uint32, ack ...uint32) Preview {
		return Resolve(Request{Mode: ModeCustom, Platform: pc, Image: userImage(), Override: Settings{UID: p32(3000), GID: p32(3000), SupplementalGroups: groups}, AcknowledgePrivilegedGroups: ack})
	}
	if p := run(gs()); p.Compose.GroupAdd == nil || len(p.Compose.GroupAdd) != 0 {
		t.Fatal("explicit empty list must render group_add: []", p.Compose)
	}
	if p := run(nil); p.Compose.GroupAdd != nil {
		t.Fatal("unset groups must not render group_add", p.Compose)
	}
	if p := run(gs(44)); strings.Join(p.Compose.GroupAdd, ",") != "44" || p.Resolved.SupplementalGroups.Names[0] != "video" {
		t.Fatal(p.Compose, p.Resolved.SupplementalGroups)
	}
	if p := run(gs(44, 2200, 44)); strings.Join(p.Compose.GroupAdd, ",") != "44,2200" || !has(p, SeverityWarning, "group_unnamed") || len(blockers(p)) != 0 {
		t.Fatal(p.Compose, codes(p, ""))
	}
	for _, g := range []uint32{0, 970, 4294967295} {
		if p := run(gs(44, g)); len(blockers(p)) == 0 || len(p.Compose.GroupAdd) != 0 && p.Compose.GroupAdd != nil {
			t.Fatal(g, codes(p, ""), p.Compose)
		}
	}
	// Without any configured group table, every requested GID warns that its
	// privilege cannot be assessed (it is not silently treated as safe).
	bare := dockerRootful()
	bare.Groups = nil
	if p := Resolve(Request{Mode: ModeCustom, Platform: bare, Image: userImage(), Override: Settings{UID: p32(3000), GID: p32(3000), SupplementalGroups: gs(977)}}); !has(p, SeverityWarning, "group_unnamed") || strings.Join(p.Compose.GroupAdd, ",") != "977" {
		t.Fatal(codes(p, ""), p.Compose)
	}
	// A privileged group is only added after explicit acknowledgement, and still warns.
	if p := run(gs(970), 970); len(blockers(p)) != 0 || !has(p, SeverityWarning, "privileged_group") || strings.Join(p.Compose.GroupAdd, ",") != "970" {
		t.Fatal(codes(p, ""), p.Compose)
	}
	// Unmapped rootless GID.
	rl := dockerRootless()
	rl.GIDMap = []IDRange{{0, 2001, 1}, {1, 300000, 1000}}
	if p := Resolve(Request{Mode: ModeCustom, Platform: rl, Image: userImage(), Override: Settings{UID: p32(500), GID: p32(500), SupplementalGroups: gs(4100)}}); !has(p, SeverityBlocker, "unmapped_id") {
		t.Fatal(codes(p, ""))
	}
	// Unsupported by the image: blocker when explicit.
	img := userImage()
	img.SupplementalGroups = false
	if p := Resolve(Request{Mode: ModeCustom, Platform: pc, Image: img, Override: Settings{SupplementalGroups: gs(44)}}); !has(p, SeverityBlocker, "groups_unsupported") {
		t.Fatal(codes(p, ""))
	}
}

func TestPlatformsRootlessRootfulPodmanAndKubernetes(t *testing.T) {
	ov := Settings{UID: p32(3000), GID: p32(3000), SupplementalGroups: gs(44, 4100)}
	// Rootful: identity mapping, host == container.
	p := Resolve(Request{Mode: ModeCustom, Platform: dockerRootful(), Image: userImage(), Override: ov})
	if *p.Resolved.UID.Host != 3000 || p.Resolved.SupplementalGroups.Host[0] != 44 {
		t.Fatal(p.Resolved)
	}
	// Rootless with unknown mapping: warn, no host IDs, volumes unverified.
	rl := dockerRootless()
	rl.MappingKnown, rl.UIDMap, rl.GIDMap = false, nil, nil
	p = Resolve(Request{Mode: ModeCustom, Platform: rl, Image: userImage(), Override: ov, Volumes: []Volume{{Name: "data", Target: "/data", Access: AccessReadWrite, Host: &HostPathInfo{Directory: true, OwnerUID: 1, OwnerGID: 1, Mode: 0o777}}}})
	if !has(p, SeverityWarning, "mapping_unknown") || p.Resolved.UID.Host != nil || p.Volumes[0].Status != VolumeUnverified {
		t.Fatal(codes(p, ""), p.Volumes)
	}
	// Podman rootless and rootful render Compose fields too.
	for _, mode := range []string{"rootless", "rootful"} {
		p = Resolve(Request{Mode: ModeCustom, Platform: podman(mode), Image: userImage(), Override: ov})
		if p.Compose == nil || p.Compose.User != "3000:3000" || p.Kubernetes != nil {
			t.Fatal(mode, p.Compose, codes(p, ""))
		}
	}
	// Kubernetes and K3s: pod-scoped supplementalGroups, container-scoped runAs*, never Compose.
	for _, dist := range []string{"k3s-cluster", "kubernetes-cluster"} {
		p = Resolve(Request{Mode: ModeCustom, Platform: k8s(dist), Image: userImage(), Override: ov})
		k := p.Kubernetes
		if p.Compose != nil || k == nil || *k.ContainerSecurityContext.RunAsUser != 3000 || *k.ContainerSecurityContext.RunAsGroup != 3000 || !*k.ContainerSecurityContext.RunAsNonRoot || *k.ContainerSecurityContext.AllowPrivilegeEscalation || len(k.PodSecurityContext.SupplementalGroups) != 2 {
			t.Fatalf("%s %+v", dist, k)
		}
		b, _ := json.Marshal(k)
		if strings.Contains(string(b), `"containerSecurityContext":{"supplementalGroups"`) || !strings.Contains(string(b), `"podSecurityContext":{"supplementalGroups":[44,4100]}`) {
			t.Fatal(string(b))
		}
	}
	// Kubernetes env-method images get env entries, sorted.
	p = Resolve(Request{Mode: ModeCustom, Platform: k8s("k3s"), Image: envImage(), Override: Settings{UID: p32(3000), GID: p32(3000), Umask: um(0o027)}})
	if p.Kubernetes.ContainerSecurityContext != nil || len(p.Kubernetes.Env) != 3 || p.Kubernetes.Env[0].Name != "PGID" || p.Kubernetes.Env[2].Value != "027" {
		t.Fatalf("%+v", p.Kubernetes)
	}
	// Unsupported platform kinds and invalid maps fail closed.
	for _, pc := range []PlatformContext{{ID: "x", Kind: "lxc"}, {ID: "y", Kind: "docker", Mode: "rootless", MappingKnown: true, UIDMap: []IDRange{{0, 1, 10}, {5, 100, 10}}}} {
		if p := Resolve(Request{Mode: ModeCustom, Platform: pc, Image: userImage(), Override: ov}); len(blockers(p)) == 0 || p.Compose != nil {
			t.Fatal(pc, codes(p, ""))
		}
	}
}

func TestProfilesUseRolesNotHostIDs(t *testing.T) {
	for _, p := range Profiles() {
		b, _ := json.Marshal(p)
		if strings.Contains(string(b), "1000") || strings.Contains(string(b), "uid") {
			t.Fatal("profile must not carry host IDs", string(b))
		}
	}
	p := Resolve(Request{Mode: ModeInherit, Platform: dockerRootless(), Image: envImage(), Profile: "media-application"})
	if p.Resolved.SupplementalGroups.Value[0] != 4100 || p.Resolved.Umask.Value.String() != "002" || p.Resolved.SupplementalGroups.Source != LayerProfile {
		t.Fatal(p.Resolved)
	}
	p = Resolve(Request{Mode: ModeInherit, Platform: dockerRootless(), Image: envImage(), Profile: "backup-service"})
	if !has(p, SeverityWarning, "profile_role_unmapped") || len(p.Resolved.SupplementalGroups.Value) != 0 || p.Resolved.Umask.Value.String() != "077" {
		t.Fatal(codes(p, ""), p.Resolved)
	}
	p = Resolve(Request{Mode: ModeInherit, Platform: dockerRootless(), Image: envImage(), Profile: "read-only-monitoring", Volumes: []Volume{{Name: "logs", Target: "/logs", Access: AccessReadWrite}}})
	if !has(p, SeverityWarning, "profile_volume_rw") {
		t.Fatal(codes(p, ""))
	}
	if p := Resolve(Request{Mode: ModeInherit, Platform: dockerRootless(), Image: envImage(), Profile: "nope"}); !has(p, SeverityBlocker, "profile_unknown") {
		t.Fatal(codes(p, ""))
	}
}

func TestVolumeCompatibility(t *testing.T) {
	pc := dockerRootful()
	base := Request{Mode: ModeCustom, Platform: pc, Image: userImage(), Override: Settings{UID: p32(3000), GID: p32(3000), SupplementalGroups: gs(44)}}
	vol := func(access Access, h HostPathInfo) VolumeResult {
		r := base
		r.Volumes = []Volume{{Name: "media", Target: "/media", Access: access, Host: &h}}
		return Resolve(r).Volumes[0]
	}
	if v := vol(AccessReadWrite, HostPathInfo{Directory: true, OwnerUID: 3000, OwnerGID: 1, Mode: 0o700}); v.Status != VolumeCompatible {
		t.Fatal(v)
	}
	if v := vol(AccessReadWrite, HostPathInfo{Directory: true, OwnerUID: 9, OwnerGID: 44, Mode: 0o770}); v.Status != VolumeCompatible || !strings.Contains(v.Reason, "group") {
		t.Fatal(v)
	}
	v := vol(AccessReadWrite, HostPathInfo{Directory: true, OwnerUID: 9, OwnerGID: 4100, Mode: 0o775})
	if v.Status != VolumeIncompatible || !strings.Contains(strings.Join(v.Proposals, " "), "supplemental group") || !strings.Contains(strings.Join(v.Proposals, " "), "does not change host ownership") {
		t.Fatal(v)
	}
	if v := vol(AccessRead, HostPathInfo{Directory: true, OwnerUID: 9, OwnerGID: 9, Mode: 0o750, ExtendedACL: true}); v.Status != VolumeUnverified {
		t.Fatal(v)
	}
	// Results never leak host paths; host metadata cannot arrive through JSON.
	var r Request
	if err := json.Unmarshal([]byte(`{"volumes":[{"name":"x","target":"/x","access":"rw","host":{"ownerUid":0,"mode":511}}]}`), &r); err != nil || r.Volumes[0].Host != nil {
		t.Fatal("client-supplied host metadata must be ignored", err)
	}
	// Image-default identity: unverified, never guessed.
	r = base
	r.Mode = ModeImage
	r.Volumes = []Volume{{Name: "m", Target: "/m", Access: AccessRead, Host: &HostPathInfo{Directory: true, Mode: 0o755}}}
	if Resolve(r).Volumes[0].Status != VolumeUnverified {
		t.Fatal("expected unverified")
	}
}

func TestObservedVersusDesired(t *testing.T) {
	req := Request{Mode: ModeCustom, Platform: k8s("k3s"), Image: userImage(), Override: Settings{UID: p32(3000), GID: p32(3000), SupplementalGroups: gs(44)}}
	if Resolve(req).Verification != VerifyNotReported {
		t.Fatal("no observation must be not-reported")
	}
	req.Observed = &Observed{UID: p32(3000), GID: p32(3000), SupplementalGroups: []uint32{44}, Source: "kubernetes-pod-template"}
	if p := Resolve(req); p.Verification != VerifyMatch || p.Observed.Source != "kubernetes-pod-template" {
		t.Fatal(p.Verification)
	}
	req.Observed = &Observed{UID: p32(1001), Source: "kubernetes-pod-template"}
	if p := Resolve(req); p.Verification != VerifyMismatch || !has(p, SeverityWarning, "observed_mismatch") || p.Applied {
		t.Fatal(p.Verification, codes(p, ""))
	}
}

func TestNoContainerPlatformAndRangeValidation(t *testing.T) {
	if p := Resolve(Request{Mode: ModeCustom, Image: userImage()}); !has(p, SeverityBlocker, "platform_unsupported") || p.Compose != nil || p.Kubernetes != nil {
		t.Fatal(codes(p, ""))
	}
	for _, bad := range [][]IDRange{{{0, 0, 0}}, {{4294967290, 0, 10}}, {{0, 0, 10}, {9, 50, 2}}} {
		if ValidateRanges(bad) == nil {
			t.Fatal("accepted", bad)
		}
	}
	if ValidateRanges([]IDRange{{0, 2001, 1}, {1, 300000, 65536}}) != nil {
		t.Fatal("rejected a valid subuid-style map")
	}
}
