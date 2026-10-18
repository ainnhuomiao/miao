package tools

import "strings"

// shortCluster 逐字符检查 -abc 形式的短参数簇：noArg 是不带值的选项，withArg 是带值的选项
// （值可以粘在后面，也可以是下一个参数）。ok=false 表示出现了不在白名单里的字符；
// takesNext=true 表示下一个参数是值。
func shortCluster(a, noArg, withArg string) (ok, takesNext bool) {
	body := a[1:]
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case strings.IndexByte(withArg, c) >= 0:
			return true, i == len(body)-1
		case strings.IndexByte(noArg, c) >= 0:
		default:
			return false, false
		}
	}
	return true, false
}

var (
	sortLongNoArg = map[string]bool{"reverse": true, "numeric-sort": true, "unique": true,
		"ignore-case": true, "ignore-leading-blanks": true, "human-numeric-sort": true,
		"version-sort": true, "stable": true, "general-numeric-sort": true, "month-sort": true,
		"dictionary-order": true, "ignore-nonprinting": true, "check": true, "random-sort": true,
		"zero-terminated": true, "debug": true}
	sortLongArg = map[string]bool{"key": true, "field-separator": true, "buffer-size": true,
		"parallel": true, "check": true, "sort": true}
)

// sortUnsafe 用白名单判断 sort 参数：-o/--output 写文件、--compress-program 执行程序、
// -T/--temporary-directory 写到任意目录、--files0-from/--random-source 读任意文件，都不在白名单里。
func sortUnsafe(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return false
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		if strings.HasPrefix(a, "--") {
			k, _, eq := strings.Cut(a[2:], "=")
			switch {
			case eq && sortLongArg[k]:
			case !eq && sortLongNoArg[k]:
			case !eq && (k == "key" || k == "field-separator"):
				i++
			default:
				return true
			}
			continue
		}
		ok, next := shortCluster(a, "bdfghiMnrRsuVcCz", "ktS")
		if !ok {
			return true
		}
		if next {
			i++
		}
	}
	return false
}

// grepRecursive 报告 grep 是否递归搜索（-r/-R/--recursive/-d recurse 等），递归会扫到 .env 等密钥文件。
func grepRecursive(args []string) bool {
	const withArg = "efmABCdDX"
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return false
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		if strings.HasPrefix(a, "--") {
			k, v, eq := strings.Cut(a[2:], "=")
			switch k {
			case "recursive", "dereference-recursive":
				return true
			case "directories":
				if !eq && i+1 < len(args) {
					i++
					v = args[i]
				}
				if v != "read" && v != "skip" {
					return true
				}
			case "regexp", "file", "max-count", "after-context", "before-context", "context",
				"devices", "label", "include", "exclude", "exclude-dir", "exclude-from", "binary-files", "color", "colour":
				if !eq {
					i++
				}
			}
			continue
		}
		// 扫描簇里的 r/R/d；其他无值短参数（-n、-i、-l 等）视为无害，遇到带值选项后其余部分是值。
		bad, next := false, false
		body := a[1:]
		for j := 0; j < len(body); j++ {
			c := body[j]
			if c == 'r' || c == 'R' {
				bad = true
				break
			}
			if strings.IndexByte(withArg, c) >= 0 {
				val := body[j+1:]
				next = val == ""
				if c == 'd' {
					if next && i+1 < len(args) {
						val = args[i+1]
					}
					if val != "read" && val != "skip" {
						bad = true
					}
				}
				break
			}
		}
		if bad {
			return true
		}
		if next {
			i++
		}
	}
	return false
}

// rgUnrestricted 报告 rg 是否关掉了忽略规则或开启隐藏文件搜索（--hidden、-.、--no-ignore*、-u/-uu/-uuu）。
func rgUnrestricted(args []string) bool {
	const withArg = "ABCefgjMmrtTEd"
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return false
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		if strings.HasPrefix(a, "--") {
			k, _, eq := strings.Cut(a[2:], "=")
			if k == "hidden" || k == "unrestricted" || strings.HasPrefix(k, "no-ignore") ||
				k == "follow" {
				return true
			}
			switch k {
			case "regexp", "file", "glob", "iglob", "max-count", "after-context", "before-context",
				"context", "threads", "max-columns", "replace", "type", "type-not", "encoding", "max-depth",
				"type-add", "type-clear", "max-filesize", "sort", "sortr", "path-separator", "colors", "color":
				if !eq {
					i++
				}
			}
			continue
		}
		body := a[1:]
		next := false
		for j := 0; j < len(body); j++ {
			c := body[j]
			if c == 'u' || c == '.' || c == 'L' {
				return true
			}
			if strings.IndexByte(withArg, c) >= 0 {
				next = body[j+1:] == ""
				break
			}
		}
		if next {
			i++
		}
	}
	return false
}
