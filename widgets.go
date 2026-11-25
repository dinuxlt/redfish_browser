package main

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var (
	colGreen  = color.NRGBA{R: 0x22, G: 0xa5, B: 0x5a, A: 0xff}
	colOrange = color.NRGBA{R: 0xe8, G: 0x8a, B: 0x0b, A: 0xff}
	colRed    = color.NRGBA{R: 0xe0, G: 0x3e, B: 0x3e, A: 0xff}
	colGrey   = color.NRGBA{R: 0x8a, G: 0x93, B: 0xa0, A: 0xff}
	colBlue   = color.NRGBA{R: 0x3b, G: 0x82, B: 0xf6, A: 0xff}
)

// statusColor maps Redfish Health / State / PowerState values to a colour.
func statusColor(v string) color.NRGBA {
	switch strings.ToLower(v) {
	case "ok", "enabled", "on", "up", "linkup", "connected", "operable", "completed", "true", "yes", "present":
		return colGreen
	case "warning", "degraded", "starting", "updating", "poweringon", "poweringoff", "inTest", "standbyspare", "quiesced", "deferring", "intest":
		return colOrange
	case "critical", "error", "failed", "down", "linkdown", "unavailableoffline", "exception", "killed":
		return colRed
	case "off", "disabled", "absent", "standbyoffline", "nolink", "unknown", "", "—", "no", "false":
		return colGrey
	}
	return colBlue
}

func isStatusKey(k string) bool {
	switch k {
	case "Health", "HealthRollup", "State", "PowerState", "LinkStatus", "InterfaceEnabled", "Severity":
		return true
	}
	return false
}

// newPill renders a small rounded status badge.
func newPill(text string, col color.NRGBA) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.NRGBA{R: col.R, G: col.G, B: col.B, A: 0x30})
	bg.StrokeColor = col
	bg.StrokeWidth = 1
	bg.CornerRadius = 9
	t := canvas.NewText(text, col)
	t.TextStyle.Bold = true
	t.TextSize = theme.TextSize() - 1
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(2, 2, 9, 9), t))
}

// pillLeft keeps a pill at its natural width instead of stretching.
func pillLeft(text string, col color.NRGBA) fyne.CanvasObject {
	return container.NewHBox(newPill(text, col))
}

func statusPill(label, value string) fyne.CanvasObject {
	txt := value
	if label != "" {
		txt = label + ": " + value
	}
	return newPill(txt, statusColor(value))
}

func boldLabel(s string) *widget.Label {
	l := widget.NewLabelWithStyle(s, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return l
}

func valueLabel(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Wrapping = fyne.TextWrapWord
	l.Selectable = true
	return l
}

func heading(s string, size float32) *canvas.Text {
	t := canvas.NewText(s, theme.Color(theme.ColorNameForeground))
	t.TextSize = size
	t.TextStyle.Bold = true
	return t
}

func mutedText(s string) *canvas.Text {
	t := canvas.NewText(s, theme.Color(theme.ColorNamePlaceHolder))
	t.TextSize = theme.TextSize() - 1
	return t
}

func indent(o fyne.CanvasObject, px float32) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(0, 0, px, 0), o)
}

// linkButton is a flat button that navigates to a Redfish path.
func linkButton(label, path string, nav func(string)) *widget.Button {
	b := widget.NewButtonWithIcon(label, iconFor(label, path), func() { nav(path) })
	b.Importance = widget.LowImportance
	b.Alignment = widget.ButtonAlignLeading
	return b
}

// iconFor picks a theme icon that loosely matches a resource. Members of a collection
// ("/Systems/1") fall back to the collection's icon.
func iconFor(name, path string) fyne.Resource {
	if path == serviceRoot {
		return theme.ListIcon()
	}
	if ic := iconMatch(strings.ToLower(name + " " + lastSegment(path))); ic != nil {
		return ic
	}
	base, _ := splitFragment(path)
	if i := strings.LastIndex(base, "/"); i > 0 {
		if ic := iconMatch(strings.ToLower(lastSegment(base[:i]))); ic != nil {
			return ic
		}
	}
	return theme.DocumentIcon()
}

func iconMatch(s string) fyne.Resource {
	switch {
	case strings.Contains(s, "overview"):
		return theme.HomeIcon()
	case strings.Contains(s, "bios"), strings.Contains(s, "registr"), strings.Contains(s, "schema"), strings.Contains(s, "json"):
		return theme.FileTextIcon()
	case strings.Contains(s, "system"):
		return theme.ComputerIcon()
	case strings.Contains(s, "chassis"):
		return theme.DesktopIcon()
	case strings.Contains(s, "manager"):
		return theme.SettingsIcon()
	case strings.Contains(s, "storage"), strings.Contains(s, "drive"), strings.Contains(s, "volume"), strings.Contains(s, "disk"):
		return theme.StorageIcon()
	case strings.Contains(s, "account"), strings.Contains(s, "role"), strings.Contains(s, "session"):
		return theme.AccountIcon()
	case strings.Contains(s, "log"), strings.Contains(s, "entries"), strings.Contains(s, "task"):
		return theme.HistoryIcon()
	case strings.Contains(s, "event"), strings.Contains(s, "subscription"):
		return theme.MailSendIcon()
	case strings.Contains(s, "update"), strings.Contains(s, "firmware"), strings.Contains(s, "inventory"):
		return theme.UploadIcon()
	case strings.Contains(s, "ethernet"), strings.Contains(s, "network"), strings.Contains(s, "port"), strings.Contains(s, "fabric"), strings.Contains(s, "nic"):
		return theme.GridIcon()
	case strings.Contains(s, "power"), strings.Contains(s, "thermal"), strings.Contains(s, "sensor"), strings.Contains(s, "fan"):
		return theme.WarningIcon()
	case strings.Contains(s, "processor"), strings.Contains(s, "cpu"), strings.Contains(s, "memory"), strings.Contains(s, "dimm"), strings.Contains(s, "pcie"):
		return theme.ViewFullScreenIcon()
	}
	return nil
}

// sectionCard wraps content in a card with a title.
func sectionCard(title string, content fyne.CanvasObject) fyne.CanvasObject {
	return widget.NewCard("", "", container.NewVBox(heading(title, theme.TextSize()+3), widget.NewSeparator(), content))
}

// ---------- Auto-sizing table ----------

// tableLayout lays out row backgrounds followed by cells (row-major). Column widths
// follow the content but shrink proportionally when there is not enough room, so the
// table never needs a horizontal scrollbar.
type tableLayout struct {
	rows, cols int
	pref       []float32 // preferred width per column
}

func (t *tableLayout) widths(avail float32) []float32 {
	gap := theme.Padding()
	avail -= gap * float32(t.cols-1)
	w := make([]float32, t.cols)
	var sum float32
	for i, p := range t.pref {
		w[i] = p
		sum += p
	}
	if sum <= avail || sum == 0 {
		// Give leftover space to the last column so row backgrounds look continuous.
		return w
	}
	// Water-fill: narrow columns keep their size, wide ones share what is left.
	fixed := make([]bool, t.cols)
	remaining := avail
	left := t.cols
	for changed := true; changed; {
		changed = false
		fair := remaining / float32(left)
		for i := range w {
			if !fixed[i] && t.pref[i] <= fair {
				fixed[i] = true
				remaining -= t.pref[i]
				left--
				changed = true
			}
		}
		if left == 0 {
			break
		}
	}
	var wideSum float32
	for i := range w {
		if !fixed[i] {
			wideSum += t.pref[i]
		}
	}
	for i := range w {
		if !fixed[i] {
			w[i] = remaining * t.pref[i] / wideSum
		}
	}
	return w
}

func (t *tableLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	gap := theme.Padding()
	w := t.widths(size.Width)
	bgs, cells := objs[:t.rows], objs[t.rows:]
	var y float32
	for r := 0; r < t.rows; r++ {
		var h float32
		for c := 0; c < t.cols; c++ {
			h = max(h, cells[r*t.cols+c].MinSize().Height)
		}
		x := float32(0)
		for c := 0; c < t.cols; c++ {
			o := cells[r*t.cols+c]
			o.Move(fyne.NewPos(x, y))
			o.Resize(fyne.NewSize(w[c], h))
			x += w[c] + gap
		}
		bgs[r].Move(fyne.NewPos(0, y))
		bgs[r].Resize(fyne.NewSize(size.Width, h))
		y += h
	}
}

func (t *tableLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	cells := objs[t.rows:]
	var h float32
	for r := 0; r < t.rows; r++ {
		var rh float32
		for c := 0; c < t.cols; c++ {
			rh = max(rh, cells[r*t.cols+c].MinSize().Height)
		}
		h += rh
	}
	return fyne.NewSize(float32(t.cols)*40, h)
}

// tableCell is either a text value or a ready-made widget.
type tableCell struct {
	Text   string
	Status bool
	Obj    fyne.CanvasObject
}

// newTable builds a table with a bold header row.
func newTable(headers []string, rows [][]tableCell) fyne.CanvasObject {
	cols := len(headers)
	tl := &tableLayout{rows: len(rows) + 1, cols: cols, pref: make([]float32, cols)}
	// Measure with an untruncated label so padding and font match what is rendered.
	measure := func(s string, bold bool) float32 {
		if len([]rune(s)) > 70 {
			s = string([]rune(s)[:70])
		}
		return widget.NewLabelWithStyle(s, fyne.TextAlignLeading, fyne.TextStyle{Bold: bold}).MinSize().Width + 2
	}

	var bgs, cells []fyne.CanvasObject
	headerBg := canvas.NewRectangle(theme.Color(theme.ColorNameHover))
	headerBg.CornerRadius = 4
	bgs = append(bgs, headerBg)
	for c, h := range headers {
		l := widget.NewLabelWithStyle(h, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		l.Truncation = fyne.TextTruncateEllipsis
		cells = append(cells, l)
		tl.pref[c] = max(tl.pref[c], measure(h, true))
	}
	for r, row := range rows {
		bg := canvas.NewRectangle(color.Transparent)
		if r%2 == 1 {
			bg.FillColor = theme.Color(theme.ColorNameInputBackground)
		}
		bgs = append(bgs, bg)
		for c := 0; c < cols; c++ {
			var cell tableCell
			if c < len(row) {
				cell = row[c]
			}
			var o fyne.CanvasObject
			switch {
			case cell.Obj != nil:
				o = cell.Obj
				tl.pref[c] = max(tl.pref[c], o.MinSize().Width)
			case cell.Status && cell.Text != "" && cell.Text != "—":
				o = container.NewVBox(layout.NewSpacer(), pillLeft(cell.Text, statusColor(cell.Text)), layout.NewSpacer())
				tl.pref[c] = max(tl.pref[c], o.MinSize().Width+theme.Padding())
			default:
				l := widget.NewLabel(cell.Text)
				l.Truncation = fyne.TextTruncateEllipsis
				o = l
				tl.pref[c] = max(tl.pref[c], measure(cell.Text, false))
			}
			cells = append(cells, o)
		}
	}
	return container.New(tl, append(bgs, cells...)...)
}

// ---------- Dashboard tile ----------

func newTile(icon fyne.Resource, value, caption string, accent color.NRGBA) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.NRGBA{R: accent.R, G: accent.G, B: accent.B, A: 0x1c})
	bg.CornerRadius = 10
	bg.StrokeColor = color.NRGBA{R: accent.R, G: accent.G, B: accent.B, A: 0x70}
	bg.StrokeWidth = 1
	v := widget.NewLabelWithStyle(value, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	v.SizeName = theme.SizeNameSubHeadingText
	v.Truncation = fyne.TextTruncateEllipsis
	// Long captions (e.g. CPU model names) wrap onto several lines instead of overflowing.
	capt := widget.NewLabel(caption)
	capt.Wrapping = fyne.TextWrapWord
	ic := widget.NewIcon(icon)
	text := container.New(layout.NewCustomPaddedLayout(0, 0, 0, 0), container.NewVBox(v, capt))
	body := container.NewBorder(nil, nil, container.NewCenter(ic), nil, text)
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(6, 6, 12, 8), body))
}

// tileGrid lays tiles out at a fixed width; every tile gets the height of the tallest
// one, so wrapped captions always fit and the row stays even.
func tileGrid(tiles []fyne.CanvasObject) fyne.CanvasObject {
	const w = 270
	var h float32
	for _, t := range tiles {
		t.Resize(fyne.NewSize(w, 400)) // lets wrapped labels compute their real height
		h = max(h, t.MinSize().Height)
	}
	return container.NewGridWrap(fyne.NewSize(w, h), tiles...)
}

// fixedWidth lays out its children at a fixed width and their natural height.
type fixedWidth struct{ w float32 }

func (f *fixedWidth) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

func (f *fixedWidth) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(f.w, h)
}
