package tools

import "strings"

// lineDiff 返回逐行差异：每行以 "+ "、"- " 开头，只含改动行（不含上下文）。
// 文件太大时退化为“整段删除 + 整段新增”。
func lineDiff(oldS, newS string) (lines []string, add, del int) {
	a := splitLines(oldS)
	b := splitLines(newS)
	// 去掉公共前后缀，缩小 LCS 规模。
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	a, b = a[p:len(a)-s], b[p:len(b)-s]

	if len(a)*len(b) > 4_000_000 {
		for _, l := range a {
			lines = append(lines, "- "+l)
		}
		for _, l := range b {
			lines = append(lines, "+ "+l)
		}
		return lines, len(b), len(a)
	}
	// LCS 表
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
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			i++
			j++
		case j < m && (i == n || dp[i][j+1] > dp[i+1][j]):
			lines = append(lines, "+ "+b[j])
			add++
			j++
		default:
			lines = append(lines, "- "+a[i])
			del++
			i++
		}
	}
	return lines, add, del
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
