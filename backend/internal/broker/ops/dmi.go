package ops

import (
	"context"
	"encoding/json"
	. "homelab-control/backend/internal/broker/types"
	"os"
	"strings"
)

var dmiRootOnly = []string{"product_serial", "product_uuid", "board_serial", "chassis_serial"}

// Vendors often leave these strings instead of real identifiers. They must
// not be presented as a hardware identity.
var dmiPlaceholders = []string{
	"to be filled by o.e.m.", "default string", "system serial number", "not specified",
	"not applicable", "none", "0123456789", "123456789", "00000000", "xxxxxxxx", "chassis serial number",
	"03000200-0400-0500-0006-000700080009", "00000000-0000-0000-0000-000000000000",
}

func isPlaceholder(v string) bool {
	l := strings.ToLower(strings.TrimSpace(v))
	if l == "" {
		return true
	}
	for _, p := range dmiPlaceholders {
		if l == p {
			return true
		}
	}
	return strings.Trim(l, "0- ") == "" // all zeros/dashes
}

func dmiIdentity(_ context.Context, env *Env, raw json.RawMessage) (any, error) {
	if err := requireNoArgs(raw); err != nil {
		return nil, err
	}
	dir := env.Roots.Path("/sys/class/dmi/id")
	out := DMIIdentity{Fields: map[string]DMIField{}, Source: "/sys/class/dmi/id"}
	if _, err := os.Stat(dir); err != nil {
		out.Reason = "No SMBIOS/DMI tables are exposed by this system (common on some VMs and ARM boards)."
		return out, nil
	}
	out.Available = true
	for _, f := range dmiRootOnly {
		b, err := readFixed(dir+"/"+f, 256)
		switch {
		case isNotExist(err):
			out.Fields[f] = DMIField{Status: "missing"}
		case err != nil:
			out.Fields[f] = DMIField{Status: "unreadable"}
		default:
			v := clean(string(b), 128)
			if isPlaceholder(v) {
				out.Fields[f] = DMIField{Value: v, Status: "placeholder"}
			} else {
				out.Fields[f] = DMIField{Value: v, Status: "present"}
			}
		}
	}
	return out, nil
}
