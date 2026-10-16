// Package hwinfo builds a read-only hardware inventory from world-readable
// sysfs, /dev/disk/by-id symlinks and the pci.ids database. It never executes
// programs and never writes.
//
// Drive identity follows CLAUDE.md: kernel names (sda, nvme0n1) are reported
// as volatile; identity comes from WWN, serial or by-id links, each labelled
// with its source and a confidence level.
package hwinfo

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Roots allows fixture trees in tests. Root "/" in production.
type Roots struct{ Root string }

func (r Roots) path(abs string) string {
	if r.Root == "" || r.Root == "/" {
		return abs
	}
	return filepath.Join(r.Root, abs)
}

// Meta mirrors hostinfo section metadata.
type Meta struct {
	Status  string   `json:"status"` // ok | partial | unavailable
	Sources []string `json:"sources"`
	Notes   []string `json:"notes,omitempty"`
}

func finish(m *Meta, haveData bool) {
	switch {
	case !haveData && len(m.Notes) > 0:
		m.Status = "unavailable"
	case len(m.Notes) > 0:
		m.Status = "partial"
	default:
		m.Status = "ok"
	}
}

// System holds world-readable DMI fields.
type System struct {
	Meta
	Vendor      string `json:"vendor,omitempty"`
	Product     string `json:"product,omitempty"`
	Version     string `json:"version,omitempty"`
	BoardVendor string `json:"board_vendor,omitempty"`
	BoardName   string `json:"board_name,omitempty"`
	BIOSVendor  string `json:"bios_vendor,omitempty"`
	BIOSVersion string `json:"bios_version,omitempty"`
	BIOSDate    string `json:"bios_date,omitempty"`
	ChassisType string `json:"chassis_type,omitempty"`
	Hypervisor  string `json:"hypervisor,omitempty"`
}

// PCIDevice is one PCI function.
type PCIDevice struct {
	Address    string `json:"address"`
	ClassCode  string `json:"class_code"`
	Class      string `json:"class"`
	VendorID   string `json:"vendor_id"`
	DeviceID   string `json:"device_id"`
	VendorName string `json:"vendor_name,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
	Driver     string `json:"driver,omitempty"`
	IOMMUGroup string `json:"iommu_group,omitempty"`
}

// USBDevice is one USB device (interfaces omitted).
type USBDevice struct {
	Bus          string `json:"bus_path"`
	VendorID     string `json:"vendor_id"`
	ProductID    string `json:"product_id"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
	SpeedMbps    string `json:"speed_mbps,omitempty"`
}

// Identifier is one stable identity candidate for a disk.
type Identifier struct {
	Kind   string `json:"kind"`   // wwn | serial | by-id
	Value  string `json:"value"`  //
	Source string `json:"source"` // file or link it was read from
}

// Disk is one block device.
type Disk struct {
	KernelName  string       `json:"kernel_name"`
	KernelNote  string       `json:"kernel_name_note"`
	SizeBytes   uint64       `json:"size_bytes"`
	Model       string       `json:"model,omitempty"`
	Vendor      string       `json:"vendor,omitempty"`
	Transport   string       `json:"transport"`
	Rotational  *bool        `json:"rotational,omitempty"`
	Removable   bool         `json:"removable"`
	ReadOnly    bool         `json:"read_only"`
	Virtual     bool         `json:"virtual"`
	Identifiers []Identifier `json:"identifiers"`
	Confidence  string       `json:"identity_confidence"` // high | medium | low | none
	Identity    string       `json:"identity,omitempty"`  // best identifier value
}

// Sensor is one hwmon reading.
type Sensor struct {
	Chip  string  `json:"chip"`
	Label string  `json:"label"`
	Kind  string  `json:"kind"` // temperature | fan
	Value float64 `json:"value"`
	Unit  string  `json:"unit"` // °C | RPM
}

// Inventory is GET /api/v1/hardware (unprivileged part).
type Inventory struct {
	System System `json:"system"`
	PCI    struct {
		Meta
		Devices []PCIDevice `json:"devices"`
	} `json:"pci"`
	USB struct {
		Meta
		Devices []USBDevice `json:"devices"`
	} `json:"usb"`
	Disks struct {
		Meta
		Devices []Disk `json:"devices"`
	} `json:"disks"`
	Sensors struct {
		Meta
		Readings []Sensor `json:"readings"`
	} `json:"sensors"`
}

// Collector builds inventories.
type Collector struct {
	Roots Roots
	ids   *pciIDs
	once  sync.Once
}

// New returns a collector for the live host.
func New() *Collector { return &Collector{Roots: Roots{Root: "/"}} }

func readStr(p string, max int64) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, max))
	return cleanStr(string(b))
}

func cleanStr(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	return s
}

// Collect gathers everything; failures are per section.
func (c *Collector) Collect() Inventory {
	var inv Inventory
	inv.System = c.system()
	inv.PCI.Devices, inv.PCI.Meta = c.pci()
	inv.USB.Devices, inv.USB.Meta = c.usb()
	inv.Disks.Devices, inv.Disks.Meta = c.disks()
	inv.Sensors.Readings, inv.Sensors.Meta = c.sensors()
	return inv
}

var chassisTypes = map[string]string{"1": "Other", "2": "Unknown", "3": "Desktop", "4": "Low-profile desktop", "6": "Mini tower", "7": "Tower", "8": "Portable", "9": "Laptop", "10": "Notebook", "13": "All-in-one", "17": "Main server chassis", "23": "Rack mount chassis", "24": "Sealed-case PC", "30": "Tablet", "31": "Convertible", "35": "Mini PC", "36": "Stick PC"}

func (c *Collector) system() System {
	s := System{}
	dir := c.Roots.path("/sys/class/dmi/id")
	s.Sources = []string{"/sys/class/dmi/id (world-readable fields)"}
	if _, err := os.Stat(dir); err != nil {
		s.Notes = append(s.Notes, "No SMBIOS/DMI tables are exposed by this system (common on some VMs and ARM boards).")
	} else {
		rd := func(n string) string { return readStr(filepath.Join(dir, n), 256) }
		s.Vendor, s.Product, s.Version = rd("sys_vendor"), rd("product_name"), rd("product_version")
		s.BoardVendor, s.BoardName = rd("board_vendor"), rd("board_name")
		s.BIOSVendor, s.BIOSVersion, s.BIOSDate = rd("bios_vendor"), rd("bios_version"), rd("bios_date")
		if ct := rd("chassis_type"); ct != "" {
			if n, ok := chassisTypes[ct]; ok {
				s.ChassisType = n
			} else {
				s.ChassisType = "Type " + ct
			}
		}
	}
	// /sys/hypervisor/type exists for Xen; the cpuinfo "hypervisor" flag covers KVM/others.
	if h := readStr(c.Roots.path("/sys/hypervisor/type"), 64); h != "" {
		s.Hypervisor = h
	} else if strings.Contains(readStr(c.Roots.path("/proc/cpuinfo"), 1<<20), " hypervisor") {
		s.Hypervisor = "present (virtual machine)"
	}
	s.Sources = append(s.Sources, "/proc/cpuinfo (hypervisor flag)")
	finish(&s.Meta, s.Vendor != "" || s.Product != "")
	return s
}

var pciClasses = map[string]string{
	"00": "Unclassified", "01": "Storage controller", "02": "Network controller", "03": "Display controller",
	"04": "Multimedia controller", "05": "Memory controller", "06": "Bridge", "07": "Communication controller",
	"08": "System peripheral", "09": "Input device", "0a": "Docking station", "0b": "Processor",
	"0c": "Serial bus controller", "0d": "Wireless controller", "0e": "Intelligent controller",
	"0f": "Satellite controller", "10": "Encryption controller", "11": "Signal processing", "12": "Processing accelerator",
	"13": "Non-essential instrumentation", "ff": "Unassigned",
}

var pciAddrRE = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)

func linkBase(p string) string {
	t, err := os.Readlink(p)
	if err != nil {
		return ""
	}
	return filepath.Base(t)
}

func (c *Collector) pci() ([]PCIDevice, Meta) {
	m := Meta{Sources: []string{"/sys/bus/pci/devices"}}
	base := c.Roots.path("/sys/bus/pci/devices")
	ents, err := os.ReadDir(base)
	if err != nil {
		m.Notes = append(m.Notes, "PCI bus not visible: "+err.Error())
		finish(&m, false)
		return []PCIDevice{}, m
	}
	ids := c.pciDB(&m)
	out := []PCIDevice{}
	for _, e := range ents {
		a := e.Name()
		if !pciAddrRE.MatchString(a) {
			continue
		}
		d := filepath.Join(base, a)
		cls := strings.TrimPrefix(readStr(filepath.Join(d, "class"), 16), "0x")
		v := strings.TrimPrefix(readStr(filepath.Join(d, "vendor"), 16), "0x")
		dev := strings.TrimPrefix(readStr(filepath.Join(d, "device"), 16), "0x")
		p := PCIDevice{Address: a, ClassCode: cls, VendorID: v, DeviceID: dev, Driver: linkBase(filepath.Join(d, "driver")), IOMMUGroup: linkBase(filepath.Join(d, "iommu_group"))}
		if len(cls) >= 2 {
			p.Class = pciClasses[cls[:2]]
		}
		if ids != nil {
			p.VendorName, p.DeviceName = ids.lookup(v, dev)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	finish(&m, len(out) > 0)
	return out, m
}

type pciIDs struct {
	vendors map[string]string
	devices map[string]string // vendor+device
}

func (p *pciIDs) lookup(v, d string) (string, string) {
	return p.vendors[v], p.devices[v+d]
}

// pciDB loads pci.ids once (it is ~1.5 MB). Missing database only removes names.
func (c *Collector) pciDB(m *Meta) *pciIDs {
	c.once.Do(func() {
		for _, p := range []string{"/usr/share/hwdata/pci.ids", "/usr/share/misc/pci.ids", "/usr/share/pci.ids"} {
			f, err := os.Open(c.Roots.path(p))
			if err != nil {
				continue
			}
			c.ids = parsePCIIDs(io.LimitReader(f, 8<<20))
			f.Close()
			break
		}
	})
	if c.ids == nil {
		m.Notes = append(m.Notes, "pci.ids not installed (package hwdata or pciutils); device names are omitted.")
	} else {
		m.Sources = append(m.Sources, "pci.ids")
	}
	return c.ids
}

func parsePCIIDs(r io.Reader) *pciIDs {
	ids := &pciIDs{vendors: map[string]string{}, devices: map[string]string{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	vendor := ""
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		if strings.HasPrefix(line, "C ") { // class section ends the vendor list
			break
		}
		switch {
		case line[0] != '\t' && len(line) > 6 && line[4] == ' ':
			vendor = strings.ToLower(line[:4])
			ids.vendors[vendor] = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "\t\t") && len(line) > 7 && vendor != "":
			ids.devices[vendor+strings.ToLower(line[1:5])] = strings.TrimSpace(line[7:])
		}
	}
	return ids
}

var usbDevRE = regexp.MustCompile(`^[0-9]+-[0-9.]+$|^usb[0-9]+$`)

func (c *Collector) usb() ([]USBDevice, Meta) {
	m := Meta{Sources: []string{"/sys/bus/usb/devices"}}
	base := c.Roots.path("/sys/bus/usb/devices")
	ents, err := os.ReadDir(base)
	if err != nil {
		m.Notes = append(m.Notes, "No USB bus is visible on this system.")
		finish(&m, false)
		return []USBDevice{}, m
	}
	out := []USBDevice{}
	for _, e := range ents {
		n := e.Name()
		if !usbDevRE.MatchString(n) {
			continue // skip interfaces (contain ':')
		}
		d := filepath.Join(base, n)
		out = append(out, USBDevice{Bus: n, VendorID: readStr(filepath.Join(d, "idVendor"), 8), ProductID: readStr(filepath.Join(d, "idProduct"), 8),
			Manufacturer: readStr(filepath.Join(d, "manufacturer"), 128), Product: readStr(filepath.Join(d, "product"), 128), SpeedMbps: readStr(filepath.Join(d, "speed"), 16)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bus < out[j].Bus })
	if len(out) == 0 {
		m.Notes = append(m.Notes, "No USB devices found.")
	}
	finish(&m, len(out) > 0)
	return out, m
}

var diskRE = regexp.MustCompile(`^(sd[a-z]+|nvme[0-9]+n[0-9]+|vd[a-z]+|xvd[a-z]+|hd[a-z]+|mmcblk[0-9]+|md[0-9]+|dm-[0-9]+)$`)

// byIDIndex maps kernel names to their /dev/disk/by-id link names.
func (c *Collector) byIDIndex(m *Meta) map[string][]string {
	idx := map[string][]string{}
	dir := c.Roots.path("/dev/disk/by-id")
	ents, err := os.ReadDir(dir)
	if err != nil {
		m.Notes = append(m.Notes, "/dev/disk/by-id is not available (no udev); identity relies on sysfs only.")
		return idx
	}
	m.Sources = append(m.Sources, "/dev/disk/by-id")
	for _, e := range ents {
		t := linkBase(filepath.Join(dir, e.Name()))
		if t != "" && !strings.Contains(e.Name(), "-part") {
			idx[t] = append(idx[t], e.Name())
		}
	}
	return idx
}

func virtualTransport(t string) bool { return t == "virtio" || t == "xen" || t == "virtual" }

func transportOf(name, devpath string) string {
	switch {
	case strings.HasPrefix(name, "nvme"):
		return "nvme"
	case strings.HasPrefix(name, "vd"):
		return "virtio"
	case strings.HasPrefix(name, "xvd"):
		return "xen"
	case strings.HasPrefix(name, "mmcblk"):
		return "mmc"
	case strings.HasPrefix(name, "md"), strings.HasPrefix(name, "dm-"):
		return "virtual"
	case strings.Contains(devpath, "/usb"):
		return "usb"
	case strings.Contains(devpath, "/ata"):
		return "sata"
	case strings.Contains(devpath, "/host") || strings.Contains(devpath, "/sas"):
		return "scsi/sas"
	}
	return "unknown"
}

func (c *Collector) disks() ([]Disk, Meta) {
	m := Meta{Sources: []string{"/sys/block"}}
	base := c.Roots.path("/sys/block")
	ents, err := os.ReadDir(base)
	if err != nil {
		m.Notes = append(m.Notes, "/sys/block not readable: "+err.Error())
		finish(&m, false)
		return []Disk{}, m
	}
	byID := c.byIDIndex(&m)
	out := []Disk{}
	for _, e := range ents {
		n := e.Name()
		if !diskRE.MatchString(n) {
			continue
		}
		d := filepath.Join(base, n)
		devpath, _ := filepath.EvalSymlinks(d)
		sectors, _ := strconv.ParseUint(readStr(filepath.Join(d, "size"), 32), 10, 64)
		dk := Disk{KernelName: n, KernelNote: "Kernel names can change between boots; use the identity below.", SizeBytes: sectors * 512,
			Model: readStr(filepath.Join(d, "device/model"), 128), Vendor: readStr(filepath.Join(d, "device/vendor"), 64),
			Removable: readStr(filepath.Join(d, "removable"), 4) == "1", ReadOnly: readStr(filepath.Join(d, "ro"), 4) == "1",
			Transport: transportOf(n, devpath), Identifiers: []Identifier{}}
		dk.Virtual = dk.Transport == "virtual"
		// The rotational flag is only meaningful for physical transports;
		// virtio-blk, Xen and device-mapper/md report a default, not media type.
		if r := readStr(filepath.Join(d, "queue/rotational"), 4); r != "" && !virtualTransport(dk.Transport) {
			rot := r == "1"
			dk.Rotational = &rot
		}
		for _, f := range []string{"wwid", "device/wwid"} {
			if v := readStr(filepath.Join(d, f), 256); v != "" {
				dk.Identifiers = append(dk.Identifiers, Identifier{Kind: "wwn", Value: v, Source: "/sys/block/" + n + "/" + f})
				break
			}
		}
		for _, f := range []string{"serial", "device/serial"} {
			if v := readStr(filepath.Join(d, f), 128); v != "" {
				dk.Identifiers = append(dk.Identifiers, Identifier{Kind: "serial", Value: v, Source: "/sys/block/" + n + "/" + f})
				break
			}
		}
		links := byID[n]
		sort.Strings(links)
		for _, l := range links {
			dk.Identifiers = append(dk.Identifiers, Identifier{Kind: "by-id", Value: l, Source: "/dev/disk/by-id"})
		}
		dk.Confidence, dk.Identity = rankIdentity(dk.Identifiers)
		out = append(out, dk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].KernelName < out[j].KernelName })
	if len(out) == 0 {
		m.Notes = append(m.Notes, "No disks found in /sys/block.")
	}
	finish(&m, len(out) > 0)
	return out, m
}

// rankIdentity picks the best identifier. WWN (world-wide, vendor-assigned)
// ranks highest; a serial is medium (unique per vendor/model, sometimes
// missing or truncated over USB bridges); by-id links alone are low because
// they are derived from the same data via udev rules and differ by bridge.
func rankIdentity(ids []Identifier) (string, string) {
	best, conf := "", "none"
	rank := map[string]int{"wwn": 3, "serial": 2, "by-id": 1}
	top := 0
	for _, id := range ids {
		if r := rank[id.Kind]; r > top {
			top, best = r, id.Value
		}
	}
	switch top {
	case 3:
		conf = "high"
	case 2:
		conf = "medium"
	case 1:
		conf = "low"
	}
	return conf, best
}

var hwmonInputRE = regexp.MustCompile(`^(temp|fan)([0-9]+)_input$`)

func (c *Collector) sensors() ([]Sensor, Meta) {
	m := Meta{Sources: []string{"/sys/class/hwmon"}}
	base := c.Roots.path("/sys/class/hwmon")
	ents, err := os.ReadDir(base)
	if err != nil || len(ents) == 0 {
		m.Notes = append(m.Notes, "No hardware monitoring chips are exposed (no hwmon drivers loaded, or a VM).")
		finish(&m, false)
		return []Sensor{}, m
	}
	out := []Sensor{}
	for _, e := range ents {
		d := filepath.Join(base, e.Name())
		chip := readStr(filepath.Join(d, "name"), 64)
		files, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, f := range files {
			mm := hwmonInputRE.FindStringSubmatch(f.Name())
			if mm == nil {
				continue
			}
			raw, err := strconv.ParseFloat(readStr(filepath.Join(d, f.Name()), 32), 64)
			if err != nil {
				continue // unreadable or disabled channel
			}
			label := readStr(filepath.Join(d, mm[1]+mm[2]+"_label"), 64)
			if label == "" {
				label = mm[1] + mm[2]
			}
			s := Sensor{Chip: chip, Label: label}
			if mm[1] == "temp" {
				s.Kind, s.Value, s.Unit = "temperature", raw/1000, "°C"
			} else {
				s.Kind, s.Value, s.Unit = "fan", raw, "RPM"
			}
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Chip != out[j].Chip {
			return out[i].Chip < out[j].Chip
		}
		return out[i].Label < out[j].Label
	})
	if len(out) == 0 {
		m.Notes = append(m.Notes, "Hardware monitoring chips found, but no readable temperature or fan inputs.")
	}
	finish(&m, len(out) > 0)
	return out, m
}
