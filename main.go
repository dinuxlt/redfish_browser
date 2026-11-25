package main

import (
	"fyne.io/fyne/v2/app"
)

func main() {
	fa := app.NewWithID("io.github.redfishbrowser")
	a := NewApp(fa)
	a.showLogin("")
	a.win.ShowAndRun()
}
