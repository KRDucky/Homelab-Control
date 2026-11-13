// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package storagecap

// PlanItem is one package in a preview-only install plan.
type PlanItem struct {
	ID        string `json:"id"`
	Package   string `json:"package"`
	Required  bool   `json:"required"`
	Installed bool   `json:"installed"`
	Query     string `json:"query"`
}

// Plan is a user-visible install preview. Executes is always false until
// ADR 0014 / packages.manage exists. The honest action is Software Center
// search (/software?q=).
type Plan struct {
	Family   string     `json:"family"`
	Items    []PlanItem `json:"items"`
	Executes bool       `json:"executes"`
	Reason   string     `json:"reason"`
}

const (
	planNoExecute = "ADR 0014 package transactions are not implemented; Software Center search only."
	planNoPackage = "No package name for this family; no install offer."
)

// PlanFromTooling builds a preview. Missing required tools with a package
// name get a /software?q= query. Empty package names produce no query.
func PlanFromTooling(t Tooling) Plan {
	p := Plan{Family: t.Family, Executes: false, Reason: planNoExecute}
	add := func(it ToolItem, required bool) {
		item := PlanItem{ID: it.ID, Package: it.Package, Required: required, Installed: it.Installed}
		if !it.Installed && it.Package != "" {
			item.Query = "/software?q=" + it.Package
		}
		if !it.Installed && it.Package == "" {
			item.Query = ""
		}
		p.Items = append(p.Items, item)
	}
	for _, it := range t.Required {
		add(it, true)
	}
	for _, it := range t.Optional {
		add(it, false)
	}
	if !KnownFamily(t.Family) {
		p.Reason = planNoPackage
	}
	return p
}
