package archtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepository applies every rule to the real source tree.
func TestRepository(t *testing.T) {
	root := repoRoot(t)
	var problems []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		if d.IsDir() {
			switch rel {
			case ".git", "bin", "build", "frontend/node_modules", "frontend/dist", "frontend/bindings":
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go"):
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			problems = append(problems, CheckGo(rel, src)...)
		case strings.HasPrefix(rel, "frontend/src/") && (strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".tsx")),
			rel == "frontend/index.html":
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			problems = append(problems, CheckFrontend(rel, src)...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func TestCheckGoCatchesViolations(t *testing.T) {
	src := `package feature

import (
	"os/exec"
	"k8s.io/client-go/dynamic"
	_ "k8s.io/client-go/kubernetes/scheme"
	_ "helm.sh/helm/v4/pkg/action"
	_ "helm.sh/helm/v4/pkg/release/v1"
)

func f(s interface{ Mutator() int; HelmMutator() int }) {
	_ = s.Mutator()
	_ = s.HelmMutator()
	_ = exec.Command("bash", "-c", "echo hi")
	var _ dynamic.Interface
}
`
	problems := CheckGo("internal/feature/f.go", []byte(src))
	want := []string{"imports k8s.io/client-go/dynamic", "imports helm.sh/helm/v4/pkg/action", "calls Mutator()", "calls HelmMutator()", `runs the shell "bash"`}
	if len(problems) != len(want) {
		t.Fatalf("got %d problems, want %d: %v", len(problems), len(want), problems)
	}
	for i, w := range want {
		if !strings.Contains(problems[i], w) {
			t.Errorf("problem %d = %q, want it to mention %q", i, problems[i], w)
		}
	}
}

func TestCheckGoAllowsKubeAndGuard(t *testing.T) {
	kubeSrc := `package kube
import "k8s.io/client-go/dynamic"
var _ dynamic.Interface
`
	if p := CheckGo("internal/kube/kube.go", []byte(kubeSrc)); len(p) != 0 {
		t.Errorf("internal/kube flagged: %v", p)
	}
	guardSrc := `package guard
func f(s interface{ Mutator() int }) { _ = s.Mutator() }
`
	if p := CheckGo("internal/guard/g.go", []byte(guardSrc)); len(p) != 0 {
		t.Errorf("internal/guard flagged: %v", p)
	}
	execSrc := `package x
import "os/exec"
func f(path string) { _ = exec.Command(path, "version") }
`
	if p := CheckGo("internal/x/x.go", []byte(execSrc)); len(p) != 0 {
		t.Errorf("direct exec flagged: %v", p)
	}
}

func TestCheckFrontendCatchesViolations(t *testing.T) {
	src := "const a = <div dangerouslySetInnerHTML={{__html: x}} />\nel.innerHTML = y\neval(z)\nnew Function('x')\n"
	if got := len(CheckFrontend("frontend/src/a.tsx", []byte(src))); got != 4 {
		t.Errorf("got %d problems, want 4", got)
	}
	if got := CheckFrontend("frontend/src/b.tsx", []byte("el.textContent = y\n")); len(got) != 0 {
		t.Errorf("safe code flagged: %v", got)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func TestTerminalRules(t *testing.T) {
	src := `package terminal

var args = []string{"-NoLogo", "-Command"}
var bash = []string{"--login", "-c"}
`
	problems := CheckGo("internal/terminal/x.go", []byte(src))
	if len(problems) != 2 || !strings.Contains(problems[0], `"-Command"`) || !strings.Contains(problems[1], `"-c"`) {
		t.Errorf("terminal command flags: %v", problems)
	}
	if p := CheckGo("internal/feature/x.go", []byte(src)); len(p) != 0 {
		t.Errorf("the flag rule is only for the terminal package: %v", p)
	}
	imp := "package x\nimport _ \"github.com/iheb-amiri/kupi/internal/terminal\"\n"
	if p := CheckGo("internal/feature/x.go", []byte(imp)); len(p) != 1 || !strings.Contains(p[0], "only internal/bridge") {
		t.Errorf("terminal import: %v", p)
	}
	if p := CheckGo("internal/bridge/terminal.go", []byte(imp)); len(p) != 0 {
		t.Errorf("bridge may import the terminal: %v", p)
	}
}
