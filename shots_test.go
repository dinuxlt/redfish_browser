package main

import (
	"context"
	"image/png"
	"os"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// TestScreenshots renders key pages against a running mock (REDFISH_MOCK=host:port).
func TestScreenshots(t *testing.T) {
	host := os.Getenv("REDFISH_MOCK")
	out := os.Getenv("SHOT_DIR")
	if host == "" || out == "" {
		t.Skip("set REDFISH_MOCK and SHOT_DIR")
	}
	fa := test.NewApp()
	a := &App{fyneApp: fa}
	a.win = fa.NewWindow("Redfish Browser")
	a.win.Resize(fyne.NewSize(1320, 860))
	shot := func(name string) {
		time.Sleep(1500 * time.Millisecond)
		var img = a.win.Canvas().Capture()
		f, _ := os.Create(out + "/" + name + ".png")
		png.Encode(f, img)
		f.Close()
	}
	fyne.DoAndWait(func() { a.showLogin("") })
	shot("0-login")

	c, err := NewClient(host, "admin", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	fyne.DoAndWait(func() { a.startSession(c) })
	shot("1-overview")
	for i, p := range []string{"/redfish/v1/Systems/1", "/redfish/v1/Systems/1/Memory", "/redfish/v1/Chassis/1/Thermal", "/redfish/v1/Systems/1/LogServices/SEL/Entries", "/redfish/v1/Systems/1/Storage/RAID.1"} {
		fyne.DoAndWait(func() { a.navigate(p) })
		shot(string(rune('2'+i)) + "-" + lastSegment(p))
	}
	fyne.DoAndWait(func() { a.search("CN77") })
	shot("8-search")
	fyne.DoAndWait(func() {
		a.tree.OpenBranch("\x1f/redfish/v1/Systems")
	})
	time.Sleep(time.Second)
	fyne.DoAndWait(func() { a.tree.OpenBranch("\x1f/redfish/v1/Systems\x1f/redfish/v1/Systems/1") })
	shot("9-tree")
	fyne.DoAndWait(func() { a.crawl() })
	time.Sleep(3 * time.Second)
	shot("10-crawl")
	t.Logf("cached after crawl: %d", len(c.CachedAll()))
}
