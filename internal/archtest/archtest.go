// Package archtest enforces Kupi's architecture and security rules on the
// source tree. The checks run as an ordinary Go test, so a violation fails
// `go test ./...` and CI.
package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// clientImports may only be imported by internal/kube.
var clientImports = []string{
	"k8s.io/client-go/dynamic",
	"k8s.io/client-go/kubernetes",
	"k8s.io/client-go/rest",
	"k8s.io/client-go/tools/remotecommand",
	"k8s.io/client-go/tools/portforward",
	// Helm packages that talk to the cluster; its data types (chart, release) are fine.
	"helm.sh/helm/v4/pkg/action",
	"helm.sh/helm/v4/pkg/kube",
	"helm.sh/helm/v4/pkg/storage",
}

// allowedClientImports are harmless packages under a forbidden prefix.
var allowedClientImports = map[string]bool{
	"k8s.io/client-go/kubernetes/scheme": true,
}

var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true,
	"cmd": true, "cmd.exe": true, "powershell": true, "powershell.exe": true, "pwsh": true, "pwsh.exe": true,
}

const terminalImport = "github.com/iheb-amiri/kupi/internal/terminal"

// commandFlags make a shell run a command instead of being interactive.
var commandFlags = map[string]bool{
	"-c": true, "/c": true, "/k": true, "/r": true, "-command": true, "-encodedcommand": true,
	"-file": true, "-ec": true, "--command": true,
}

// CheckGo returns rule violations in one non-test Go file. rel is the file's
// path relative to the repository root, with forward slashes.
func CheckGo(rel string, src []byte) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		return []string{fmt.Sprintf("%s: parse error: %v", rel, err)}
	}
	var problems []string
	inKube := strings.HasPrefix(rel, "internal/kube/")
	inGuard := strings.HasPrefix(rel, "internal/guard/")
	inTerminal := strings.HasPrefix(rel, "internal/terminal/")

	execNames := map[string]bool{}
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if !inKube && isClientImport(path) {
			problems = append(problems, fmt.Sprintf("%s: imports %s; only internal/kube may talk to the API server directly", rel, path))
		}
		if path == terminalImport && !strings.HasPrefix(rel, "internal/bridge/") && rel != "main.go" {
			problems = append(problems, fmt.Sprintf("%s: imports %s; only internal/bridge may open terminals", rel, path))
		}
		if path == "os/exec" {
			name := "exec"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			execNames[name] = true
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		// The terminal starts interactive shells only: never with a command.
		if lit, ok := n.(*ast.BasicLit); ok && inTerminal && lit.Kind == token.STRING {
			s, _ := strconv.Unquote(lit.Value)
			if commandFlags[strings.ToLower(s)] {
				problems = append(problems, fmt.Sprintf("%s:%d: the flag %q makes a shell run a command; the terminal only starts interactive shells", rel, fset.Position(lit.Pos()).Line, s))
			}
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pos := fset.Position(call.Pos())
		if (sel.Sel.Name == "Mutator" || sel.Sel.Name == "HelmMutator") && len(call.Args) == 0 && !inKube && !inGuard {
			problems = append(problems, fmt.Sprintf("%s:%d: calls %s(); only internal/guard may change a cluster", rel, pos.Line, sel.Sel.Name))
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && execNames[pkg.Name] {
			argIndex := -1
			switch sel.Sel.Name {
			case "Command":
				argIndex = 0
			case "CommandContext":
				argIndex = 1
			}
			if argIndex >= 0 && len(call.Args) > argIndex {
				if lit, ok := call.Args[argIndex].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					name, _ := strconv.Unquote(lit.Value)
					if shells[strings.ToLower(name)] {
						problems = append(problems, fmt.Sprintf("%s:%d: runs the shell %q; run programs directly with an argument list", rel, pos.Line, name))
					}
				}
			}
		}
		return true
	})
	return problems
}

func isClientImport(path string) bool {
	if allowedClientImports[path] {
		return false
	}
	for _, p := range clientImports {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

var frontendRules = []struct {
	re  *regexp.Regexp
	msg string
}{
	{regexp.MustCompile(`dangerouslySetInnerHTML`), "uses dangerouslySetInnerHTML; render text, or sanitize with DOMPurify in the one approved component"},
	{regexp.MustCompile(`\.(inner|outer)HTML\s*=`), "assigns innerHTML/outerHTML; use textContent or React"},
	{regexp.MustCompile(`\beval\s*\(`), "uses eval"},
	{regexp.MustCompile(`\bnew\s+Function\s*\(`), "uses new Function"},
	{regexp.MustCompile(`(?i)<script[^>]+src\s*=\s*["']https?:`), "loads a remote script"},
	{regexp.MustCompile(`(?i)@import\s+url\(\s*["']?https?:`), "loads a remote stylesheet"},
}

// CheckFrontend returns rule violations in one frontend source file.
func CheckFrontend(rel string, src []byte) []string {
	var problems []string
	for i, line := range strings.Split(string(src), "\n") {
		for _, r := range frontendRules {
			if r.re.MatchString(line) {
				problems = append(problems, fmt.Sprintf("%s:%d: %s", rel, i+1, r.msg))
			}
		}
	}
	return problems
}
