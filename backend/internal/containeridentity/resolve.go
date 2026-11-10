// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package containeridentity

import (
	"fmt"
	"slices"
	"strings"
)

// Severity of a validation issue. Blockers make a preview not applicable;
// warnings require the user's attention; info explains behaviour.
type Severity string

const (
	SeverityBlocker Severity = "blocker"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Issue is one validation finding.
type Issue struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Field    string   `json:"field,omitempty"`
	Message  string   `json:"message"`
}

// PrivilegedGroupNames are host group names that commonly grant authority
// far beyond file access (container daemons, hypervisors, raw disks, sudo,
// logs). Matching is by the administrator-configured name of a GID on the
// platform host; GIDs are never assumed. GID 0 is always privileged.
var PrivilegedGroupNames = []string{"root", "wheel", "sudo", "admin", "adm", "docker", "podman", "lxd", "incus", "incus-admin", "libvirt", "kvm", "disk", "shadow", "systemd-journal"}

// Request is everything needed to resolve and preview one container's identity.
type Request struct {
	Mode        Mode            `json:"mode"`
	Platform    PlatformContext `json:"platform"`
	Image       ImageSupport    `json:"image"`
	Application Settings        `json:"application"`
	Profile     string          `json:"profile,omitempty"`
	Override    Settings        `json:"override"`
	// AcknowledgePrivilegedGroups lists GIDs the user explicitly accepted
	// despite a privileged-group warning. Without it such groups block.
	AcknowledgePrivilegedGroups []uint32  `json:"acknowledgePrivilegedGroups,omitempty"`
	Observed                    *Observed `json:"observed,omitempty"`
	Volumes                     []Volume  `json:"volumes,omitempty"`
}

// ResolvedID is a desired UID or GID and the layer that supplied it.
type ResolvedID struct {
	Value  *uint32 `json:"value"`
	Source Layer   `json:"source"`
	// Host is the host ID after the platform's user-namespace mapping, when known.
	Host *uint32 `json:"host,omitempty"`
}

// UmaskEffect explains a UMASK for the common creation modes.
type UmaskEffect struct {
	Files       string `json:"files"`       // from requested 0666
	Directories string `json:"directories"` // from requested 0777
	Explanation string `json:"explanation"`
}

type ResolvedUmask struct {
	Value  *Umask       `json:"value"`
	Source Layer        `json:"source"`
	Effect *UmaskEffect `json:"effect,omitempty"`
}

type ResolvedGroups struct {
	Value  []uint32 `json:"value"`
	Source Layer    `json:"source"`
	Host   []uint32 `json:"host,omitempty"`
	Names  []string `json:"names,omitempty"` // configured host names, "" when unknown
}

// Resolved is the effective desired configuration after layering and
// validation. Values that cannot be applied to this image are dropped (and
// reported), never silently "applied".
type Resolved struct {
	UID                ResolvedID     `json:"uid"`
	GID                ResolvedID     `json:"gid"`
	Umask              ResolvedUmask  `json:"umask"`
	SupplementalGroups ResolvedGroups `json:"supplementalGroups"`
}

// Verification compares desired settings with platform observations.
type Verification string

const (
	VerifyNotReported Verification = "not-reported"   // the platform cannot report effective identity
	VerifyMatch       Verification = "observed-match" // observed values equal the desired ones
	VerifyMismatch    Verification = "observed-mismatch"
)

// Preview is the complete, read-only result. Applied and Deployable are
// always false in this phase: previews never change a workload.
type Preview struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Mode          Mode                `json:"mode"`
	Platform      PlatformSummary     `json:"platform"`
	Resolved      Resolved            `json:"resolved"`
	Mechanism     MechanismSummary    `json:"mechanism"`
	Compose       *ComposeFragment    `json:"compose,omitempty"`
	Kubernetes    *KubernetesFragment `json:"kubernetes,omitempty"`
	Volumes       []VolumeResult      `json:"volumes"`
	Issues        []Issue             `json:"issues"`
	Verification  Verification        `json:"verification"`
	Observed      *Observed           `json:"observed,omitempty"`
	Applied       bool                `json:"applied"`
	Deployable    bool                `json:"deployable"`
}

type PlatformSummary struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Mode         string `json:"mode"`
	MappingKnown bool   `json:"mappingKnown"`
}

type MechanismSummary struct {
	Identity           IdentityMethod `json:"identity"`
	Umask              UmaskMethod    `json:"umask"`
	SupplementalGroups bool           `json:"supplementalGroups"`
	// Declared is true when every rendered value depends on the image's
	// declaration being accurate (env-based identity/umask): HomeControl can
	// show what it configures but not prove the process honours it.
	Declared bool `json:"declared"`
}

type sourced[T any] struct {
	v   *T
	src Layer
}

func pick[T any](layers []Layer, vals []*T) sourced[T] {
	out := sourced[T]{src: LayerUnset}
	for i, v := range vals {
		if v != nil {
			out = sourced[T]{v: v, src: layers[i]}
		}
	}
	return out
}

// Resolve layers, validates and previews a request. It is pure: no I/O.
func Resolve(req Request) Preview {
	p := Preview{SchemaVersion: 1, Mode: req.Mode, Platform: PlatformSummary{req.Platform.ID, req.Platform.Kind, req.Platform.Mode, req.Platform.MappingKnown},
		Volumes: []VolumeResult{}, Issues: []Issue{}, Verification: VerifyNotReported, Observed: req.Observed}
	add := func(sev Severity, code, field, msg string, a ...any) {
		p.Issues = append(p.Issues, Issue{sev, code, field, fmt.Sprintf(msg, a...)})
	}
	p.Mechanism = MechanismSummary{req.Image.IdentityMethod, req.Image.UmaskMethod, req.Image.SupplementalGroups,
		req.Image.IdentityMethod == MethodEnv || req.Image.UmaskMethod == UmaskEnv}
	if err := req.Image.Validate(); err != nil {
		add(SeverityBlocker, "image_declaration_invalid", "image", "Image identity declaration is invalid: %v.", err)
		return finish(p)
	}
	switch req.Platform.Kind {
	case "docker", "podman", "kubernetes":
	default:
		add(SeverityBlocker, "platform_unsupported", "platform", "Container identity is not supported for this platform kind.")
		return finish(p)
	}
	if err := firstErr(ValidateRanges(req.Platform.UIDMap), ValidateRanges(req.Platform.GIDMap)); err != nil {
		add(SeverityBlocker, "platform_mapping_invalid", "platform", "Platform ID mapping is invalid: %v.", err)
		return finish(p)
	}

	// Collect layers. Image mode uses nothing HomeControl-supplied.
	var profile *Profile
	if req.Profile != "" {
		if req.Mode == ModeImage {
			add(SeverityWarning, "profile_ignored", "profile", "Profiles are ignored when using image defaults.")
		} else if pr, ok := ProfileByID(req.Profile); ok {
			profile = &pr
		} else {
			add(SeverityBlocker, "profile_unknown", "profile", "Unknown identity profile.")
		}
	}
	var prof Settings
	if profile != nil {
		prof = profile.settings(req.Platform, add)
	}
	layers := []Layer{LayerPlatform, LayerApplication, LayerProfile, LayerOverride}
	sets := []Settings{req.Platform.Defaults, req.Application, prof, req.Override}
	switch req.Mode {
	case ModeImage:
		sets = []Settings{{}, {}, {}, {}}
		if !isZero(req.Override) || !isZero(req.Application) {
			add(SeverityInfo, "image_defaults", "", "Image defaults selected: HomeControl leaves the image's identity, UMASK and groups unchanged.")
		}
	case ModeInherit:
		if !isZero(req.Override) {
			add(SeverityBlocker, "override_requires_custom", "override", "Per-container overrides require the custom mode.")
		}
		sets[3] = Settings{}
	case ModeCustom:
	default:
		add(SeverityBlocker, "mode_invalid", "mode", "Mode must be image, inherit or custom.")
		return finish(p)
	}

	uid := pick(layers, []*uint32{sets[0].UID, sets[1].UID, sets[2].UID, sets[3].UID})
	gid := pick(layers, []*uint32{sets[0].GID, sets[1].GID, sets[2].GID, sets[3].GID})
	um := pick(layers, []*Umask{sets[0].Umask, sets[1].Umask, sets[2].Umask, sets[3].Umask})
	grp := pick(layers, []*[]uint32{sets[0].SupplementalGroups, sets[1].SupplementalGroups, sets[2].SupplementalGroups, sets[3].SupplementalGroups})

	p.Resolved.UID, p.Resolved.GID = resolveIdentity(req, uid, gid, add)
	p.Resolved.Umask = resolveUmask(req, um, add)
	p.Resolved.SupplementalGroups = resolveGroups(req, grp, add)

	if req.Observed != nil {
		p.Verification = verify(p.Resolved, *req.Observed)
		if p.Verification == VerifyMismatch {
			add(SeverityWarning, "observed_mismatch", "", "The platform reports an identity different from the desired configuration; the running workload has not been changed.")
		}
	} else if p.Mechanism.Declared && (p.Resolved.UID.Source != LayerUnset || p.Resolved.Umask.Source != LayerUnset) {
		add(SeverityInfo, "declared_only", "", "Values are passed through variables the image declares it honours; the effective runtime identity cannot be verified until the platform reports it.")
	}
	render(&p, req)
	p.Volumes = checkVolumes(req, p.Resolved, add)
	return finish(p)
}

func finish(p Preview) Preview {
	p.Applied, p.Deployable = false, false
	return p
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func isZero(s Settings) bool {
	return s.UID == nil && s.GID == nil && s.Umask == nil && s.SupplementalGroups == nil
}

func resolveIdentity(req Request, uid, gid sourced[uint32], add func(Severity, string, string, string, ...any)) (ResolvedID, ResolvedID) {
	img := req.Image
	ru, rg := ResolvedID{Source: LayerUnset}, ResolvedID{Source: LayerUnset}
	explicit := func(s sourced[uint32]) bool { return s.src == LayerOverride || s.src == LayerApplication }
	// Image declared defaults are shown as the image layer when nothing overrides them.
	imageDefault := func(v *uint32) ResolvedID {
		if v != nil {
			return ResolvedID{Value: v, Source: LayerImage}
		}
		return ResolvedID{Source: LayerUnset}
	}
	set := uid.v != nil || gid.v != nil
	if set {
		for _, s := range []struct {
			name string
			v    sourced[uint32]
		}{{"uid", uid}, {"gid", gid}} {
			if s.v.v != nil && *s.v.v > MaxID {
				add(SeverityBlocker, "id_out_of_range", s.name, "%s must be between 1 and %d.", strings.ToUpper(s.name), MaxID)
				return ru, rg
			}
			if s.v.v != nil && *s.v.v == 0 {
				add(SeverityBlocker, "root_identity", s.name, "HomeControl never configures %s 0 (root). Use a non-root identity; image defaults are left unchanged when nothing is set.", strings.ToUpper(s.name))
				return ru, rg
			}
		}
	}
	switch img.IdentityMethod {
	case MethodFixed:
		ru, rg = ResolvedID{Value: img.FixedUID, Source: LayerImage}, ResolvedID{Value: img.FixedGID, Source: LayerImage}
		if set && !((uid.v == nil || *uid.v == *img.FixedUID) && (gid.v == nil || *gid.v == *img.FixedGID)) {
			sev := SeverityWarning
			if explicit(uid) || explicit(gid) {
				sev = SeverityBlocker
			}
			add(sev, "fixed_identity", "uid", "This image runs with a fixed identity (%d:%d) and does not support PUID/PGID changes; the requested identity is not applied.", *img.FixedUID, *img.FixedGID)
		}
		return mapIDs(req, ru, rg, add)
	case MethodUnknown:
		if set {
			sev := SeverityWarning
			if explicit(uid) || explicit(gid) {
				sev = SeverityBlocker
			}
			add(sev, "identity_unsupported", "uid", "The image does not declare how it accepts a process identity, so HomeControl does not configure one (no variables are injected). Declare env or runtime-user support in the manifest first.")
		}
		return imageDefault(img.DefaultUID), imageDefault(img.DefaultGID)
	}
	ru, rg = imageDefault(img.DefaultUID), imageDefault(img.DefaultGID)
	if uid.v != nil {
		ru = ResolvedID{Value: uid.v, Source: uid.src}
	}
	if gid.v != nil {
		rg = ResolvedID{Value: gid.v, Source: gid.src}
	}
	if set && (uid.v == nil || gid.v == nil) {
		missing := "PGID"
		if uid.v == nil {
			missing = "PUID"
		}
		if img.IdentityMethod == MethodRuntimeUser {
			add(SeverityBlocker, "partial_identity", strings.ToLower(missing[1:]), "Runtime identity needs both UID and GID: with only one, Docker/Podman pick the image's primary group (possibly GID 0) and Kubernetes keeps the image group. Set %s too.", missing)
			return ResolvedID{Source: LayerUnset}, ResolvedID{Source: LayerUnset}
		}
		add(SeverityWarning, "partial_identity", strings.ToLower(missing[1:]), "%s is not set; the image's default applies for it.", missing)
	}
	return mapIDs(req, ru, rg, add)
}

func mapIDs(req Request, ru, rg ResolvedID, add func(Severity, string, string, string, ...any)) (ResolvedID, ResolvedID) {
	pc := req.Platform
	for _, x := range []struct {
		name   string
		r      *ResolvedID
		ranges []IDRange
	}{{"uid", &ru, pc.UIDMap}, {"gid", &rg, pc.GIDMap}} {
		if x.r.Value == nil {
			continue
		}
		switch {
		case pc.Mode == "rootless" && !pc.MappingKnown:
			add(SeverityWarning, "mapping_unknown", x.name, "Rootless platform: the host %s for container %s %d is unknown because no user-namespace map is configured; host file access cannot be checked.", strings.ToUpper(x.name), strings.ToUpper(x.name), *x.r.Value)
		case pc.MappingKnown && len(x.ranges) > 0:
			h, ok := mapID(*x.r.Value, x.ranges)
			if !ok {
				add(SeverityBlocker, "unmapped_id", x.name, "Container %s %d is outside the platform's user-namespace map; files would appear owned by the overflow ID and writes would fail.", strings.ToUpper(x.name), *x.r.Value)
				continue
			}
			x.r.Host = &h
		case pc.MappingKnown:
			h := *x.r.Value // identity mapping (rootful, no userns remap)
			x.r.Host = &h
		}
	}
	return ru, rg
}

func resolveUmask(req Request, um sourced[Umask], add func(Severity, string, string, string, ...any)) ResolvedUmask {
	if um.v == nil {
		return ResolvedUmask{Source: LayerUnset}
	}
	switch req.Image.UmaskMethod {
	case UmaskEnv, UmaskEntrypoint:
	default:
		sev := SeverityWarning
		if um.src == LayerOverride || um.src == LayerApplication {
			sev = SeverityBlocker
		}
		add(sev, "umask_unsupported", "umask", "The image does not declare UMASK support, so HomeControl does not set it (an unrecognised variable would have no effect). The image's default UMASK applies.")
		return ResolvedUmask{Source: LayerUnset}
	}
	v := *um.v
	return ResolvedUmask{Value: &v, Source: um.src, Effect: umaskEffect(v)}
}

func umaskEffect(u Umask) *UmaskEffect {
	f, d := 0o666&^uint16(u), 0o777&^uint16(u)
	e := &UmaskEffect{Files: fmt.Sprintf("%04o", f), Directories: fmt.Sprintf("%04o", d)}
	switch {
	case u&0o077 == 0o077:
		e.Explanation = "Removes all group and other permissions from new files and directories: only the owner can access them."
	case u&0o020 == 0:
		e.Explanation = "Keeps group write: members of the file's group can modify new files and directories (typical for shared media)."
	case u&0o007 == 0o007:
		e.Explanation = "Group may read (not write); others get no access to new files and directories."
	default:
		e.Explanation = "Group and others may read new files; only the owner may write."
	}
	e.Explanation += " These are upper bounds: the application's requested creation mode and the filesystem (ACLs, mount options) can grant less."
	return e
}

func resolveGroups(req Request, grp sourced[[]uint32], add func(Severity, string, string, string, ...any)) ResolvedGroups {
	if grp.v == nil {
		return ResolvedGroups{Value: []uint32{}, Source: LayerUnset}
	}
	explicit := grp.src == LayerOverride || grp.src == LayerApplication
	in := *grp.v
	if !req.Image.SupplementalGroups && len(in) > 0 {
		sev := SeverityWarning
		if explicit {
			sev = SeverityBlocker
		}
		add(sev, "groups_unsupported", "supplementalGroups", "The image or deployment method does not support supplemental groups; none are added.")
		return ResolvedGroups{Value: []uint32{}, Source: LayerUnset}
	}
	if len(in) > MaxSupplementalGroups {
		add(SeverityBlocker, "groups_too_many", "supplementalGroups", "At most %d supplemental groups are supported.", MaxSupplementalGroups)
		return ResolvedGroups{Value: []uint32{}, Source: LayerUnset}
	}
	out := slices.Clone(in)
	slices.Sort(out)
	if c := slices.Compact(out); len(c) != len(in) {
		add(SeverityInfo, "groups_deduplicated", "supplementalGroups", "Duplicate GIDs were removed.")
		out = c
	}
	res := ResolvedGroups{Value: out, Source: grp.src, Names: make([]string, len(out))}
	names := map[uint32]string{}
	for _, g := range req.Platform.Groups {
		names[g.GID] = g.Name
	}
	acked := map[uint32]bool{}
	for _, g := range req.AcknowledgePrivilegedGroups {
		acked[g] = true
	}
	blocked := false
	for i, g := range out {
		res.Names[i] = names[g]
		if g > MaxID {
			add(SeverityBlocker, "gid_out_of_range", "supplementalGroups", "GID %d is out of range.", g)
			blocked = true
			continue
		}
		if g == 0 {
			add(SeverityBlocker, "privileged_group", "supplementalGroups", "GID 0 (root group) is never added as a supplemental group.")
			blocked = true
			continue
		}
		if name := names[g]; slices.Contains(PrivilegedGroupNames, name) {
			if acked[g] {
				add(SeverityWarning, "privileged_group", "supplementalGroups", "GID %d (%s) may grant elevated authority (for example control of a container daemon, virtual machines, disks or logs); accepted explicitly.", g, name)
			} else {
				add(SeverityBlocker, "privileged_group", "supplementalGroups", "GID %d (%s) may grant elevated authority. It is not added unless explicitly acknowledged.", g, name)
				blocked = true
			}
		} else if names[g] == "" {
			// Without a configured name, HomeControl cannot tell whether this GID
			// is a privileged host group (for example docker or lxd); say so.
			add(SeverityWarning, "group_unnamed", "supplementalGroups", "GID %d is not in the platform's configured group table, so HomeControl cannot tell whether it grants elevated authority (for example a container daemon or hypervisor group). Verify it on the host before use.", g)
		}
	}
	if blocked {
		return ResolvedGroups{Value: []uint32{}, Source: LayerUnset}
	}
	pc := req.Platform
	if pc.Mode == "rootless" && !pc.MappingKnown && len(out) > 0 {
		add(SeverityWarning, "mapping_unknown", "supplementalGroups", "Rootless platform without a configured GID map: the host groups these GIDs correspond to are unknown.")
	} else if pc.MappingKnown {
		host := make([]uint32, 0, len(out))
		for _, g := range out {
			if len(pc.GIDMap) == 0 {
				host = append(host, g)
				continue
			}
			h, ok := mapID(g, pc.GIDMap)
			if !ok {
				add(SeverityBlocker, "unmapped_id", "supplementalGroups", "Supplemental GID %d is outside the platform's user-namespace map and would not grant any host access.", g)
				return ResolvedGroups{Value: []uint32{}, Source: LayerUnset}
			}
			host = append(host, h)
		}
		res.Host = host
	}
	return res
}

func verify(r Resolved, o Observed) Verification {
	if o.UID == nil && o.GID == nil && o.SupplementalGroups == nil {
		return VerifyNotReported
	}
	eq := func(want *uint32, got *uint32) bool { return want == nil || (got != nil && *got == *want) }
	if !eq(r.UID.Value, o.UID) || !eq(r.GID.Value, o.GID) {
		return VerifyMismatch
	}
	if r.SupplementalGroups.Source != LayerUnset && o.SupplementalGroups != nil {
		got := slices.Clone(o.SupplementalGroups)
		slices.Sort(got)
		if !slices.Equal(slices.Compact(got), r.SupplementalGroups.Value) {
			return VerifyMismatch
		}
	}
	return VerifyMatch
}
