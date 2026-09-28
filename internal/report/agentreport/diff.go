package agentreport

import (
	"fmt"
	"strings"
)

// diffContext is the number of unchanged lines kept around each change.
const diffContext = 3

// maxDiffCells bounds the O(n*m) LCS table.
const maxDiffCells = 4_000_000

// unifiedDiff computes a line diff between from and to (LCS-based) and
// collapses long unchanged runs, returning display lines plus an optional
// note.
func unifiedDiff(from, to string) ([]diffLineView, string) {
	a := splitLines(from)
	b := splitLines(to)
	if len(a)*len(b) > maxDiffCells {
		return nil, fmt.Sprintf("Diff too large to render (%d → %d lines).", len(a), len(b))
	}

	// LCS lengths, suffix-based.
	n, m := len(a), len(b)
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

	var raw []diffLineView
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			raw = append(raw, diffLineView{Kind: "ctx", Sign: " ", Text: a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			raw = append(raw, diffLineView{Kind: "del", Sign: "-", Text: a[i]})
			i++
		default:
			raw = append(raw, diffLineView{Kind: "add", Sign: "+", Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		raw = append(raw, diffLineView{Kind: "del", Sign: "-", Text: a[i]})
	}
	for ; j < m; j++ {
		raw = append(raw, diffLineView{Kind: "add", Sign: "+", Text: b[j]})
	}

	changed := false
	for _, l := range raw {
		if l.Kind != "ctx" {
			changed = true
			break
		}
	}
	if !changed {
		return nil, "No changes."
	}

	// Keep only context lines within diffContext of a change.
	keep := make([]bool, len(raw))
	for idx, l := range raw {
		if l.Kind == "ctx" {
			continue
		}
		for k := idx - diffContext; k <= idx+diffContext; k++ {
			if k >= 0 && k < len(raw) {
				keep[k] = true
			}
		}
	}
	var out []diffLineView
	skipped := 0
	for idx, l := range raw {
		if keep[idx] {
			if skipped > 0 {
				out = append(out, diffLineView{Kind: "gap", Sign: "⋯", Text: fmt.Sprintf("%d unchanged lines", skipped)})
				skipped = 0
			}
			out = append(out, l)
		} else {
			skipped++
		}
	}
	if skipped > 0 {
		out = append(out, diffLineView{Kind: "gap", Sign: "⋯", Text: fmt.Sprintf("%d unchanged lines", skipped)})
	}
	return out, ""
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
