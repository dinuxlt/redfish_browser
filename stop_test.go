package main

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// walk visits every object in a canvas tree.
func walk(o fyne.CanvasObject, fn func(fyne.CanvasObject)) {
	fn(o)
	switch t := o.(type) {
	case *fyne.Container:
		for _, c := range t.Objects {
			walk(c, fn)
		}
	case *widget.Card:
		walk(t.Content, fn)
	case *widget.Form:
		for _, it := range t.Items {
			walk(it.Widget, fn)
		}
	}
}

func TestStopConnect(t *testing.T) {
	fa := test.NewApp()
	a := &App{fyneApp: fa}
	a.win = fa.NewWindow("t")
	fyne.DoAndWait(func() { a.showLogin("") })

	var connect, stop *widget.Button
	var entries []*widget.Entry
	var labels []*widget.Label
	walk(a.win.Content(), func(o fyne.CanvasObject) {
		switch w := o.(type) {
		case *widget.Button:
			switch w.Text {
			case "Connect":
				connect = w
			case "Stop":
				stop = w
			}
		case *widget.Entry:
			entries = append(entries, w)
		case *widget.Label:
			labels = append(labels, w)
		}
	})
	if connect == nil || stop == nil || len(entries) < 3 {
		t.Fatalf("widgets not found: %v %v %d", connect, stop, len(entries))
	}
	if stop.Visible() {
		t.Fatal("stop visible before connecting")
	}
	fyne.DoAndWait(func() {
		entries[0].SetText("10.255.255.1") // unroutable: connection hangs
		test.Tap(connect)
	})
	time.Sleep(time.Second)
	if !stop.Visible() || connect.Visible() {
		t.Fatal("stop button should be shown while connecting")
	}
	start := time.Now()
	fyne.DoAndWait(func() { test.Tap(stop) })
	time.Sleep(300 * time.Millisecond)
	if stop.Visible() || !connect.Visible() {
		t.Fatal("connect button should be back after stop")
	}
	found := false
	for _, l := range labels {
		if l.Text == "Connection attempt stopped." {
			found = true
		}
	}
	if !found {
		t.Fatal("stopped message not shown")
	}
	t.Logf("UI reset %v after Stop", time.Since(start))
}
