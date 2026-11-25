package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Link is a navigable reference to another resource.
type Link struct {
	Name     string
	Path     string
	IsMember bool
}

// odataID returns the @odata.id of v if v is an object carrying one.
func odataID(v any) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", false
	}
	s, ok := m["@odata.id"].(string)
	return s, ok && s != ""
}

// isLinkOnly reports whether v is a pure reference ({"@odata.id": ...}).
func isLinkOnly(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := odataID(m); !ok {
		return false
	}
	for k := range m {
		if !strings.HasPrefix(k, "@") {
			return false
		}
	}
	return true
}

// ChildLinks returns the subordinate resources of r, used to build the navigation tree.
// Cross references under "Links" and fragment links are excluded.
func ChildLinks(r *Resource) []Link {
	self, _ := splitFragment(r.Path)
	seen := map[string]bool{self: true}
	var out []Link
	add := func(name, p string, member bool) {
		p = NormalizePath(p)
		if strings.Contains(p, "#") || seen[p] {
			return
		}
		// Skip references back up the hierarchy.
		if strings.HasPrefix(self, p+"/") {
			return
		}
		seen[p] = true
		out = append(out, Link{Name: name, Path: p, IsMember: member})
	}

	if members, ok := r.Data["Members"].([]any); ok {
		for _, m := range members {
			if id, ok := odataID(m); ok {
				add(lastSegment(id), id, true)
			}
		}
	}

	var walk func(prefix string, m map[string]any, depth int)
	walk = func(prefix string, m map[string]any, depth int) {
		for _, k := range sortedKeys(m) {
			if k == "Links" || k == "Members" || k == "Actions" || strings.HasPrefix(k, "@") || strings.Contains(k, "@odata") {
				continue
			}
			name := k
			if prefix != "" {
				name = prefix + " › " + k
			}
			obj, ok := m[k].(map[string]any)
			if !ok {
				continue
			}
			if id, ok := odataID(obj); ok {
				add(humanize(name), id, false)
				continue
			}
			if depth < 4 {
				walk(name, obj, depth+1)
			}
		}
	}
	walk("", r.Data, 0)
	return out
}

func lastSegment(p string) string {
	p, _ = splitFragment(p)
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// keyPriority orders the most informative properties first.
var keyPriority = map[string]int{
	"Id": 0, "Name": 1, "Description": 2, "Status": 3, "PowerState": 4,
	"Manufacturer": 5, "Model": 6, "SKU": 7, "SerialNumber": 8, "PartNumber": 9,
	"AssetTag": 10, "HostName": 11, "BiosVersion": 12, "FirmwareVersion": 13,
	"UUID": 14, "SystemType": 15, "ChassisType": 16, "ManagerType": 17,
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		pi, iok := keyPriority[keys[i]]
		pj, jok := keyPriority[keys[j]]
		switch {
		case iok && jok:
			return pi < pj
		case iok:
			return true
		case jok:
			return false
		}
		oi, oj := keys[i] == "Oem", keys[j] == "Oem"
		if oi != oj {
			return oj
		}
		return keys[i] < keys[j]
	})
	return keys
}

var acronyms = map[string]bool{
	"BIOS": true, "UUID": true, "CPU": true, "PCIe": true, "MAC": true, "IP": true, "IPv4": true,
	"IPv6": true, "BMC": true, "NIC": true, "SKU": true, "OEM": true, "URI": true, "TPM": true,
}

// humanize turns "SerialNumber" into "Serial Number" and "IPv4Addresses" into "IPv4 Addresses".
func humanize(s string) string {
	if s == "Oem" {
		return "OEM"
	}
	s = strings.TrimPrefix(s, "@")
	var b strings.Builder
	r := []rune(s)
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) {
			prev := r[i-1]
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if unicode.IsLower(prev) || (unicode.IsUpper(prev) && nextLower) || unicode.IsDigit(prev) && nextLower {
				b.WriteRune(' ')
			}
		}
		b.WriteRune(c)
	}
	out := b.String()
	// Re-glue common tokens the splitter breaks apart.
	for _, fix := range [][2]string{{"I Pv4", "IPv4"}, {"I Pv6", "IPv6"}, {"PC Ie", "PCIe"}, {"Ip V4", "IPv4"}, {"Ip V6", "IPv6"}, {"Mi B", "MiB"}, {"Gi B", "GiB"}, {"Gi Bs", "GiBs"}} {
		out = strings.ReplaceAll(out, fix[0], fix[1])
	}
	return out
}

func isScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

// formatValue renders a scalar for display, adding units where the key implies them.
func formatValue(key string, v any) string {
	switch t := v.(type) {
	case nil:
		return "—"
	case bool:
		if t {
			return "Yes"
		}
		return "No"
	case string:
		if t == "" {
			return "—"
		}
		return t
	case json.Number:
		return formatNumber(key, t)
	case float64:
		return formatNumber(key, json.Number(strconv.FormatFloat(t, 'f', -1, 64)))
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if isScalar(e) {
				parts = append(parts, formatValue("", e))
			}
		}
		if len(parts) == 0 {
			return "—"
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

func formatNumber(key string, n json.Number) string {
	f, err := n.Float64()
	if err != nil {
		return n.String()
	}
	s := n.String()
	if strings.Contains(s, ".") {
		s = strconv.FormatFloat(f, 'f', 2, 64)
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	k := key
	switch {
	case strings.HasSuffix(k, "MiB"):
		if f >= 1024 {
			return fmt.Sprintf("%s MiB  (%s GiB)", s, strconv.FormatFloat(f/1024, 'f', -1, 64))
		}
		return s + " MiB"
	case strings.HasSuffix(k, "GiB"):
		return s + " GiB"
	case strings.HasSuffix(k, "Bytes"):
		return s + humanBytes(f)
	case strings.HasSuffix(k, "Celsius") || k == "ReadingCelsius":
		return s + " °C"
	case strings.HasSuffix(k, "Watts") || strings.HasPrefix(k, "PowerConsumed") || strings.HasPrefix(k, "PowerCapacity") || k == "LimitInWatts":
		return s + " W"
	case strings.HasSuffix(k, "MHz"):
		return s + " MHz"
	case strings.HasSuffix(k, "Mbps"):
		return s + " Mbps"
	case strings.HasSuffix(k, "Gbps"):
		return s + " Gbps"
	case strings.HasSuffix(k, "Volts") || k == "ReadingVolts":
		return s + " V"
	case strings.HasSuffix(k, "Percent"):
		return s + " %"
	case strings.HasSuffix(k, "RPM"):
		return s + " RPM"
	case strings.HasSuffix(k, "Seconds"):
		return s + " s"
	}
	return s
}

func humanBytes(b float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for b >= 1000 && i < len(units)-1 {
		b /= 1000
		i++
	}
	if i == 0 {
		return ""
	}
	return fmt.Sprintf("  (%.1f %s)", b, units[i])
}

// prettyType turns "#ComputerSystem.v1_20_0.ComputerSystem" into "ComputerSystem v1.20.0".
func prettyType(t string) string {
	t = strings.TrimPrefix(t, "#")
	parts := strings.Split(t, ".")
	if len(parts) == 3 && strings.HasPrefix(parts[1], "v") {
		return parts[0] + "  " + strings.ReplaceAll(parts[1], "_", ".")
	}
	if len(parts) >= 1 {
		return parts[0]
	}
	return t
}

// resourceTitle picks the best human title for a resource.
func resourceTitle(r *Resource) string {
	if n, ok := r.Data["Name"].(string); ok && n != "" {
		if id, ok := r.Data["Id"].(string); ok && id != "" && id != n && !strings.Contains(n, id) {
			return n + " (" + id + ")"
		}
		return n
	}
	if id, ok := r.Data["Id"].(string); ok && id != "" {
		return id
	}
	if r.Path == serviceRoot {
		return "Service Root"
	}
	return lastSegment(r.Path)
}
