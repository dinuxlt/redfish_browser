package main

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const hlPool = 12 // highlight rectangles per visible row

var (
	colMatch   = color.NRGBA{R: 0xf5, G: 0xc5, B: 0x18, A: 0x55}
	colCurrent = color.NRGBA{R: 0xff, G: 0x8c, B: 0x00, A: 0xc0}
)

type dispLine struct {
	src   int // source line index
	start int // rune offset in the source line
	text  string
}

type match struct{ line, col, n int } // in source line / rune coordinates

type segment struct {
	col, n  int
	current bool
}

// jsonViewer is a read-only, virtualised text view with line numbers, soft wrapping
// and find-in-text highlighting.
type jsonViewer struct {
	lines [][]rune

	cols      int
	disp      []dispLine
	firstDisp []int

	matches []match
	cur     int
	segs    map[int][]segment

	list      *widget.List
	selected  int
	cellW     float32
	cellH     float32
	textX     float32
	textSize  float32
	fgColor   color.Color
	numColor  color.Color
	onChanged func() // called when the match set or current match changes
}

func newJSONViewer(text string) *jsonViewer {
	v := &jsonViewer{selected: -1, cur: -1}
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		v.lines = append(v.lines, []rune(l))
	}
	v.textSize = theme.TextSize()
	m := fyne.MeasureText("M", v.textSize, fyne.TextStyle{Monospace: true})
	v.cellW, v.cellH = m.Width, m.Height
	digits := len(strconv.Itoa(len(v.lines)))
	v.textX = 8 + float32(digits)*v.cellW + 14
	v.fgColor = theme.Color(theme.ColorNameForeground)
	v.numColor = theme.Color(theme.ColorNamePlaceHolder)
	v.rewrap(120)

	v.list = widget.NewList(
		func() int { return len(v.disp) },
		v.createRow,
		v.updateRow,
	)
	v.list.HideSeparators = true
	v.list.OnSelected = func(id widget.ListItemID) {
		if id < len(v.disp) {
			v.selected = v.disp[id].src
		}
	}
	return v
}

// object returns the viewer as a canvas object that re-wraps lines when resized.
func (v *jsonViewer) object() fyne.CanvasObject {
	return container.New(&wrapLayout{v: v}, v.list)
}

type wrapLayout struct{ v *jsonViewer }

func (w *wrapLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	v := w.v
	cols := int((size.Width - v.textX - theme.ScrollBarSize() - 2*theme.Padding()) / v.cellW)
	if cols < 20 {
		cols = 20
	}
	if cols != v.cols {
		v.rewrap(cols)
		v.list.Refresh()
	}
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

func (w *wrapLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(200, 100)
}

func (v *jsonViewer) rewrap(cols int) {
	v.cols = cols
	v.disp = v.disp[:0]
	v.firstDisp = make([]int, len(v.lines))
	for i, l := range v.lines {
		v.firstDisp[i] = len(v.disp)
		if len(l) == 0 {
			v.disp = append(v.disp, dispLine{src: i})
			continue
		}
		for s := 0; s < len(l); s += cols {
			e := min(s+cols, len(l))
			v.disp = append(v.disp, dispLine{src: i, start: s, text: string(l[s:e])})
		}
	}
	v.buildSegments()
}

func (v *jsonViewer) createRow() fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, 0, hlPool+2)
	for i := 0; i < hlPool; i++ {
		r := canvas.NewRectangle(colMatch)
		r.CornerRadius = 2
		r.Hide()
		objs = append(objs, r)
	}
	num := canvas.NewText("", v.numColor)
	num.TextStyle.Monospace = true
	num.TextSize = v.textSize
	num.Alignment = fyne.TextAlignTrailing
	txt := canvas.NewText("", v.fgColor)
	txt.TextStyle.Monospace = true
	txt.TextSize = v.textSize
	objs = append(objs, num, txt)
	return container.New(&rowLayout{h: v.cellH + 2}, objs...)
}

// rowLayout gives list rows a compact, constant height; children are positioned in updateRow.
type rowLayout struct{ h float32 }

func (r *rowLayout) Layout([]fyne.CanvasObject, fyne.Size) {}

func (r *rowLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(10, r.h) }

func (v *jsonViewer) updateRow(id widget.ListItemID, o fyne.CanvasObject) {
	if id >= len(v.disp) {
		return
	}
	row := o.(*fyne.Container)
	d := v.disp[id]
	num := row.Objects[hlPool].(*canvas.Text)
	txt := row.Objects[hlPool+1].(*canvas.Text)
	if d.start == 0 {
		num.Text = strconv.Itoa(d.src + 1)
	} else {
		num.Text = "↪"
	}
	num.Move(fyne.NewPos(0, 1))
	num.Resize(fyne.NewSize(v.textX-14, v.cellH))
	num.Refresh()
	txt.Text = d.text
	txt.Move(fyne.NewPos(v.textX, 1))
	txt.Refresh()

	segs := v.segs[id]
	for i := 0; i < hlPool; i++ {
		r := row.Objects[i].(*canvas.Rectangle)
		if i >= len(segs) {
			r.Hide()
			continue
		}
		s := segs[i]
		r.FillColor = colMatch
		if s.current {
			r.FillColor = colCurrent
		}
		r.Move(fyne.NewPos(v.textX+float32(s.col)*v.cellW, 1))
		r.Resize(fyne.NewSize(float32(s.n)*v.cellW, v.cellH))
		r.Show()
		r.Refresh()
	}
}

// Search finds all occurrences of q and makes the first one current.
func (v *jsonViewer) Search(q string, matchCase bool) {
	v.matches = v.matches[:0]
	v.cur = -1
	if q != "" {
		needle := []rune(q)
		if !matchCase {
			needle = []rune(strings.ToLower(q))
		}
		for i, l := range v.lines {
			hay := l
			if !matchCase {
				hay = []rune(strings.ToLower(string(l)))
				if len(hay) != len(l) { // lower-casing changed length; fall back to exact runes
					hay = l
				}
			}
			for c := 0; c+len(needle) <= len(hay); {
				if runesEqual(hay[c:c+len(needle)], needle) {
					v.matches = append(v.matches, match{i, c, len(needle)})
					c += len(needle)
				} else {
					c++
				}
			}
		}
		if len(v.matches) > 0 {
			v.cur = 0
		}
	}
	v.buildSegments()
	v.list.Refresh()
	v.scrollToCurrent()
	v.notify()
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Next moves to the next (dir=1) or previous (dir=-1) match, wrapping around.
func (v *jsonViewer) Next(dir int) {
	if len(v.matches) == 0 {
		return
	}
	v.cur = (v.cur + dir + len(v.matches)) % len(v.matches)
	v.buildSegments()
	v.list.Refresh()
	v.scrollToCurrent()
	v.notify()
}

func (v *jsonViewer) notify() {
	if v.onChanged != nil {
		v.onChanged()
	}
}

// Status describes the search state, e.g. "3 of 17".
func (v *jsonViewer) Status(q string) string {
	switch {
	case q == "":
		return ""
	case len(v.matches) == 0:
		return "No matches"
	}
	return fmt.Sprintf("%d of %d", v.cur+1, len(v.matches))
}

// buildSegments maps matches (source coordinates) onto wrapped display lines.
func (v *jsonViewer) buildSegments() {
	v.segs = map[int][]segment{}
	for i, m := range v.matches {
		col, left := m.col, m.n
		for left > 0 {
			d := v.firstDisp[m.line] + col/v.cols
			c := col % v.cols
			n := min(left, v.cols-c)
			v.segs[d] = append(v.segs[d], segment{col: c, n: n, current: i == v.cur})
			col += n
			left -= n
		}
	}
}

func (v *jsonViewer) scrollToCurrent() {
	if v.cur < 0 || v.cur >= len(v.matches) {
		return
	}
	m := v.matches[v.cur]
	idx := v.firstDisp[m.line] + m.col/v.cols
	// ScrollTo only scrolls as little as needed; bracketing the match with two calls
	// leaves it roughly centred whichever direction we came from.
	half := int(v.list.Size().Height/(v.cellH+4)) / 2
	v.list.ScrollTo(min(idx+half, len(v.disp)-1))
	v.list.ScrollTo(max(idx-half, 0))
}

// SelectedLine returns the text of the line the user clicked, if any.
func (v *jsonViewer) SelectedLine() (string, bool) {
	if v.selected < 0 || v.selected >= len(v.lines) {
		return "", false
	}
	return strings.TrimSpace(string(v.lines[v.selected])), true
}

// rawJSONView builds the Raw JSON tab: toolbar with find bar plus the viewer.
// It returns the view and the find entry (so Ctrl+F can focus it).
func (a *App) rawJSONView(res *Resource) (fyne.CanvasObject, *widget.Entry) {
	v := newJSONViewer(string(res.Raw))

	find := widget.NewEntry()
	find.SetPlaceHolder("Find in JSON…  (Enter = next)")
	status := widget.NewLabel("")
	status.Alignment = fyne.TextAlignCenter
	matchCase := widget.NewCheck("Match case", nil)

	run := func() { v.Search(find.Text, matchCase.Checked) }
	v.onChanged = func() { status.SetText(v.Status(find.Text)) }
	find.OnChanged = func(string) { run() }
	find.OnSubmitted = func(string) { v.Next(1) }
	matchCase.OnChanged = func(bool) { run() }

	prev := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() { v.Next(-1) })
	next := widget.NewButtonWithIcon("", theme.MoveDownIcon(), func() { v.Next(1) })
	clear := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() { find.SetText("") })

	copyAll := widget.NewButtonWithIcon("Copy JSON", theme.ContentCopyIcon(), func() {
		a.fyneApp.Clipboard().SetContent(string(res.Raw))
	})
	copyLine := widget.NewButtonWithIcon("Copy line", theme.ContentCopyIcon(), func() {
		if s, ok := v.SelectedLine(); ok {
			a.fyneApp.Clipboard().SetContent(s)
		}
	})
	copyLine.Disable()
	v.list.OnSelected = func(id widget.ListItemID) {
		if id < len(v.disp) {
			v.selected = v.disp[id].src
			copyLine.Enable()
		}
	}

	statusBox := container.New(&fixedWidth{110}, status)
	findBox := container.NewBorder(nil, nil, nil,
		container.NewHBox(statusBox, prev, next, clear, matchCase),
		find)
	bar := container.NewBorder(nil, nil,
		container.NewHBox(copyAll, copyLine),
		nil,
		container.New(layout.NewCustomPaddedLayout(0, 0, 12, 0), findBox))
	footer := container.NewHBox(layout.NewSpacer(), mutedText(fmt.Sprintf("%d lines · %d bytes   ", len(v.lines), len(res.Raw))))
	return container.NewBorder(bar, footer, nil, nil, v.object()), find
}
