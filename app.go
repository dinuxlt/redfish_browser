package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	uidOverview  = "\x1eoverview"
	uidRoot      = "\x1eroot"
	pathOverview = "overview"
	pathSearch   = "search"
)

type treeNode struct {
	path     string
	label    string
	member   bool
	special  bool
	children []string
	loaded   bool
	loading  bool
}

// App holds all UI and session state. All fields are only touched on the Fyne goroutine.
type App struct {
	fyneApp fyne.App
	win     fyne.Window
	client  *Client

	ctx    context.Context
	cancel context.CancelFunc

	tree  *widget.Tree
	nodes map[string]*treeNode

	content   *fyne.Container
	pathEntry *widget.Entry
	backBtn   *widget.Button
	status    *widget.Label

	current   string
	history   []string
	navSeq    int
	navCancel context.CancelFunc
	lastQuery string

	searchEntry  *widget.Entry
	rawFind      *widget.Entry // find box of the visible Raw JSON tab, if any
	rawTabActive bool
}

func NewApp(fa fyne.App) *App {
	a := &App{fyneApp: fa}
	a.win = fa.NewWindow("Redfish Browser")
	a.win.Resize(fyne.NewSize(1320, 860))
	a.win.SetOnClosed(func() {
		if a.client != nil {
			a.client.Logout()
		}
	})
	return a
}

// ---------------- Login ----------------

func (a *App) showLogin(errMsg string) {
	prefs := a.fyneApp.Preferences()
	host := widget.NewEntry()
	host.SetPlaceHolder("192.168.1.100 or https://bmc.example.com")
	host.SetText(prefs.String("host"))
	user := widget.NewEntry()
	user.SetPlaceHolder("root")
	user.SetText(prefs.StringWithFallback("user", ""))
	pass := widget.NewPasswordEntry()
	insecure := widget.NewCheck("Accept self-signed / invalid TLS certificates", nil)
	insecure.SetChecked(prefs.BoolWithFallback("insecure", true))

	errLabel := widget.NewLabel(errMsg)
	errLabel.Importance = widget.DangerImportance
	errLabel.Wrapping = fyne.TextWrapWord
	spinner := widget.NewActivity()
	progress := widget.NewLabel("")

	var connectBtn, stopBtn *widget.Button
	var cancelConnect context.CancelFunc // non-nil while a connection attempt is running
	attempt := 0                         // ignores results of attempts that were stopped

	finish := func() {
		cancelConnect = nil
		spinner.Stop()
		progress.SetText("")
		stopBtn.Hide()
		connectBtn.Show()
	}
	connect := func() {
		if cancelConnect != nil {
			return
		}
		if strings.TrimSpace(host.Text) == "" {
			errLabel.SetText("Please enter the BMC address.")
			return
		}
		c, err := NewClient(host.Text, user.Text, pass.Text, insecure.Checked)
		if err != nil {
			errLabel.SetText(err.Error())
			return
		}
		prefs.SetString("host", host.Text)
		prefs.SetString("user", user.Text)
		prefs.SetBool("insecure", insecure.Checked)

		errLabel.SetText("")
		progress.SetText("Connecting to " + c.Base + " …")
		connectBtn.Hide()
		stopBtn.Show()
		spinner.Start()

		const timeout = 90 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		cancelConnect = cancel
		attempt++
		my := attempt
		go func() {
			defer cancel()
			err := c.Connect(ctx)
			if err != nil && ctx.Err() != nil {
				go c.Logout() // a session may already have been created before the stop
			}
			fyne.Do(func() {
				if my != attempt {
					return // stopped by the user; UI was already reset
				}
				finish()
				if err != nil {
					if ctx.Err() == context.DeadlineExceeded {
						err = fmt.Errorf("connection timed out after %s", timeout)
					}
					errLabel.SetText(err.Error())
					return
				}
				a.startSession(c)
			})
		}()
	}
	stop := func() {
		if cancelConnect == nil {
			return
		}
		cancelConnect()
		attempt++
		finish()
		errLabel.SetText("Connection attempt stopped.")
	}
	pass.OnSubmitted = func(string) { connect() }
	host.OnSubmitted = func(string) { a.win.Canvas().Focus(user) }
	user.OnSubmitted = func(string) { a.win.Canvas().Focus(pass) }
	connectBtn = widget.NewButtonWithIcon("Connect", theme.LoginIcon(), connect)
	connectBtn.Importance = widget.HighImportance
	stopBtn = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), stop)
	stopBtn.Importance = widget.DangerImportance
	stopBtn.Hide()

	form := widget.NewForm(
		widget.NewFormItem("BMC address", host),
		widget.NewFormItem("Username", user),
		widget.NewFormItem("Password", pass),
	)
	logo := widget.NewIcon(theme.ComputerIcon())
	// A Label (not canvas.Text) so the colour follows the theme even if the system
	// light/dark setting is applied after this screen is built.
	title := widget.NewLabelWithStyle("Redfish Browser", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.SizeName = theme.SizeNameHeadingText
	subtitle := widget.NewLabel("Explore servers through their BMC Redfish API (iDRAC, iLO, XClarity, OpenBMC, Supermicro …)")
	subtitle.Wrapping = fyne.TextWrapWord

	spacer := func(h float32) fyne.CanvasObject {
		r := canvas.NewRectangle(color.Transparent)
		r.SetMinSize(fyne.NewSize(1, h))
		return r
	}
	header := container.NewBorder(nil, nil,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(56, 56), logo)), nil,
		container.NewVBox(title, subtitle))
	content := container.NewVBox(
		header,
		spacer(8),
		widget.NewSeparator(),
		spacer(16),
		form,
		spacer(4),
		insecure,
		spacer(16),
		container.NewHBox(layout.NewSpacer(), progress, spinner, stopBtn, connectBtn),
		errLabel,
	)
	card := widget.NewCard("", "", container.New(layout.NewCustomPaddedLayout(20, 20, 28, 28), content))
	a.win.SetContent(container.NewCenter(container.New(&fixedWidth{820}, card)))
	if host.Text == "" {
		a.win.Canvas().Focus(host)
	} else {
		a.win.Canvas().Focus(pass)
	}
}

// ---------------- Main window ----------------

func (a *App) startSession(c *Client) {
	a.client = c
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.nodes = map[string]*treeNode{}
	a.history = nil
	a.current = ""
	a.win.SetTitle("Redfish Browser — " + c.Base)

	// Tree top level: Overview, Service Root and every collection linked from the root.
	a.nodes[""] = &treeNode{loaded: true}
	a.addChild("", uidOverview, &treeNode{path: pathOverview, label: "Overview", special: true, loaded: true})
	a.addChild("", uidRoot, &treeNode{path: serviceRoot, label: "Service Root", special: true, loaded: true})
	if root := c.Cached(serviceRoot); root != nil {
		for _, l := range ChildLinks(root) {
			a.addChild("", "\x1f"+l.Path, &treeNode{path: l.Path, label: l.Name})
		}
	}

	a.tree = widget.NewTree(a.treeChildren, a.treeIsBranch, a.treeCreate, a.treeUpdate)
	a.tree.OnBranchOpened = func(uid string) { a.loadNode(uid) }
	a.tree.OnSelected = func(uid string) {
		if n := a.nodes[uid]; n != nil && n.path != a.current {
			a.navigate(n.path)
		}
	}

	a.content = container.NewStack()
	a.backBtn = widget.NewButtonWithIcon("", theme.NavigateBackIcon(), a.goBack)
	a.backBtn.Disable()
	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), a.refresh)
	a.pathEntry = widget.NewEntry()
	a.pathEntry.SetPlaceHolder("/redfish/v1/…  (type a path and press Enter)")
	a.pathEntry.OnSubmitted = func(s string) {
		if strings.TrimSpace(s) != "" {
			a.navigate(NormalizePath(s))
		}
	}
	search := widget.NewEntry()
	search.SetPlaceHolder("Search loaded data…")
	search.ActionItem = widget.NewIcon(theme.SearchIcon())
	search.OnSubmitted = func(q string) { a.search(q) }
	a.searchEntry = search
	// Ctrl+F: find in the Raw JSON tab when it is open, otherwise the global search.
	a.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: fyne.KeyModifierShortcutDefault}, func(fyne.Shortcut) {
		if a.rawFind != nil {
			a.win.Canvas().Focus(a.rawFind)
		} else if a.searchEntry != nil {
			a.win.Canvas().Focus(a.searchEntry)
		}
	})
	crawlBtn := widget.NewButtonWithIcon("Load everything", theme.DownloadIcon(), a.crawl)
	exportBtn := widget.NewButtonWithIcon("Export", theme.DocumentSaveIcon(), a.export)
	logoutBtn := widget.NewButtonWithIcon("Disconnect", theme.LogoutIcon(), a.disconnect)

	toolbar := container.NewBorder(nil, nil,
		container.NewHBox(a.backBtn, refresh),
		container.NewHBox(container.NewGridWrap(fyne.NewSize(240, search.MinSize().Height), search), crawlBtn, exportBtn, logoutBtn),
		a.pathEntry,
	)
	a.status = widget.NewLabel("")

	treeHeader := container.NewVBox(heading("Resources", theme.TextSize()+2), widget.NewSeparator())
	left := container.NewBorder(container.NewPadded(treeHeader), nil, nil, nil, a.tree)
	split := container.NewHSplit(left, a.content)
	split.Offset = 0.24

	a.win.SetContent(container.NewBorder(
		container.NewVBox(container.NewPadded(toolbar), widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), a.status),
		nil, nil, split))

	a.tree.Select(uidOverview)
	go a.statusLoop(a.ctx)
}

func (a *App) statusLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fyne.Do(a.updateStatus)
		}
	}
}

func (a *App) updateStatus() {
	if a.client == nil || a.status == nil {
		return
	}
	product := ""
	if r := a.client.Cached(serviceRoot); r != nil {
		v, _ := r.Data["Vendor"].(string)
		p, _ := r.Data["Product"].(string)
		ver, _ := r.Data["RedfishVersion"].(string)
		product = strings.TrimSpace(v + " " + p)
		if ver != "" {
			product += "  ·  Redfish " + ver
		}
	}
	a.status.SetText(fmt.Sprintf("  Connected to %s  ·  %s  ·  Auth: %s  ·  %d requests  ·  %d resources loaded",
		a.client.Base, product, a.client.AuthMode, a.client.Requests.Load(), len(a.client.CachedAll())))
}

func (a *App) disconnect() {
	if a.cancel != nil {
		a.cancel()
	}
	c := a.client
	a.client = nil
	go c.Logout()
	a.win.SetTitle("Redfish Browser")
	a.showLogin("")
}

// ---------------- Tree ----------------

func (a *App) addChild(parent, uid string, n *treeNode) {
	if _, exists := a.nodes[uid]; !exists {
		a.nodes[uid] = n
		a.nodes[parent].children = append(a.nodes[parent].children, uid)
	}
}

func (a *App) treeChildren(uid string) []string {
	if n := a.nodes[uid]; n != nil {
		return n.children
	}
	return nil
}

func (a *App) treeIsBranch(uid string) bool {
	if uid == "" {
		return true
	}
	n := a.nodes[uid]
	if n == nil || n.special {
		return false
	}
	return !n.loaded || len(n.children) > 0
}

func (a *App) treeCreate(branch bool) fyne.CanvasObject {
	return container.NewHBox(widget.NewIcon(theme.FolderIcon()), widget.NewLabel("template"))
}

func (a *App) treeUpdate(uid string, branch bool, o fyne.CanvasObject) {
	n := a.nodes[uid]
	if n == nil {
		return
	}
	box := o.(*fyne.Container)
	label := n.label
	if n.member && a.client != nil {
		if r := a.client.Cached(n.path); r != nil {
			if name, ok := r.Data["Name"].(string); ok && name != "" {
				label = name
				if id, ok := r.Data["Id"].(string); ok && id != "" && id != name && !strings.Contains(name, id) {
					label += " (" + id + ")"
				}
			}
		}
	}
	if n.loading {
		label += "  …"
	}
	box.Objects[0].(*widget.Icon).SetResource(iconFor(n.label, n.path))
	box.Objects[1].(*widget.Label).SetText(label)
}

// loadNode fetches a node's resource and populates its children.
func (a *App) loadNode(uid string) {
	n := a.nodes[uid]
	if n == nil || n.loaded || n.loading {
		return
	}
	n.loading = true
	a.tree.RefreshItem(uid)
	ctx := a.ctx
	go func() {
		r, err := a.client.Get(ctx, n.path, false)
		fyne.Do(func() {
			n.loading = false
			n.loaded = true
			if err == nil {
				a.populate(uid, r)
			}
			a.tree.Refresh()
		})
	}()
}

func (a *App) populate(uid string, r *Resource) {
	for _, l := range ChildLinks(r) {
		a.addChild(uid, uid+"\x1f"+l.Path, &treeNode{path: l.Path, label: l.Name, member: l.IsMember})
	}
}

// ---------------- Navigation ----------------

func (a *App) navigate(path string) { a.show(path, true) }

func (a *App) goBack() {
	if len(a.history) == 0 {
		return
	}
	p := a.history[len(a.history)-1]
	a.history = a.history[:len(a.history)-1]
	a.show(p, false)
}

func (a *App) refresh() {
	switch a.current {
	case pathSearch:
		a.search(a.lastQuery)
	case pathOverview:
		a.showOverview(true)
	default:
		a.load(a.current, true)
	}
}

func (a *App) show(path string, push bool) {
	if push && a.current != "" && a.current != path {
		a.history = append(a.history, a.current)
		if len(a.history) > 200 {
			a.history = a.history[1:]
		}
	}
	if len(a.history) > 0 {
		a.backBtn.Enable()
	} else {
		a.backBtn.Disable()
	}
	a.current = path
	switch path {
	case pathOverview:
		a.pathEntry.SetText("")
		a.showOverview(false)
	case pathSearch:
	default:
		a.pathEntry.SetText(path)
		a.load(path, false)
	}
	a.syncTreeSelection(path)
}

// syncTreeSelection highlights a tree node for path if one is already visible.
func (a *App) syncTreeSelection(path string) {
	// OnSelected ignores nodes whose path is already current, so this does not re-navigate.
	for uid, n := range a.nodes {
		if n.path == path {
			a.tree.Select(uid)
			return
		}
	}
	a.tree.UnselectAll()
}

// beginNav cancels the background work of the previous page and returns a new context.
func (a *App) beginNav() (context.Context, int) {
	if a.navCancel != nil {
		a.navCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.navCancel = cancel
	a.navSeq++
	return ctx, a.navSeq
}

func (a *App) setContent(o fyne.CanvasObject) {
	if _, isTabs := o.(*container.AppTabs); !isTabs {
		a.rawFind = nil
	}
	a.content.Objects = []fyne.CanvasObject{o}
	a.content.Refresh()
}

func (a *App) loadingView(what string) fyne.CanvasObject {
	sp := widget.NewActivity()
	sp.Start()
	return container.NewCenter(container.NewVBox(container.NewCenter(sp), widget.NewLabel("Loading "+what+" …")))
}

func (a *App) errorView(path string, err error, retry func()) fyne.CanvasObject {
	msg := widget.NewLabel(err.Error())
	msg.Wrapping = fyne.TextWrapWord
	msg.Alignment = fyne.TextAlignCenter
	return container.NewCenter(container.NewGridWrap(fyne.NewSize(560, 220), container.NewVBox(
		container.NewCenter(widget.NewIcon(theme.ErrorIcon())),
		container.NewCenter(heading("Could not load resource", 18)),
		container.NewCenter(mutedText(path)),
		msg,
		container.NewCenter(widget.NewButtonWithIcon("Retry", theme.ViewRefreshIcon(), retry)),
	)))
}

func (a *App) load(path string, force bool) {
	ctx, seq := a.beginNav()
	if !force {
		if r := a.client.Cached(path); r != nil {
			a.setContent(a.renderResource(ctx, r))
			return
		}
	}
	a.setContent(a.loadingView(path))
	go func() {
		r, err := a.client.Get(ctx, path, force)
		fyne.Do(func() {
			if seq != a.navSeq {
				return
			}
			if err != nil {
				a.setContent(a.errorView(path, err, func() { a.load(path, true) }))
				return
			}
			a.setContent(a.renderResource(ctx, r))
			a.tree.Refresh()
		})
	}()
}

// ---------------- Search ----------------

type hit struct {
	path, title, key, value string
}

func (a *App) search(q string) {
	q = strings.TrimSpace(q)
	if q == "" || a.client == nil {
		return
	}
	a.lastQuery = q
	if a.current != pathSearch {
		a.show(pathSearch, true)
	}
	a.beginNav()
	a.pathEntry.SetText("")
	needle := strings.ToLower(q)
	all := a.client.CachedAll()
	paths := make([]string, 0, len(all))
	for p := range all {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	const limit = 400
	var hits []hit
	for _, p := range paths {
		r := all[p]
		title := resourceTitle(r)
		var walk func(prefix string, v any)
		walk = func(prefix string, v any) {
			if len(hits) >= limit {
				return
			}
			switch t := v.(type) {
			case map[string]any:
				for _, k := range sortedKeys(t) {
					kp := k
					if prefix != "" {
						kp = prefix + "." + k
					}
					if strings.HasPrefix(k, "@odata") {
						continue
					}
					if _, isObj := t[k].(map[string]any); !isObj && strings.Contains(strings.ToLower(k), needle) {
						if _, isArr := t[k].([]any); !isArr || allScalars(t[k].([]any)) {
							hits = append(hits, hit{p, title, kp, formatValue(k, t[k])})
							continue
						}
					}
					walk(kp, t[k])
				}
			case []any:
				for i, e := range t {
					walk(fmt.Sprintf("%s[%d]", prefix, i), e)
				}
			default:
				s := formatValue("", v)
				if strings.Contains(strings.ToLower(s), needle) {
					hits = append(hits, hit{p, title, prefix, s})
				}
			}
		}
		walk("", r.Data)
	}

	box := container.NewVBox(
		heading(fmt.Sprintf("Search: “%s”", q), 22),
		mutedText(fmt.Sprintf("%d matches in %d loaded resources. Click “Load everything” first to search the whole server.", len(hits), len(all))),
	)
	if len(hits) >= limit {
		box.Add(mutedText(fmt.Sprintf("Showing the first %d matches — refine your search.", limit)))
	}
	rows := make([][]tableCell, 0, len(hits))
	for _, h := range hits {
		p := h.path
		btn := linkButton(h.title, p, a.navigate)
		rows = append(rows, []tableCell{{Obj: btn}, {Text: humanize(h.key)}, {Text: h.value}})
	}
	if len(rows) > 0 {
		box.Add(sectionCard("Results", newTable([]string{"Resource", "Property", "Value"}, rows)))
	}
	a.setContent(container.NewVScroll(container.NewPadded(box)))
}

// ---------------- Crawl & export ----------------

// crawlLinks returns every resource referenced by r (including cross references).
func crawlLinks(r *Resource) []string {
	var out []string
	var walk func(v any, inMembers bool)
	walk = func(v any, inMembers bool) {
		switch t := v.(type) {
		case map[string]any:
			if id, ok := odataID(t); ok && !strings.Contains(id, "#") {
				// Expanded collection members are already complete; don't refetch them.
				if !(inMembers && !isLinkOnly(t)) {
					out = append(out, NormalizePath(id))
				}
			}
			for k, e := range t {
				if k == "@odata.id" {
					continue
				}
				walk(e, k == "Members")
			}
		case []any:
			for _, e := range t {
				walk(e, inMembers)
			}
		}
	}
	for k, v := range r.Data {
		if k != "@odata.id" {
			walk(v, k == "Members")
		}
	}
	return out
}

func (a *App) crawl() {
	ctx, cancel := context.WithCancel(a.ctx)
	const maxResources = 10000

	count := widget.NewLabel("Starting …")
	cur := widget.NewLabel("")
	cur.Truncation = fyne.TextTruncateEllipsis
	bar := widget.NewProgressBarInfinite()
	content := container.NewVBox(
		widget.NewLabel("Fetching every reachable Redfish resource. BMCs can be slow — this may take a few minutes."),
		bar, count, cur)
	dlg := dialog.NewCustom("Loading everything", "Cancel", content, a.win)
	finished := false
	dlg.SetOnClosed(func() {
		if !finished {
			cancel()
		}
	})
	dlg.Resize(fyne.NewSize(560, 220))
	dlg.Show()

	start := time.Now()
	go func() {
		var (
			mu      sync.Mutex
			visited = map[string]bool{serviceRoot: true}
			wg      sync.WaitGroup
			done    int
			errs    int
			last    string
		)
		// A single ticker drives progress updates so the UI isn't flooded.
		stopTicker := make(chan struct{})
		go func() {
			t := time.NewTicker(250 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-stopTicker:
					return
				case <-t.C:
					mu.Lock()
					d, total, e, p := done, len(visited), errs, last
					mu.Unlock()
					fyne.DoAndWait(func() {
						count.SetText(fmt.Sprintf("%d of %d discovered resources fetched  ·  %d errors", d, total, e))
						cur.SetText(p)
					})
				}
			}
		}()
		var visit func(p string)
		visit = func(p string) {
			defer wg.Done()
			r, err := a.client.Get(ctx, p, false)
			mu.Lock()
			done++
			if err != nil {
				errs++
			}
			last = p
			mu.Unlock()
			if err != nil {
				return
			}
			for _, l := range crawlLinks(r) {
				mu.Lock()
				if !visited[l] && len(visited) < maxResources {
					visited[l] = true
					wg.Add(1)
					go visit(l)
				}
				mu.Unlock()
			}
		}
		wg.Add(1)
		go visit(serviceRoot)
		wg.Wait()
		close(stopTicker)

		fyne.Do(func() {
			stopped := ctx.Err() != nil
			finished = true
			dlg.Hide()
			cancel()
			a.tree.Refresh()
			a.updateStatus()
			if a.ctx.Err() != nil {
				return
			}
			msg := fmt.Sprintf("Loaded %d resources in %s (%d could not be fetched).\n\nYou can now search everything or export it to a JSON file.",
				done, time.Since(start).Round(time.Second), errs)
			if stopped {
				msg = fmt.Sprintf("Stopped after %d resources.", done)
			}
			dialog.ShowInformation("Done", msg, a.win)
		})
	}()
}

func (a *App) export() {
	all := a.client.CachedAll()
	save := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil || w == nil {
			return
		}
		defer w.Close()
		out := map[string]any{
			"service":   a.client.Base,
			"exported":  time.Now().Format(time.RFC3339),
			"resources": map[string]any{},
		}
		res := out["resources"].(map[string]any)
		for p, r := range all {
			res[p] = r.Data
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		dialog.ShowInformation("Exported", fmt.Sprintf("Saved %d resources to\n%s", len(all), w.URI().Path()), a.win)
	}, a.win)
	host := strings.NewReplacer("https://", "", "http://", "", ":", "_", "/", "_").Replace(a.client.Base)
	save.SetFileName(fmt.Sprintf("redfish-%s-%s.json", host, time.Now().Format("20060102-150405")))
	save.Resize(fyne.NewSize(900, 600))
	save.Show()
}
