package api

import (
	"fmt"
	"strings"
)

// unifiedDiff produces a minimal unified-diff-formatted string between
// oldText and newText, labeled with filename. It implements a classic
// O(n*m) longest-common-subsequence line diff -- more than adequate for the
// small generated Terraform files (variables.tf, main.tf, one
// <module>.auto.tfvars per module) this is used against; a full Myers
// implementation would be overkill for Phase 1's preview/diff UX, which
// just needs a readable +/- view, not minimal-hunk optimality.
//
// Returns "" if oldText == newText (no diff to show for this file).
func unifiedDiff(filename, oldText, newText string) string {
	if oldText == newText {
		return ""
	}

	oldLines := splitLines(oldText)
	newLines := splitLines(newText)
	ops := lcsDiff(oldLines, newLines)

	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n", filename)
	fmt.Fprintf(&b, "+++ b/%s\n", filename)
	for _, op := range ops {
		switch op.kind {
		case diffEqual:
			b.WriteString(" " + op.line + "\n")
		case diffDelete:
			b.WriteString("-" + op.line + "\n")
		case diffInsert:
			b.WriteString("+" + op.line + "\n")
		}
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

type diffOpKind int

const (
	diffEqual diffOpKind = iota
	diffDelete
	diffInsert
)

type diffOp struct {
	kind diffOpKind
	line string
}

// lcsDiff computes a line-level diff via dynamic-programming LCS, then
// walks the DP table backwards to emit an edit script (equal/delete/insert
// ops in display order). Deliberately simple/unoptimized: Phase 1's
// generated files are at most a few dozen lines, so the O(n*m) table is
// trivial in practice.
func lcsDiff(a, b []string) []diffOp {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{diffEqual, a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, diffOp{diffDelete, a[i]})
			i++
		default:
			ops = append(ops, diffOp{diffInsert, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{diffDelete, a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{diffInsert, b[j]})
	}
	return ops
}
