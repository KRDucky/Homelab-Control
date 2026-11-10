// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package containeridentity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"

	"homelab-control/backend/internal/platform"
)

// ContainerRuntime is the optional `runtime.container` section of the
// existing v1 plugin manifest (schemas/plugin/v1.json). It is declarative:
// it states what a plugin's container image supports so the installer can
// validate and preview configuration. It grants nothing by itself.
type ContainerRuntime struct {
	SupportedPlatforms []string            `json:"supportedPlatforms"`
	DeploymentMethods  []string            `json:"deploymentMethods"`
	Identity           IdentityDecl        `json:"identity"`
	Privileged         bool                `json:"privileged"`
	Capabilities       []string            `json:"capabilities"`
	Volumes            []VolumeRequirement `json:"volumes"`
}

type IdentityDecl struct {
	Configurable       bool      `json:"configurable"`
	Method             string    `json:"method"`
	UIDVariable        string    `json:"uidVariable,omitempty"`
	GIDVariable        string    `json:"gidVariable,omitempty"`
	FixedUID           *uint32   `json:"fixedUid,omitempty"`
	FixedGID           *uint32   `json:"fixedGid,omitempty"`
	Umask              UmaskDecl `json:"umask"`
	SupplementalGroups bool      `json:"supplementalGroups"`
}

type UmaskDecl struct {
	Method   string `json:"method"`
	Variable string `json:"variable,omitempty"`
}

type VolumeRequirement struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Access string `json:"access"`
}

var (
	capName   = regexp.MustCompile(`^[A-Z][A-Z_]{1,31}$`)
	volName   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	platforms = []string{"docker", "podman", "kubernetes"}
	methods   = []string{"compose", "kubernetes-manifest"}
)

// ParseContainerRuntime strictly decodes and validates a runtime.container
// section: bounded size and depth, no duplicate or unknown keys.
func ParseContainerRuntime(b []byte) (ContainerRuntime, error) {
	var out ContainerRuntime
	var probe any
	if platform.DecodeBounded(b, 16<<10, 8, &probe) != nil {
		return out, errors.New("runtime.container must be one bounded JSON object without duplicate keys")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("runtime.container: %v", err)
	}
	if dec.Decode(&probe) != io.EOF {
		return out, errors.New("runtime.container: trailing data")
	}
	return out, out.Validate()
}

func setOf(vals, allowed []string, field string) error {
	if len(vals) == 0 {
		return fmt.Errorf("%s must list at least one value", field)
	}
	seen := map[string]bool{}
	for _, v := range vals {
		if !slices.Contains(allowed, v) || seen[v] {
			return fmt.Errorf("%s contains an unknown or duplicate value", field)
		}
		seen[v] = true
	}
	return nil
}

// Validate checks consistency; it is also used for manifests parsed elsewhere.
func (c ContainerRuntime) Validate() error {
	if err := setOf(c.SupportedPlatforms, platforms, "supportedPlatforms"); err != nil {
		return err
	}
	if err := setOf(c.DeploymentMethods, methods, "deploymentMethods"); err != nil {
		return err
	}
	if slices.Contains(c.DeploymentMethods, "kubernetes-manifest") != slices.Contains(c.SupportedPlatforms, "kubernetes") {
		return errors.New("kubernetes-manifest deployment and the kubernetes platform must be declared together")
	}
	if len(c.Capabilities) > 16 {
		return errors.New("at most 16 capabilities")
	}
	for _, cp := range c.Capabilities {
		if !capName.MatchString(cp) || strings.HasPrefix(cp, "CAP_") {
			return errors.New("capabilities must be Linux capability names without the CAP_ prefix")
		}
	}
	if len(c.Volumes) > 16 {
		return errors.New("at most 16 volumes")
	}
	seen := map[string]bool{}
	for _, v := range c.Volumes {
		if !volName.MatchString(v.Name) || seen[v.Name] || !path.IsAbs(v.Target) || path.Clean(v.Target) != v.Target || v.Target == "/" || len(v.Target) > 200 || (v.Access != "ro" && v.Access != "rw") {
			return errors.New("volumes need a unique name, a clean absolute target and access ro or rw")
		}
		seen[v.Name] = true
	}
	_, err := c.ImageSupport()
	return err
}

// ImageSupport converts the declaration into the resolver's ImageSupport.
// A non-configurable identity must be fixed: "not configurable" without a
// stated identity would leave the installer guessing.
func (c ContainerRuntime) ImageSupport() (ImageSupport, error) {
	d := c.Identity
	s := ImageSupport{SupplementalGroups: d.SupplementalGroups, UIDVariable: d.UIDVariable, GIDVariable: d.GIDVariable, FixedUID: d.FixedUID, FixedGID: d.FixedGID}
	switch d.Method {
	case "env":
		s.IdentityMethod = MethodEnv
	case "runtime-user":
		s.IdentityMethod = MethodRuntimeUser
	case "fixed":
		s.IdentityMethod = MethodFixed
	default:
		return s, errors.New("identity.method must be env, runtime-user or fixed")
	}
	if d.Configurable == (s.IdentityMethod == MethodFixed) {
		return s, errors.New("identity.configurable must be false exactly when identity.method is fixed")
	}
	if s.IdentityMethod != MethodEnv && (d.UIDVariable != "" || d.GIDVariable != "") {
		return s, errors.New("uidVariable/gidVariable are only valid with the env method")
	}
	if s.IdentityMethod != MethodFixed && (d.FixedUID != nil || d.FixedGID != nil) {
		return s, errors.New("fixedUid/fixedGid are only valid with the fixed method")
	}
	switch d.Umask.Method {
	case "env":
		s.UmaskMethod = UmaskEnv
	case "entrypoint":
		s.UmaskMethod = UmaskEntrypoint
	case "none":
		s.UmaskMethod = UmaskNone
	default:
		return s, errors.New("identity.umask.method must be env, entrypoint or none")
	}
	if s.UmaskMethod == UmaskNone && d.Umask.Variable != "" {
		return s, errors.New("identity.umask.variable is only valid with env or entrypoint")
	}
	s.UmaskVariable = d.Umask.Variable
	if s.IdentityMethod == MethodFixed && s.FixedUID != nil && *s.FixedUID == 0 {
		return s, errors.New("a fixed identity of UID 0 (root) is not accepted for HomeControl plugins")
	}
	return s, s.Validate()
}

// Volumes converts declared requirements into resolver volumes (without host
// metadata, which only a trusted host inspector may supply).
func (c ContainerRuntime) RequiredVolumes() []Volume {
	out := make([]Volume, 0, len(c.Volumes))
	for _, v := range c.Volumes {
		out = append(out, Volume{Name: v.Name, Target: v.Target, Access: Access(v.Access)})
	}
	return out
}
