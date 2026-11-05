//go:build windows

package clipboard

import (
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Native returns the Windows clipboard, which marks every copy as excluded
// from clipboard history (Win+V), cloud clipboard sync and clipboard
// monitors, as password managers do. fallback is unused on Windows.
func Native(fallback Backend) Backend { return windowsBoard{} }

var (
	user32                   = windows.NewLazySystemDLL("user32.dll")
	kernel32                 = windows.NewLazySystemDLL("kernel32.dll")
	openClipboard            = user32.NewProc("OpenClipboard")
	closeClipboard           = user32.NewProc("CloseClipboard")
	emptyClipboard           = user32.NewProc("EmptyClipboard")
	setClipboardData         = user32.NewProc("SetClipboardData")
	getClipboardData         = user32.NewProc("GetClipboardData")
	registerClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")
	isClipboardFormatAvail   = user32.NewProc("IsClipboardFormatAvailable")
	globalAlloc              = kernel32.NewProc("GlobalAlloc")
	globalFree               = kernel32.NewProc("GlobalFree")
	globalLock               = kernel32.NewProc("GlobalLock")
	globalUnlock             = kernel32.NewProc("GlobalUnlock")
	moveMemory               = kernel32.NewProc("RtlMoveMemory")
	lstrlenW                 = kernel32.NewProc("lstrlenW")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// The formats Windows documents for keeping sensitive content out of
// clipboard history and cloud sync.
var privacyFormats = []string{"ExcludeClipboardContentFromMonitorProcessing", "CanIncludeInClipboardHistory", "CanUploadToCloudClipboard"}

type windowsBoard struct{}

// open opens the clipboard, retrying briefly while another program holds it.
func open() bool {
	for i := 0; i < 20; i++ {
		if r, _, _ := openClipboard.Call(0); r != 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// globalCopy puts data in a movable global memory block the clipboard can own.
func globalCopy(data []byte) uintptr {
	h, _, _ := globalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return 0
	}
	p, _, _ := globalLock.Call(h)
	if p == 0 {
		globalFree.Call(h)
		return 0
	}
	moveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	globalUnlock.Call(h)
	return h
}

func (windowsBoard) SetText(text string) bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !open() {
		return false
	}
	defer closeClipboard.Call()
	if r, _, _ := emptyClipboard.Call(); r == 0 {
		return false
	}
	if text == "" {
		return true
	}
	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return false
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(&utf16[0])), len(utf16)*2)
	h := globalCopy(data)
	if h == 0 {
		return false
	}
	if r, _, _ := setClipboardData.Call(cfUnicodeText, h); r == 0 {
		globalFree.Call(h)
		return false
	}
	// A zero DWORD for each privacy format. If one can't be set the text
	// is still on the clipboard; Kupi clears it after the delay anyway.
	zero := []byte{0, 0, 0, 0}
	for _, name := range privacyFormats {
		namePtr, err := windows.UTF16PtrFromString(name)
		if err != nil {
			continue
		}
		format, _, _ := registerClipboardFormatW.Call(uintptr(unsafe.Pointer(namePtr)))
		if format == 0 {
			continue
		}
		if h := globalCopy(zero); h != 0 {
			if r, _, _ := setClipboardData.Call(format, h); r == 0 {
				globalFree.Call(h)
			}
		}
	}
	return true
}

func (windowsBoard) Text() (string, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if r, _, _ := isClipboardFormatAvail.Call(cfUnicodeText); r == 0 {
		return "", true
	}
	if !open() {
		return "", false
	}
	defer closeClipboard.Call()
	h, _, _ := getClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	p, _, _ := globalLock.Call(h)
	if p == 0 {
		return "", false
	}
	defer globalUnlock.Call(h)
	n, _, _ := lstrlenW.Call(p)
	buf := make([]uint16, n)
	if n > 0 {
		moveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), p, n*2)
	}
	return windows.UTF16ToString(buf), true
}
