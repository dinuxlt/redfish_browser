package main

import (
	"context"
	"image/png"
	"os"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

func TestRawFind(t *testing.T) {
	host, out := os.Getenv("REDFISH_MOCK"), os.Getenv("SHOT_DIR")
	if host == "" {
		t.Skip("set REDFISH_MOCK")
	}
	fa := test.NewApp()
	a := &App{fyneApp: fa}
	a.win = fa.NewWindow("t")
	a.win.Resize(fyne.NewSize(1320, 860))
	c, _ := NewClient(host, "admin", "admin", true)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	fyne.DoAndWait(func() { a.startSession(c) })
	time.Sleep(time.Second)
	fyne.DoAndWait(func() { a.navigate("/redfish/v1/Systems/1") })
	time.Sleep(time.Second)
	fyne.DoAndWait(func() {
		tabs := a.content.Objects[0].(*container.AppTabs)
		tabs.SelectIndex(1)
	})
	if a.rawFind == nil {
		t.Fatal("rawFind not set when Raw JSON tab is selected")
	}
	fyne.DoAndWait(func() {
		a.rawFind.SetText("health")
		a.rawFind.OnSubmitted("health") // next match
	})
	time.Sleep(500 * time.Millisecond)
	// Navigating keeps the Raw JSON tab open.
	if out != "" {
		img := a.win.Canvas().Capture()
		f, _ := os.Create(out + "/raw-find.png")
		png.Encode(f, img)
		f.Close()
	}
	fyne.DoAndWait(func() { a.navigate("/redfish/v1/Chassis/1/Thermal") })
	time.Sleep(time.Second)
	if a.rawFind == nil || a.content.Objects[0].(*container.AppTabs).SelectedIndex() != 1 {
		t.Fatal("Raw JSON tab should stay selected after navigating")
	}
	fyne.DoAndWait(func() { a.rawFind.SetText("ReadingCelsius") })
	time.Sleep(300 * time.Millisecond)
	if out != "" {
		img := a.win.Canvas().Capture()
		f, _ := os.Create(out + "/raw-find2.png")
		png.Encode(f, img)
		f.Close()
	}
}

func TestViewerSearch(t *testing.T) {
	test.NewApp()
	v := newJSONViewer("{\n  \"Name\": \"abcHealth\",\n  \"Health\": \"OK\"\n}")
	v.Search("health", false)
	if len(v.matches) != 2 || v.Status("health") != "1 of 2" {
		t.Fatalf("got %d matches, %q", len(v.matches), v.Status("health"))
	}
	v.Next(1)
	v.Next(1)
	if v.Status("health") != "1 of 2" {
		t.Fatalf("wrap-around failed: %q", v.Status("health"))
	}
	v.Search("health", true)
	if len(v.matches) != 0 {
		t.Fatalf("match case: got %d", len(v.matches))
	}
	// wrapped segments: a match crossing a wrap boundary is split in two
	v.Search("abcHealth", true)
	v.rewrap(12)
	n := 0
	for _, s := range v.segs {
		n += len(s)
	}
	if n != 2 {
		t.Fatalf("expected split highlight, got %d segments", n)
	}
}
