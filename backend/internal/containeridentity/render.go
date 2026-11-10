// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
// SPDX-License-Identifier: GPL-3.0-or-later
package containeridentity

import (
	"strconv"
)

// ComposeFragment holds only the Compose service keys HomeControl would set
// for Docker or Podman. Absent keys mean "leave the image default".
type ComposeFragment struct {
	Environment map[string]string `json:"environment,omitempty"`
	User        string            `json:"user,omitempty"`
	GroupAdd    []string          `json:"group_add,omitempty"`
}

// KubernetesFragment places each field at the scope the Kubernetes API
// defines: supplementalGroups exists only in the PodSecurityContext;
// runAsUser/runAsGroup are set per container so other containers in the pod
// keep their own identity.
type KubernetesFragment struct {
	PodSecurityContext       *PodSecurityContext       `json:"podSecurityContext,omitempty"`
	ContainerSecurityContext *ContainerSecurityContext `json:"containerSecurityContext,omitempty"`
	Env                      []EnvVar                  `json:"env,omitempty"`
}

type PodSecurityContext struct {
	SupplementalGroups []int64 `json:"supplementalGroups,omitempty"`
}

type ContainerSecurityContext struct {
	RunAsUser                *int64 `json:"runAsUser,omitempty"`
	RunAsGroup               *int64 `json:"runAsGroup,omitempty"`
	RunAsNonRoot             *bool  `json:"runAsNonRoot,omitempty"`
	AllowPrivilegeEscalation *bool  `json:"allowPrivilegeEscalation,omitempty"`
}

type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func u(v uint32) string { return strconv.FormatUint(uint64(v), 10) }

// render produces platform-native fragments from the resolved values only.
// Values dropped during validation (unsupported, blocked) never appear.
func render(p *Preview, req Request) {
	r := p.Resolved
	env := map[string]string{}
	img := req.Image
	idSet := r.UID.Source != LayerUnset && r.UID.Source != LayerImage || r.GID.Source != LayerUnset && r.GID.Source != LayerImage
	if idSet && img.IdentityMethod == MethodEnv {
		if r.UID.Value != nil && r.UID.Source != LayerImage {
			env[img.UIDVariable] = u(*r.UID.Value)
		}
		if r.GID.Value != nil && r.GID.Source != LayerImage {
			env[img.GIDVariable] = u(*r.GID.Value)
		}
	}
	if r.Umask.Value != nil {
		env[img.UmaskVariable] = r.Umask.Value.String()
	}
	runtimeUser := idSet && img.IdentityMethod == MethodRuntimeUser && r.UID.Value != nil && r.GID.Value != nil
	groups := r.SupplementalGroups.Source != LayerUnset

	switch req.Platform.Kind {
	case "docker", "podman":
		c := &ComposeFragment{}
		if len(env) > 0 {
			c.Environment = env
		}
		if runtimeUser {
			c.User = u(*r.UID.Value) + ":" + u(*r.GID.Value)
		}
		if groups {
			c.GroupAdd = make([]string, 0, len(r.SupplementalGroups.Value))
			for _, g := range r.SupplementalGroups.Value {
				c.GroupAdd = append(c.GroupAdd, u(g))
			}
		}
		if c.Environment != nil || c.User != "" || c.GroupAdd != nil {
			p.Compose = c
		}
	case "kubernetes":
		k := &KubernetesFragment{}
		if runtimeUser {
			ru, rg, t, f := int64(*r.UID.Value), int64(*r.GID.Value), true, false
			k.ContainerSecurityContext = &ContainerSecurityContext{RunAsUser: &ru, RunAsGroup: &rg, RunAsNonRoot: &t, AllowPrivilegeEscalation: &f}
		}
		if groups {
			sg := make([]int64, 0, len(r.SupplementalGroups.Value))
			for _, g := range r.SupplementalGroups.Value {
				sg = append(sg, int64(g))
			}
			k.PodSecurityContext = &PodSecurityContext{SupplementalGroups: sg}
		}
		for _, name := range sortedKeys(env) {
			k.Env = append(k.Env, EnvVar{Name: name, Value: env[name]})
		}
		if k.ContainerSecurityContext != nil || k.PodSecurityContext != nil || k.Env != nil {
			p.Kubernetes = k
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
