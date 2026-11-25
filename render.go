package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const maxTableCols = 8

// renderResource builds the full, human friendly view of a resource.
func (a *App) renderResource(ctx context.Context, res *Resource) fyne.CanvasObject {
	d := res.Data
	body := container.NewVBox()

	// ---- Header ----
	title := heading(resourceTitle(res), 24)
	head := container.NewVBox(title)
	sub := []string{}
	if t, ok := d["@odata.type"].(string); ok {
		sub = append(sub, prettyType(t))
	}
	sub = append(sub, res.Path)
	head.Add(mutedText(strings.Join(sub, "   ·   ")))
	if desc, ok := d["Description"].(string); ok && desc != "" && desc != resourceTitle(res) {
		head.Add(valueLabel(desc))
	}
	pills := container.NewHBox()
	if st, ok := d["Status"].(map[string]any); ok {
		for _, k := range []string{"Health", "HealthRollup", "State"} {
			if v, ok := st[k].(string); ok && v != "" {
				pills.Add(statusPill(humanize(k), v))
			}
		}
	}
	if ps, ok := d["PowerState"].(string); ok {
		pills.Add(statusPill("Power", ps))
	}
	if len(pills.Objects) > 0 {
		head.Add(pills)
	}
	body.Add(head)

	// ---- Collection members ----
	if members, ok := d["Members"].([]any); ok {
		body.Add(a.membersCard(ctx, "Members", members))
	}

	// ---- General scalar properties ----
	form := a.scalarForm(d, map[string]bool{"Name": true, "Description": true, "PowerState": true})
	if form != nil {
		body.Add(sectionCard("General", form))
	}

	// ---- Sub-resources (direct links) ----
	var subLinks []fyne.CanvasObject
	for _, l := range ChildLinks(res) {
		if l.IsMember {
			continue
		}
		subLinks = append(subLinks, linkButton(l.Name, l.Path, a.navigate))
	}
	if len(subLinks) > 0 {
		body.Add(sectionCard("Sub-resources", container.NewGridWrap(fyne.NewSize(250, 36), subLinks...)))
	}

	// ---- Nested objects and arrays ----
	for _, k := range sortedKeys(d) {
		v := d[k]
		switch k {
		case "Status", "Links", "Actions", "Members", "Oem":
			continue
		}
		if strings.HasPrefix(k, "@") || strings.Contains(k, "@odata") || isLinkOnly(v) {
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			if len(t) == 0 {
				continue
			}
			body.Add(sectionCard(humanize(k), a.renderObject(t, 1)))
		case []any:
			if len(t) == 0 || allScalars(t) {
				continue // shown in General
			}
			if allLinks(t) {
				body.Add(a.membersCard(ctx, humanize(k), t))
				continue
			}
			body.Add(sectionCard(fmt.Sprintf("%s (%d)", humanize(k), len(t)), a.renderArray(k, t, 1)))
		}
	}

	if links, ok := d["Links"].(map[string]any); ok {
		if o := a.renderLinks(links); o != nil {
			body.Add(sectionCard("Related", o))
		}
	}
	if acts, ok := d["Actions"].(map[string]any); ok && len(acts) > 0 {
		body.Add(sectionCard("Available Actions", a.renderActions(acts)))
	}
	if oem, ok := d["Oem"].(map[string]any); ok && len(oem) > 0 {
		body.Add(sectionCard("OEM / Vendor specific", a.renderObject(oem, 1)))
	}

	// ---- Raw JSON tab ----
	rawView, rawFind := a.rawJSONView(res)
	rawTab := container.NewTabItemWithIcon("Raw JSON", theme.FileTextIcon(), rawView)
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Details", theme.InfoIcon(), container.NewVScroll(container.NewPadded(body))),
		rawTab,
	)
	// Remember the chosen tab across navigation; Ctrl+F targets the visible find box.
	if a.rawTabActive {
		tabs.Select(rawTab)
		a.rawFind = rawFind
	} else {
		a.rawFind = nil
	}
	tabs.OnSelected = func(t *container.TabItem) {
		a.rawTabActive = t == rawTab
		a.rawFind = nil
		if a.rawTabActive {
			a.rawFind = rawFind
		}
	}
	return tabs
}

func allScalars(arr []any) bool {
	for _, e := range arr {
		if !isScalar(e) {
			return false
		}
	}
	return true
}

func allLinks(arr []any) bool {
	for _, e := range arr {
		if !isLinkOnly(e) {
			return false
		}
	}
	return true
}

// scalarForm renders scalar values (and scalar arrays, status, plain links) as a two column form.
func (a *App) scalarForm(m map[string]any, skip map[string]bool) fyne.CanvasObject {
	form := container.New(layout.NewFormLayout())
	for _, k := range sortedKeys(m) {
		if skip[k] || strings.HasPrefix(k, "@") || strings.Contains(k, "@odata") {
			continue
		}
		v := m[k]
		var val fyne.CanvasObject
		switch t := v.(type) {
		case map[string]any:
			if k == "Status" && len(skip) == 0 { // nested status objects inline as pills
				val = statusPills(t)
			}
		case []any:
			if len(t) > 0 && allScalars(t) {
				val = valueLabel(formatValue(k, t))
			}
		default:
			s := formatValue(k, v)
			if isStatusKey(k) {
				val = pillLeft(s, statusColor(s))
			} else {
				val = valueLabel(s)
			}
		}
		if val == nil {
			continue
		}
		// Redfish annotations like "ResetType@Redfish.AllowableValues" read better cleaned up.
		label := k
		if i := strings.Index(label, "@"); i > 0 {
			label = label[:i] + " (" + strings.TrimPrefix(label[i:], "@Redfish.") + ")"
		}
		form.Add(boldLabel(humanize(label)))
		form.Add(val)
	}
	if len(form.Objects) == 0 {
		return nil
	}
	return form
}

func statusPills(st map[string]any) fyne.CanvasObject {
	box := container.NewHBox()
	for _, k := range sortedKeys(st) {
		if s, ok := st[k].(string); ok && s != "" {
			box.Add(statusPill(humanize(k), s))
		}
	}
	return box
}

// renderObject renders a nested object: scalars as a form, deeper structures as indented sections.
func (a *App) renderObject(m map[string]any, depth int) fyne.CanvasObject {
	box := container.NewVBox()
	if f := a.scalarForm(m, nil); f != nil {
		box.Add(f)
	}
	var links []fyne.CanvasObject
	for _, k := range sortedKeys(m) {
		v := m[k]
		if strings.HasPrefix(k, "@") || strings.Contains(k, "@odata") || k == "Status" {
			continue
		}
		if id, ok := odataID(v); ok {
			links = append(links, linkButton(humanize(k), NormalizePath(id), a.navigate))
			if isLinkOnly(v) {
				continue
			}
		}
		switch t := v.(type) {
		case map[string]any:
			if len(t) == 0 {
				continue
			}
			box.Add(sectionTitle(humanize(k), depth))
			box.Add(indent(a.renderObject(t, depth+1), 14))
		case []any:
			if len(t) == 0 || allScalars(t) {
				continue
			}
			box.Add(sectionTitle(fmt.Sprintf("%s (%d)", humanize(k), len(t)), depth))
			box.Add(indent(a.renderArray(k, t, depth+1), 14))
		}
	}
	if len(links) > 0 {
		box.Add(container.NewGridWrap(fyne.NewSize(250, 36), links...))
	}
	if id, ok := m["@odata.id"].(string); ok && len(box.Objects) > 0 {
		box.Add(container.NewHBox(linkButton("Open "+lastSegment(id), NormalizePath(id), a.navigate)))
	}
	if len(box.Objects) == 0 {
		box.Add(mutedText("(empty)"))
	}
	return box
}

func sectionTitle(s string, depth int) fyne.CanvasObject {
	t := heading(s, theme.TextSize()+2-float32(min(depth, 2)))
	t.Color = theme.Color(theme.ColorNamePrimary)
	return container.New(layout.NewCustomPaddedLayout(6, 0, 0, 0), t)
}

// renderArray picks the best presentation for an array: link list, table, or cards.
func (a *App) renderArray(key string, arr []any, depth int) fyne.CanvasObject {
	if allScalars(arr) {
		return valueLabel(formatValue(key, arr))
	}
	if allLinks(arr) {
		var btns []fyne.CanvasObject
		for _, e := range arr {
			id, _ := odataID(e)
			p := NormalizePath(id)
			label := lastSegment(p)
			if r := a.client.Cached(p); r != nil {
				label = resourceTitle(r)
			}
			btns = append(btns, linkButton(label, p, a.navigate))
		}
		return container.NewGridWrap(fyne.NewSize(250, 36), btns...)
	}
	if cols, ok := tableColumns(arr); ok {
		return a.objectTable(key, arr, cols)
	}
	box := container.NewVBox()
	for i, e := range arr {
		name := fmt.Sprintf("#%d", i+1)
		if m, ok := e.(map[string]any); ok {
			if n, ok := m["Name"].(string); ok && n != "" {
				name += "  " + n
			} else if n, ok := m["MemberId"].(string); ok && n != "" {
				name += "  " + n
			}
			box.Add(sectionTitle(name, depth+1))
			box.Add(indent(a.renderObject(m, depth+1), 14))
		} else {
			box.Add(valueLabel(name + ": " + formatValue(key, e)))
		}
	}
	return box
}

type column struct {
	key, sub string // sub is set for flattened nested values, e.g. Status.Health
	title    string
}

// tableColumns decides whether an array of objects is "flat" enough for a table and
// returns the columns to show (most informative first).
func tableColumns(arr []any) ([]column, bool) {
	seen := map[string]column{}
	order := map[string]int{}
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, false
		}
		for k, v := range m {
			if strings.HasPrefix(k, "@") || strings.Contains(k, "@odata") || strings.Contains(k, "@Redfish") {
				continue
			}
			switch t := v.(type) {
			case map[string]any:
				if isLinkOnly(t) || k == "Oem" {
					continue
				}
				for sk, sv := range t {
					if strings.HasPrefix(sk, "@") {
						continue
					}
					if !isScalar(sv) {
						return nil, false
					}
					id := k + "." + sk
					title := humanize(sk)
					if k != "Status" {
						title = humanize(k) + " " + humanize(sk)
					}
					seen[id] = column{key: k, sub: sk, title: title}
				}
			case []any:
				if !allScalars(t) && !allLinks(t) {
					return nil, false
				}
				if allScalars(t) && len(t) > 0 {
					seen[k] = column{key: k, title: humanize(k)}
				}
			default:
				seen[k] = column{key: k, title: humanize(k)}
			}
		}
	}
	if len(seen) == 0 {
		return nil, false
	}
	cols := make([]column, 0, len(seen))
	for id, c := range seen {
		order[id] = columnRank(c)
		cols = append(cols, c)
	}
	sort.Slice(cols, func(i, j int) bool {
		ri, rj := columnRank(cols[i]), columnRank(cols[j])
		if ri != rj {
			return ri < rj
		}
		return cols[i].title < cols[j].title
	})
	return cols, true
}

func columnRank(c column) int {
	k := c.key
	if c.sub != "" {
		k = c.key + "." + c.sub
	}
	switch {
	case k == "Name":
		return 0
	case k == "MemberId" || k == "Id":
		return 1
	case strings.HasPrefix(k, "Reading") || k == "PowerConsumedWatts" || k == "Address":
		return 2
	case k == "Status.Health":
		return 3
	case k == "Status.State":
		return 4
	case k == "PhysicalContext" || k == "SubnetMask" || k == "Model":
		return 5
	case strings.Contains(k, "Critical") || k == "Gateway" || k == "SerialNumber" || k == "PowerCapacityWatts":
		return 6
	case strings.Contains(k, "Fatal") || strings.Contains(k, "Origin") || k == "FirmwareVersion":
		return 7
	case strings.Contains(k, "NonCritical") || strings.Contains(k, "Threshold"):
		return 8
	}
	return 10
}

func cellValue(m map[string]any, c column) (string, bool) {
	v := m[c.key]
	key := c.key
	if c.sub != "" {
		v = dig(m, c.key, c.sub)
		key = c.sub
	}
	if v == nil {
		return "—", false
	}
	return formatValue(key, v), isStatusKey(key)
}

// objectTable renders an array of flat objects as a table, with a details button per row.
func (a *App) objectTable(key string, arr []any, cols []column) fyne.CanvasObject {
	shown := cols
	if len(shown) > maxTableCols {
		shown = shown[:maxTableCols]
	}
	headers := make([]string, 0, len(shown)+1)
	for _, c := range shown {
		headers = append(headers, c.title)
	}
	headers = append(headers, "")
	rows := make([][]tableCell, 0, len(arr))
	for i, e := range arr {
		m := e.(map[string]any)
		row := make([]tableCell, 0, len(headers))
		for _, c := range shown {
			s, st := cellValue(m, c)
			row = append(row, tableCell{Text: s, Status: st})
		}
		title := fmt.Sprintf("%s #%d", humanize(key), i+1)
		if n, ok := m["Name"].(string); ok && n != "" {
			title = n
		}
		item := m
		btn := widget.NewButtonWithIcon("", theme.MoreHorizontalIcon(), func() { a.showDetails(title, item) })
		btn.Importance = widget.LowImportance
		row = append(row, tableCell{Obj: btn})
		rows = append(rows, row)
	}
	t := newTable(headers, rows)
	if len(cols) > len(shown) {
		return container.NewVBox(t, mutedText(fmt.Sprintf("%d more columns hidden — use the ⋯ button to see every field of a row", len(cols)-len(shown))))
	}
	return t
}

func (a *App) showDetails(title string, m map[string]any) {
	content := container.NewVScroll(container.NewPadded(a.renderObject(m, 1)))
	d := dialog.NewCustom(title, "Close", content, a.win)
	d.Resize(fyne.NewSize(640, 560))
	d.Show()
}

// renderLinks renders the "Links" section: cross references to related resources.
func (a *App) renderLinks(links map[string]any) fyne.CanvasObject {
	form := container.New(layout.NewFormLayout())
	for _, k := range sortedKeys(links) {
		if strings.HasPrefix(k, "@") || strings.Contains(k, "@odata") {
			continue
		}
		v := links[k]
		var btns []fyne.CanvasObject
		switch t := v.(type) {
		case map[string]any:
			if id, ok := odataID(t); ok {
				btns = append(btns, a.refButton(id))
			} else if k == "Oem" && len(t) > 0 {
				form.Add(boldLabel("OEM"))
				form.Add(a.renderObject(t, 2))
				continue
			}
		case []any:
			for _, e := range t {
				if id, ok := odataID(e); ok {
					btns = append(btns, a.refButton(id))
				}
			}
		}
		if len(btns) == 0 {
			continue
		}
		form.Add(boldLabel(humanize(k)))
		form.Add(container.NewGridWrap(fyne.NewSize(250, 36), btns...))
	}
	if len(form.Objects) == 0 {
		return nil
	}
	return form
}

func (a *App) refButton(id string) fyne.CanvasObject {
	p := NormalizePath(id)
	label := strings.TrimPrefix(p, serviceRoot+"/")
	if r := a.client.Cached(p); r != nil {
		label = resourceTitle(r)
	}
	return linkButton(label, p, a.navigate)
}

// renderActions lists the actions the resource supports (read-only browser: not invoked).
func (a *App) renderActions(acts map[string]any) fyne.CanvasObject {
	box := container.NewVBox()
	for _, k := range sortedKeys(acts) {
		m, ok := acts[k].(map[string]any)
		if !ok {
			continue
		}
		if k == "Oem" {
			box.Add(sectionTitle("OEM actions", 1))
			box.Add(indent(a.renderActions(m), 14))
			continue
		}
		name := strings.TrimPrefix(k, "#")
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		box.Add(sectionTitle(humanize(name), 1))
		form := container.New(layout.NewFormLayout())
		if t, ok := m["target"].(string); ok {
			form.Add(boldLabel("Target"))
			form.Add(valueLabel(t))
		}
		for _, pk := range sortedKeys(m) {
			if !strings.Contains(pk, "@Redfish.AllowableValues") {
				continue
			}
			form.Add(boldLabel(humanize(strings.SplitN(pk, "@", 2)[0])))
			form.Add(valueLabel(formatValue(pk, m[pk])))
		}
		box.Add(indent(form, 14))
	}
	return box
}

// membersCard shows a collection's members as a table, filling in names and health
// as each member is fetched in the background.
func (a *App) membersCard(ctx context.Context, title string, members []any) fyne.CanvasObject {
	holder := container.NewVBox()
	title = fmt.Sprintf("%s (%d)", title, len(members))
	if len(members) == 0 {
		holder.Add(mutedText("This collection is empty."))
		return sectionCard(title, holder)
	}

	type entry struct {
		path string
		data map[string]any
	}
	entries := make([]entry, len(members))
	expanded := false
	for i, m := range members {
		id, _ := odataID(m)
		entries[i] = entry{path: NormalizePath(id)}
		if mm, ok := m.(map[string]any); ok && !isLinkOnly(mm) {
			entries[i].data = mm // service already expanded the member
			expanded = true
		}
		if r := a.client.Cached(entries[i].path); r != nil {
			entries[i].data = r.Data
		}
	}

	// Expanded members that are flat (e.g. log entries) read best as a normal object table.
	if expanded {
		if cols, ok := tableColumns(members); ok {
			holder.Add(a.objectTable("Member", members, cols))
			return sectionCard(title, holder)
		}
	}

	build := func() {
		headers := []string{"Name", "Id", "Health", "State", "Info", ""}
		rows := make([][]tableCell, 0, len(entries))
		for _, e := range entries {
			name, id, health, state, info := lastSegment(e.path), "", "", "", ""
			if e.data != nil {
				if n, ok := e.data["Name"].(string); ok && n != "" {
					name = n
				}
				id, _ = e.data["Id"].(string)
				health, _ = dig(e.data, "Status", "Health").(string)
				state, _ = dig(e.data, "Status", "State").(string)
				info = memberInfo(e.data)
			}
			p := e.path
			open := widget.NewButtonWithIcon("Open", theme.NavigateNextIcon(), func() { a.navigate(p) })
			open.Importance = widget.LowImportance
			rows = append(rows, []tableCell{{Text: name}, {Text: id}, {Text: health, Status: true}, {Text: state, Status: true}, {Text: info}, {Obj: open}})
		}
		holder.Objects = []fyne.CanvasObject{newTable(headers, rows)}
		holder.Refresh()
	}
	build()

	const maxPrefetch = 200
	if len(entries) <= maxPrefetch {
		go func() {
			done := make(chan int, len(entries))
			pending := 0
			for i := range entries {
				if entries[i].data != nil {
					continue
				}
				pending++
				go func(i int) {
					if r, err := a.client.Get(ctx, entries[i].path, false); err == nil {
						fyne.Do(func() { entries[i].data = r.Data })
					}
					done <- i
				}(i)
			}
			for n := 0; n < pending; n++ {
				select {
				case <-done:
				case <-ctx.Done():
					return
				}
				// Rebuild in batches to keep the UI responsive.
				if n%8 == 7 || n == pending-1 {
					fyne.Do(func() {
						if ctx.Err() == nil {
							build()
							a.tree.Refresh()
						}
					})
				}
			}
		}()
	}
	return sectionCard(title, holder)
}

// memberInfo extracts a short, type specific summary for a collection row.
func memberInfo(d map[string]any) string {
	var parts []string
	add := func(label string, v any, key string) {
		if v == nil {
			return
		}
		s := formatValue(key, v)
		if s == "" || s == "—" {
			return
		}
		if label != "" {
			s = label + " " + s
		}
		parts = append(parts, s)
	}
	for _, k := range []string{"Model", "Manufacturer"} {
		add("", d[k], k)
	}
	add("", d["CapacityMiB"], "CapacityMiB")
	if b, ok := num(d, "CapacityBytes"); ok && b > 0 {
		parts = append(parts, strings.TrimSpace(strings.Trim(strings.TrimSpace(humanBytes(b)), "()")))
	}
	add("", d["TotalCores"], "")
	if _, ok := d["TotalCores"]; ok {
		parts[len(parts)-1] += " cores"
	}
	add("", d["MACAddress"], "")
	add("", d["SpeedMbps"], "SpeedMbps")
	add("FW", d["FirmwareVersion"], "")
	add("", d["Version"], "")
	add("", d["Created"], "")
	add("", d["Message"], "")
	s := strings.Join(parts, " · ")
	if len([]rune(s)) > 120 {
		s = string([]rune(s)[:120]) + "…"
	}
	return s
}
