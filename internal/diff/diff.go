// Package diff produces unified line diffs for change previews.
package diff

import (
	"fmt"
	"strings"
)

// maxCells caps the work for one diff (lines(a) × lines(b)). Larger inputs
// fall back to "replace everything", which is still correct, just less precise.
const maxCells = 4_000_000

type opKind byte

const (
	keep opKind = ' '
	del  opKind = '-'
	add  opKind = '+'
)

type op struct {
	kind opKind
	text string
}

// Unified returns a unified diff of a and b with ctx lines of context, or ""
// when they are equal.
func Unified(oldName, newName, a, b string, ctx int) string {
	if a == b {
		return ""
	}
	ops := lineOps(splitLines(a), splitLines(b))
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", oldName, newName)

	// Walk the ops, emitting hunks around each run of changes.
	type pos struct{ oldLine, newLine int }
	positions := make([]pos, len(ops)+1)
	o, n := 1, 1
	for i, op := range ops {
		positions[i] = pos{o, n}
		switch op.kind {
		case keep:
			o++
			n++
		case del:
			o++
		case add:
			n++
		}
	}
	positions[len(ops)] = pos{o, n}

	i := 0
	for i < len(ops) {
		if ops[i].kind == keep {
			i++
			continue
		}
		start := max(0, i-ctx)
		end := i
		for end < len(ops) {
			if ops[end].kind != keep {
				end++
				continue
			}
			// Look ahead: does another change start within 2*ctx lines?
			run := end
			for run < len(ops) && ops[run].kind == keep {
				run++
			}
			if run < len(ops) && run-end <= 2*ctx {
				end = run
				continue
			}
			end = min(len(ops), end+ctx)
			break
		}
		oldCount, newCount := 0, 0
		for _, op := range ops[start:end] {
			if op.kind != add {
				oldCount++
			}
			if op.kind != del {
				newCount++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", positions[start].oldLine, oldCount, positions[start].newLine, newCount)
		for _, op := range ops[start:end] {
			out.WriteByte(byte(op.kind))
			out.WriteString(op.text)
			out.WriteByte('\n')
		}
		i = end
	}
	return out.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// lineOps computes a line-level edit script using the longest common
// subsequence.
func lineOps(a, b []string) []op {
	n, m := len(a), len(b)
	if n*m > maxCells {
		ops := make([]op, 0, n+m)
		for _, l := range a {
			ops = append(ops, op{del, l})
		}
		for _, l := range b {
			ops = append(ops, op{add, l})
		}
		return ops
	}
	// lcs[i][j] = length of the LCS of a[i:] and b[j:].
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	ops := make([]op, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{keep, a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{del, a[i]})
			i++
		default:
			ops = append(ops, op{add, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{del, a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{add, b[j]})
	}
	return ops
}
