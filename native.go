package main

import (
	"log"
	"os"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/iheb-amiri/kupi/internal/buildmode"
)

// nativeConfirm asks a question in a native OS dialog. Page scripts can
// neither see nor click it, so it guards switches that matter (turning on
// the terminal). Cancel is the default answer.
func nativeConfirm(app *application.App, title, message, accept string) bool {
	if !buildmode.Production && os.Getenv("KUPI_DEV_AUTOCONFIRM") == "1" {
		// Development builds only, for testing in a browser (server mode)
		// where no native dialog can appear.
		log.Printf("KUPI_DEV_AUTOCONFIRM: accepting %q without asking", title)
		return true
	}
	answer := make(chan bool, 1)
	d := app.Dialog.Question().SetTitle(title).SetMessage(message)
	yes := d.AddButton(accept)
	yes.OnClick(func() { answer <- true })
	no := d.AddButton("Cancel")
	no.OnClick(func() { answer <- false })
	d.SetDefaultButton(no)
	d.SetCancelButton(no)
	if w := app.Window.Current(); w != nil {
		d.AttachToWindow(w)
	}
	d.Show()
	// Windows shows the dialog synchronously, so the answer is already in;
	// GTK answers later.
	wait := 10 * time.Minute
	if runtime.GOOS == "windows" {
		wait = 200 * time.Millisecond
	}
	select {
	case a := <-answer:
		return a
	case <-time.After(wait):
		return false
	}
}
