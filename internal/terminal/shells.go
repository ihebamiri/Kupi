package terminal

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// lookPath finds a program on PATH and insists on an absolute result.
func lookPath(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%s resolved to a relative path", name)
	}
	return p, nil
}

// Shells lists the shells installed on this computer, best first.
func Shells() []Shell { return detectShells() }

// Find returns the shell with this id, or the first one when id is "".
func Find(id string) (Shell, error) {
	shells := detectShells()
	if len(shells) == 0 {
		return Shell{}, fmt.Errorf("no supported shell found on this computer")
	}
	if id == "" {
		return shells[0], nil
	}
	for _, s := range shells {
		if s.ID == id {
			return s, nil
		}
	}
	return Shell{}, fmt.Errorf("the shell %q isn't installed; pick another in Settings", id)
}
