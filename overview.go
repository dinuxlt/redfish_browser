package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (a *App) showOverview(force bool) {
	ctx, seq := a.beginNav()
	a.setContent(a.loadingView("server overview"))
	go func() {
		data := a.gatherOverview(ctx, force)
		fyne.Do(func() {
			if seq != a.navSeq {
				return
			}
			a.setContent(a.renderOverview(data))
			a.tree.Refresh()
		})
	}()
}

type chassisInfo struct {
	res     *Resource
	thermal *Resource
	power   *Resource
}

type overviewData struct {
	root     *Resource
	systems  []*Resource
	chassis  []chassisInfo
	managers []*Resource
	err      error
}

// members fetches every member of a collection in parallel.
func (a *App) members(ctx context.Context, coll string, force bool) []*Resource {
	c, err := a.client.Get(ctx, coll, force)
	if err != nil {
		return nil
	}
	ms, _ := c.Data["Members"].([]any)
	out := make([]*Resource, len(ms))
	var wg sync.WaitGroup
	for i, m := range ms {
		id, ok := odataID(m)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			if r, err := a.client.Get(ctx, p, force); err == nil {
				out[i] = r
			}
		}(i, NormalizePath(id))
	}
	wg.Wait()
	res := out[:0]
	for _, r := range out {
		if r != nil {
			res = append(res, r)
		}
	}
	return res
}

func (a *App) gatherOverview(ctx context.Context, force bool) *overviewData {
	d := &overviewData{}
	root, err := a.client.Get(ctx, serviceRoot, force)
	if err != nil {
		d.err = err
		return d
	}
	d.root = root
	link := func(key string) string {
		if id, ok := odataID(root.Data[key]); ok {
			return NormalizePath(id)
		}
		return ""
	}

	var wg sync.WaitGroup
	if p := link("Systems"); p != "" {
		wg.Add(1)
		go func() { defer wg.Done(); d.systems = a.members(ctx, p, force) }()
	}
	if p := link("Managers"); p != "" {
		wg.Add(1)
		go func() { defer wg.Done(); d.managers = a.members(ctx, p, force) }()
	}
	if p := link("Chassis"); p != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, c := range a.members(ctx, p, force) {
				ci := chassisInfo{res: c}
				if id, ok := odataID(c.Data["Thermal"]); ok {
					ci.thermal, _ = a.client.Get(ctx, id, force)
				}
				if id, ok := odataID(c.Data["Power"]); ok {
					ci.power, _ = a.client.Get(ctx, id, force)
				}
				d.chassis = append(d.chassis, ci)
			}
		}()
	}
	wg.Wait()
	return d
}

func str(m map[string]any, keys ...string) string {
	if s, ok := dig(m, keys...).(string); ok {
		return strings.TrimSpace(s)
	}
	if n, ok := dig(m, keys...).(json.Number); ok {
		return n.String()
	}
	return ""
}

func num(m map[string]any, keys ...string) (float64, bool) {
	switch v := dig(m, keys...).(type) {
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case float64:
		return v, true
	}
	return 0, false
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func (a *App) renderOverview(d *overviewData) fyne.CanvasObject {
	if d.err != nil {
		return a.errorView(serviceRoot, d.err, func() { a.showOverview(true) })
	}
	body := container.NewVBox()

	rd := d.root.Data
	body.Add(heading("Server Overview", 26))
	body.Add(mutedText(joinNonEmpty("   ·   ",
		joinNonEmpty(" ", str(rd, "Vendor"), str(rd, "Product")),
		"Redfish "+str(rd, "RedfishVersion"),
		a.client.Base)))

	// Power usage and temperature summary for tiles.
	var watts float64
	var haveWatts bool
	maxTemp, haveTemp := 0.0, false
	for _, c := range d.chassis {
		if c.power != nil {
			if pcs, ok := c.power.Data["PowerControl"].([]any); ok && len(pcs) > 0 {
				if pc, ok := pcs[0].(map[string]any); ok {
					if w, ok := num(pc, "PowerConsumedWatts"); ok {
						watts += w
						haveWatts = true
					}
				}
			}
		}
		if c.thermal != nil {
			for _, t := range arrayOf(c.thermal.Data, "Temperatures") {
				if v, ok := num(t, "ReadingCelsius"); ok && v > maxTemp {
					maxTemp, haveTemp = v, true
				}
			}
		}
	}

	for _, s := range d.systems {
		body.Add(a.systemCard(s, watts, haveWatts, maxTemp, haveTemp))
	}
	if len(d.systems) == 0 {
		body.Add(sectionCard("Systems", mutedText("No computer systems were reported by this service.")))
	}

	if len(d.managers) > 0 {
		rows := [][]tableCell{}
		for _, m := range d.managers {
			md := m.Data
			p := m.Path
			rows = append(rows, []tableCell{
				{Text: resourceTitle(m)}, {Text: str(md, "ManagerType")}, {Text: str(md, "Model")},
				{Text: str(md, "FirmwareVersion")}, {Text: str(md, "Status", "Health"), Status: true},
				{Text: str(md, "PowerState"), Status: true}, {Obj: openBtn(p, a.navigate)},
			})
		}
		body.Add(sectionCard("Management Controllers (BMC)", newTable(
			[]string{"Name", "Type", "Model", "Firmware", "Health", "Power", ""}, rows)))
	}

	if len(d.chassis) > 0 {
		rows := [][]tableCell{}
		for _, c := range d.chassis {
			cd := c.res.Data
			rows = append(rows, []tableCell{
				{Text: resourceTitle(c.res)}, {Text: str(cd, "ChassisType")},
				{Text: joinNonEmpty(" ", str(cd, "Manufacturer"), str(cd, "Model"))},
				{Text: str(cd, "SerialNumber")}, {Text: str(cd, "Status", "Health"), Status: true},
				{Text: str(cd, "PowerState"), Status: true}, {Obj: openBtn(c.res.Path, a.navigate)},
			})
		}
		body.Add(sectionCard("Chassis", newTable(
			[]string{"Name", "Type", "Model", "Serial", "Health", "Power", ""}, rows)))
	}

	for _, c := range d.chassis {
		if c.thermal != nil {
			if o := a.temperatureCard(c); o != nil {
				body.Add(o)
			}
			if o := a.fanCard(c); o != nil {
				body.Add(o)
			}
		}
		if c.power != nil {
			if o := a.powerCard(c); o != nil {
				body.Add(o)
			}
		}
	}

	return container.NewVScroll(container.NewPadded(body))
}

func openBtn(p string, nav func(string)) fyne.CanvasObject {
	b := widget.NewButtonWithIcon("Open", theme.NavigateNextIcon(), func() { nav(p) })
	b.Importance = widget.LowImportance
	return b
}

func arrayOf(m map[string]any, key string) []map[string]any {
	arr, _ := m[key].([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if mm, ok := e.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}

func (a *App) systemCard(s *Resource, watts float64, haveWatts bool, maxTemp float64, haveTemp bool) fyne.CanvasObject {
	sd := s.Data
	title := joinNonEmpty(" ", str(sd, "Manufacturer"), str(sd, "Model"))
	if title == "" {
		title = resourceTitle(s)
	}
	box := container.NewVBox(heading(title, 22))
	box.Add(mutedText(joinNonEmpty("   ·   ",
		labelled("Serial", str(sd, "SerialNumber")),
		labelled("Host", str(sd, "HostName")),
		labelled("SKU", str(sd, "SKU")),
		labelled("Asset tag", str(sd, "AssetTag")))))

	pills := container.NewHBox()
	if h := str(sd, "Status", "HealthRollup"); h != "" {
		pills.Add(statusPill("Health", h))
	} else if h := str(sd, "Status", "Health"); h != "" {
		pills.Add(statusPill("Health", h))
	}
	if p := str(sd, "PowerState"); p != "" {
		pills.Add(statusPill("Power", p))
	}
	if b := str(sd, "Boot", "BootSourceOverrideTarget"); b != "" && b != "None" {
		pills.Add(statusPill("Boot override", b))
	}
	box.Add(pills)

	var tiles []fyne.CanvasObject
	if n, ok := num(sd, "ProcessorSummary", "Count"); ok {
		model := str(sd, "ProcessorSummary", "Model")
		if model == "" {
			model = "Processors"
		}
		if cores, ok := num(sd, "ProcessorSummary", "CoreCount"); ok {
			model += fmt.Sprintf(" · %.0f cores", cores)
		}
		tiles = append(tiles, newTile(theme.ViewFullScreenIcon(), fmt.Sprintf("%.0f × CPU", n), model, colBlue))
	}
	if gib, ok := num(sd, "MemorySummary", "TotalSystemMemoryGiB"); ok {
		tiles = append(tiles, newTile(theme.GridIcon(), fmt.Sprintf("%.0f GiB", gib), "Total memory", colGreen))
	}
	if haveWatts {
		tiles = append(tiles, newTile(theme.WarningIcon(), fmt.Sprintf("%.0f W", watts), "Power consumption", colOrange))
	}
	if haveTemp {
		accent := colGreen
		if maxTemp >= 75 {
			accent = colRed
		} else if maxTemp >= 60 {
			accent = colOrange
		}
		tiles = append(tiles, newTile(theme.WarningIcon(), fmt.Sprintf("%.0f °C", maxTemp), "Hottest sensor", accent))
	}
	if v := str(sd, "BiosVersion"); v != "" {
		tiles = append(tiles, newTile(theme.FileTextIcon(), v, "BIOS version", colGrey))
	}
	if len(tiles) > 0 {
		box.Add(tileGrid(tiles))
	}

	form := a.scalarForm(map[string]any{
		"SystemType":    sd["SystemType"],
		"UUID":          sd["UUID"],
		"IndicatorLED":  sd["IndicatorLED"],
		"LastResetTime": sd["LastResetTime"],
	}, map[string]bool{})
	if form != nil {
		box.Add(form)
	}

	var links []fyne.CanvasObject
	links = append(links, linkButton("Open system", s.Path, a.navigate))
	for _, k := range []string{"Processors", "Memory", "Storage", "EthernetInterfaces", "NetworkInterfaces", "Bios", "LogServices", "SecureBoot"} {
		if id, ok := odataID(sd[k]); ok {
			links = append(links, linkButton(humanize(k), NormalizePath(id), a.navigate))
		}
	}
	box.Add(container.NewGridWrap(fyne.NewSize(200, 36), links...))
	return widget.NewCard("", "", box)
}

func labelled(label, v string) string {
	if v == "" {
		return ""
	}
	return label + ": " + v
}

// gauge is a progress bar showing a reading against its critical threshold.
func gauge(value, maxv float64, text string) fyne.CanvasObject {
	pb := widget.NewProgressBar()
	pb.Max = maxv
	pb.SetValue(min(value, maxv))
	pb.TextFormatter = func() string { return text }
	return container.NewGridWrap(fyne.NewSize(220, pb.MinSize().Height), pb)
}

func (a *App) temperatureCard(c chassisInfo) fyne.CanvasObject {
	temps := arrayOf(c.thermal.Data, "Temperatures")
	if len(temps) == 0 {
		return nil
	}
	rows := [][]tableCell{}
	for _, t := range temps {
		v, ok := num(t, "ReadingCelsius")
		if !ok {
			continue
		}
		crit, hasCrit := num(t, "UpperThresholdCritical")
		maxv := 100.0
		if hasCrit && crit > 0 {
			maxv = crit
		}
		thr := "—"
		if hasCrit {
			thr = fmt.Sprintf("%.0f °C", crit)
		}
		rows = append(rows, []tableCell{
			{Text: str(t, "Name")}, {Obj: gauge(v, maxv, fmt.Sprintf("%.0f °C", v))},
			{Text: thr}, {Text: str(t, "PhysicalContext")},
			{Text: str(t, "Status", "Health"), Status: true},
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return sectionCard("Temperatures — "+resourceTitle(c.res), newTable(
		[]string{"Sensor", "Reading", "Critical", "Location", "Health"}, rows))
}

func (a *App) fanCard(c chassisInfo) fyne.CanvasObject {
	fans := arrayOf(c.thermal.Data, "Fans")
	if len(fans) == 0 {
		return nil
	}
	rows := [][]tableCell{}
	for _, f := range fans {
		name := str(f, "Name")
		if name == "" {
			name = str(f, "FanName")
		}
		reading := "—"
		if v, ok := num(f, "Reading"); ok {
			units := str(f, "ReadingUnits")
			if units == "" {
				units = "RPM"
			}
			reading = fmt.Sprintf("%.0f %s", v, units)
		}
		rows = append(rows, []tableCell{
			{Text: name}, {Text: reading}, {Text: str(f, "PhysicalContext")},
			{Text: str(f, "Status", "State"), Status: true},
			{Text: str(f, "Status", "Health"), Status: true},
		})
	}
	return sectionCard("Fans — "+resourceTitle(c.res), newTable(
		[]string{"Fan", "Speed", "Location", "State", "Health"}, rows))
}

func (a *App) powerCard(c chassisInfo) fyne.CanvasObject {
	psus := arrayOf(c.power.Data, "PowerSupplies")
	if len(psus) == 0 {
		return nil
	}
	rows := [][]tableCell{}
	for _, p := range psus {
		w := func(k string) string {
			if v, ok := num(p, k); ok {
				return fmt.Sprintf("%.0f W", v)
			}
			return "—"
		}
		rows = append(rows, []tableCell{
			{Text: str(p, "Name")}, {Text: joinNonEmpty(" ", str(p, "Manufacturer"), str(p, "Model"))},
			{Text: w("PowerCapacityWatts")}, {Text: w("PowerInputWatts")}, {Text: w("PowerOutputWatts")},
			{Text: str(p, "FirmwareVersion")},
			{Text: str(p, "Status", "State"), Status: true},
			{Text: str(p, "Status", "Health"), Status: true},
		})
	}
	return sectionCard("Power Supplies — "+resourceTitle(c.res), newTable(
		[]string{"PSU", "Model", "Capacity", "Input", "Output", "Firmware", "State", "Health"}, rows))
}
