//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procUpdateProcThreadAttribute = windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")

// conPTY is a shell attached to a Windows pseudo console.
type conPTY struct {
	console windows.Handle
	in      *os.File // our end: keystrokes to the console
	out     *os.File // our end: output from the console
	process windows.Handle

	mu          sync.Mutex
	done        chan struct{} // closed once the shell has exited
	code        int
	waitErr     error
	consoleOnce sync.Once
}

func startShell(sh Shell, dir string, env []string, cols, rows uint16) (process, error) {
	if !filepath.IsAbs(sh.Path) {
		return nil, fmt.Errorf("shell path must be absolute")
	}
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("create input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("create output pipe: %w", err)
	}
	var console windows.Handle
	err := windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, 0, &console)
	// The console keeps its own copies of its ends.
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)
	if err != nil {
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, fmt.Errorf("create pseudo console: %w", err)
	}
	p := &conPTY{console: console, in: os.NewFile(uintptr(inWrite), "conpty-in"), out: os.NewFile(uintptr(outRead), "conpty-out")}

	proc, err := spawn(sh, dir, env, console)
	if err != nil {
		p.closeConsole()
		return nil, err
	}
	p.process = proc
	p.done = make(chan struct{})
	go p.watch()
	return p, nil
}

func spawn(sh Shell, dir string, env []string, console windows.Handle) (windows.Handle, error) {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, fmt.Errorf("attribute list: %w", err)
	}
	defer attrs.Delete()
	// The console handle itself is the attribute value, as in the Windows
	// ConPTY sample; x/sys's Update takes a pointer, so call the API directly.
	r, _, callErr := procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(attrs.List())), 0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(console), unsafe.Sizeof(console), 0, 0)
	if r == 0 {
		return 0, fmt.Errorf("attach pseudo console: %w", callErr)
	}

	si := &windows.StartupInfoEx{}
	si.StartupInfo.Cb = uint32(unsafe.Sizeof(*si))
	// No inherited standard handles: the shell must use the pseudo console.
	si.StartupInfo.Flags = windows.STARTF_USESTDHANDLES
	si.ProcThreadAttributeList = attrs.List()

	app, err := windows.UTF16PtrFromString(sh.Path)
	if err != nil {
		return 0, err
	}
	cmdLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{sh.Path}, sh.args...)))
	if err != nil {
		return 0, err
	}
	var cwd *uint16
	if dir != "" {
		if cwd, err = windows.UTF16PtrFromString(dir); err != nil {
			return 0, err
		}
	}
	block, err := envBlock(env)
	if err != nil {
		return 0, err
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(app, cmdLine, nil, nil, false, flags, &block[0], cwd, &si.StartupInfo, &pi); err != nil {
		return 0, fmt.Errorf("start %s: %w", sh.Path, err)
	}
	windows.CloseHandle(pi.Thread)
	return pi.Process, nil
}

// envBlock encodes KEY=value pairs as a Windows environment block.
func envBlock(env []string) ([]uint16, error) {
	var b []uint16
	for _, kv := range env {
		if strings.ContainsRune(kv, 0) {
			return nil, errors.New("environment holds a NUL character")
		}
		b = append(b, utf16.Encode([]rune(kv))...)
		b = append(b, 0)
	}
	if len(b) == 0 {
		b = append(b, 0)
	}
	return append(b, 0), nil
}

func (p *conPTY) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *conPTY) Write(b []byte) (int, error) { return p.in.Write(b) }

func (p *conPTY) Resize(cols, rows uint16) error {
	return windows.ResizePseudoConsole(p.console, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// watch waits for the shell to exit. ConPTY doesn't end the output stream
// when the shell exits; closing the console does, so watch closes it.
func (p *conPTY) watch() {
	_, err := windows.WaitForSingleObject(p.process, windows.INFINITE)
	var code uint32
	if err == nil {
		err = windows.GetExitCodeProcess(p.process, &code)
	}
	p.mu.Lock()
	p.code, p.waitErr = int(code), err
	close(p.done)
	windows.CloseHandle(p.process)
	p.mu.Unlock()
	p.closeConsole()
}

func (p *conPTY) Wait() (int, error) {
	<-p.done
	return p.code, p.waitErr
}

func (p *conPTY) closeConsole() {
	p.consoleOnce.Do(func() {
		// This can wait for unread output; the reader keeps running meanwhile
		// and gets EOF once the console is gone.
		windows.ClosePseudoConsole(p.console)
		_ = p.in.Close()
		_ = p.out.Close()
	})
}

// Close ends the shell (and with it the console, through watch).
func (p *conPTY) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
	default:
		// The shell may have children (kubectl logs -f…); they lose their
		// console and end with it.
		_ = windows.TerminateProcess(p.process, 1)
	}
	return nil
}

// detectShells lists the shells installed on this PC, best first.
func detectShells() []Shell {
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		sys = `C:\Windows\System32`
	}
	var out []Shell
	add := func(id, name, path string, args ...string) {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			out = append(out, Shell{ID: id, Name: name, Path: path, args: args})
		}
	}
	if p, err := lookPath("pwsh.exe"); err == nil {
		add("pwsh", "PowerShell 7", p, "-NoLogo")
	}
	add("powershell", "Windows PowerShell", filepath.Join(sys, `WindowsPowerShell\v1.0\powershell.exe`), "-NoLogo")
	add("cmd", "Command Prompt", filepath.Join(sys, "cmd.exe"))
	for _, base := range []string{os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles")} {
		if base != "" && filepath.IsAbs(base) {
			add("git-bash", "Git Bash", filepath.Join(base, `Git\bin\bash.exe`), "--login", "-i")
			if len(out) > 0 && out[len(out)-1].ID == "git-bash" {
				break
			}
		}
	}
	add("wsl", "WSL (default distribution)", filepath.Join(sys, "wsl.exe"))
	return out
}
