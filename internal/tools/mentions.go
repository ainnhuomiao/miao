package tools

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	mentionMaxFiles     = 10
	mentionMaxFileBytes = 64 << 10
	mentionMaxTotal     = 256 << 10
)

// mentionRe 匹配行首或空白后的 @路径。
var mentionRe = regexp.MustCompile(`(?:^|\s)@([^\s@，。；：！？、（）]+)`)

// ListFiles 列出工作区内的文件（跳过 .git 和 .gitignore），按路径排序，最多 limit 个。
func (r *Registry) ListFiles(ctx context.Context, limit int) ([]string, error) {
	var out []string
	err := r.walkWorkspace(ctx, r.workspace, func(_, rel string) error {
		if isSensitive(rel) {
			return nil
		}
		out = append(out, rel)
		if limit > 0 && len(out) >= limit {
			return errStopWalk
		}
		return nil
	})
	if err == errStopWalk {
		err = nil
	}
	sort.Strings(out)
	return out, err
}

var errStopWalk = fmt.Errorf("stop")

// MentionedPaths 返回文本里出现的 @路径（去重，保持顺序）。
func MentionedPaths(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		p := strings.TrimRight(m[1], ".,;:!?，。；：！？)]}）")
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// ExpandMentions 把文本里 @引用的工作区文件内容附到消息末尾。
// attached 是成功附上的路径，warnings 是跳过的原因（不存在、太大、二进制、敏感文件等）。
// 不是文件的 @xxx（比如 @某人）原样保留、不报警告。
func (r *Registry) ExpandMentions(text string) (expanded string, attached, warnings []string) {
	var b strings.Builder
	total := 0
	for _, p := range MentionedPaths(text) {
		full, err := r.ResolvePath(p)
		if err != nil {
			continue // 绝对路径、../ 越界：静默忽略
		}
		if err := r.checkNoSymlinkEscape(full); err != nil {
			warnings = append(warnings, "@"+p+" 未附加：符号链接指向工作区外")
			continue
		}
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			continue
		}
		rel := strings.TrimPrefix(p, "./")
		switch {
		case len(attached) >= mentionMaxFiles:
			warnings = append(warnings, fmt.Sprintf("@%s 未附加：一次最多引用 %d 个文件", rel, mentionMaxFiles))
			continue
		case isSensitive(rel):
			warnings = append(warnings, fmt.Sprintf("@%s 未附加：敏感文件", rel))
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("@%s 未附加：%v", rel, err))
			continue
		}
		if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			warnings = append(warnings, fmt.Sprintf("@%s 未附加：不是文本文件", rel))
			continue
		}
		note := ""
		if len(data) > mentionMaxFileBytes {
			cut := mentionMaxFileBytes
			for cut > 0 && !utf8.RuneStart(data[cut]) {
				cut--
			}
			data = data[:cut]
			note = fmt.Sprintf(" truncated=\"只附了前 %dKiB，需要更多请用 read_file\"", mentionMaxFileBytes>>10)
		}
		if total+len(data) > mentionMaxTotal {
			warnings = append(warnings, fmt.Sprintf("@%s 未附加：引用内容合计超过 %dKiB", rel, mentionMaxTotal>>10))
			continue
		}
		total += len(data)
		fmt.Fprintf(&b, "\n\n<file path=%q%s>\n%s\n</file>", rel, note, strings.TrimRight(string(data), "\n"))
		attached = append(attached, rel)
	}
	if len(attached) == 0 {
		return text, nil, warnings
	}
	return text + "\n\n以下是用户用 @ 引用的文件内容：" + b.String(), attached, warnings
}
