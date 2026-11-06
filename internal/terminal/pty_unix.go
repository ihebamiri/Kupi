//go:build !windows

package terminal

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// unixPTY is a shell attached to a /dev/ptmx pseudo-terminal.
type unixPTY struct {
	ptmx *os.File
	cmd  *exec.Cmd

	closeOnce sync.Once
}

func startShell(sh Shell, dir string, env []string, cols, rows uint16) (process, error) {
	if !filepath.IsAbs(sh.Path) {
		return nil, fmt.Errorf("shell path must be absolute")
	}
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("unlock pty: %w", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("pty number: %w", err)
	}
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Col: cols, Row: rows}); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("pty size: %w", err)
	}
	// A non-blocking descriptor lets Close interrupt a pending Read.
	ptmx := os.NewFile(uintptr(fd), "/dev/ptmx")
	tty, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		ptmx.Close()
		return nil, fmt.Errorf("open pty: %w", err)
	}
	defer tty.Close()

	cmd := exec.Command(sh.Path, sh.args...)
	cmd.Env, cmd.Dir = env, dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		ptmx.Close()
		return nil, fmt.Errorf("start %s: %w", sh.Path, err)
	}
	return &unixPTY{ptmx: ptmx, cmd: cmd}, nil
}

func (p *unixPTY) Read(b []byte) (int, error) {
	n, err := p.ptmx.Read(b)
	// Linux reports EIO once the shell side has closed: that's the end.
	if errors.Is(err, syscall.EIO) {
		err = os.ErrClosed
	}
	return n, err
}

func (p *unixPTY) Write(b []byte) (int, error) { return p.ptmx.Write(b) }

func (p *unixPTY) Resize(cols, rows uint16) error {
	conn, err := p.ptmx.SyscallConn()
	if err != nil {
		return err
	}
	var ioErr error
	if err := conn.Control(func(fd uintptr) {
		ioErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Col: cols, Row: rows})
	}); err != nil {
		return err
	}
	return ioErr
}

func (p *unixPTY) Wait() (int, error) {
	err := p.cmd.Wait()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	}
	return -1, err
}

func (p *unixPTY) Close() error {
	p.closeOnce.Do(func() {
		if p.cmd.Process != nil {
			// Hang up the whole session, like closing a terminal window.
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP)
		}
		_ = p.ptmx.Close()
	})
	return nil
}

var unixShells = []struct{ id, name string }{
	{"bash", "Bash"}, {"zsh", "Zsh"}, {"fish", "Fish"}, {"sh", "sh"},
}

// detectShells lists the shells listed in /etc/shells that exist, with the
// user's login shell ($SHELL) first.
func detectShells() []Shell {
	allowed := map[string]bool{}
	if f, err := os.Open("/etc/shells"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "/") {
				allowed[line] = true
			}
		}
		f.Close()
	}
	var out []Shell
	seen := map[string]bool{}
	add := func(path string) {
		base := filepath.Base(path)
		for _, s := range unixShells {
			if s.id != base || seen[s.id] {
				continue
			}
			if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				out = append(out, Shell{ID: s.id, Name: s.name, Path: path})
				seen[s.id] = true
			}
		}
	}
	if login := os.Getenv("SHELL"); filepath.IsAbs(login) && (allowed[login] || len(allowed) == 0) {
		add(login)
	}
	for _, s := range unixShells {
		if p, err := lookPath(s.id); err == nil && (allowed[p] || len(allowed) == 0) {
			add(p)
		}
	}
	return out
}
