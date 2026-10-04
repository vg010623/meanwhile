package install

import (
	"strings"
)

// Diff returns a minimal line diff ("+ added", "- removed", "  same") using
// the longest common subsequence. Settings files are small, so the simple
// O(n·m) table is fine. Unchanged runs longer than 2·context lines are
// collapsed to "  …".
func Diff(before, after []byte, context int) string {
	a := splitLines(before)
	b := splitLines(after)
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	type line struct {
		op   byte
		text string
	}
	var lines []line
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			lines = append(lines, line{' ', a[i]})
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			lines = append(lines, line{'-', a[i]})
			i++
		default:
			lines = append(lines, line{'+', b[j]})
			j++
		}
	}

	// Keep only changed lines plus `context` lines around them.
	keep := make([]bool, len(lines))
	for k, l := range lines {
		if l.op != ' ' {
			for d := -context; d <= context; d++ {
				if k+d >= 0 && k+d < len(lines) {
					keep[k+d] = true
				}
			}
		}
	}
	var out strings.Builder
	skipping := false
	for k, l := range lines {
		if !keep[k] {
			if !skipping {
				out.WriteString("  …\n")
				skipping = true
			}
			continue
		}
		skipping = false
		out.WriteByte(l.op)
		out.WriteByte(' ')
		out.WriteString(l.text)
		out.WriteByte('\n')
	}
	return out.String()
}

func splitLines(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
