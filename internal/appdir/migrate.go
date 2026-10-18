package appdir

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// migrateMarker 记录在 state 目录里，存在就说明迁移已经成功做过、提示也显示过了。
const migrateMarker = "migrated-v1"

// legacySource 是一类旧目录：候选来源按优先级排列（先 miao 再 agnet），取第一个存在的。
type legacySource struct {
	candidates []string
	dst        string
}

func legacySources() ([]legacySource, error) {
	home, err := Home()
	if err != nil {
		return nil, err
	}
	var out []legacySource
	for _, k := range []struct {
		base func() (string, error)
		dst  string
	}{
		{configBase, home},
		{dataBase, filepath.Join(home, "data")},
		{stateBase, filepath.Join(home, "state")},
	} {
		b, err := k.base()
		if err != nil {
			continue
		}
		out = append(out, legacySource{
			candidates: []string{filepath.Join(b, Name), filepath.Join(b, OldName)},
			dst:        k.dst,
		})
	}
	return out, nil
}

// migrateResult 是一次复制的统计。
type migrateResult struct {
	copied    int
	conflicts []string // 目标已存在且内容不同，已跳过
	skipped   []string // 符号链接等不复制的条目
	errs      []error
}

// Migrate 把旧的 XDG 目录（$XDG_CONFIG_HOME/miao、$XDG_DATA_HOME/miao、$XDG_STATE_HOME/miao，
// 以及更早的 agnet 目录）复制到新家目录（~/.miao 或 $MIAO_HOME）。
//
// 规则：只复制不删除，旧目录原样保留；不跟随符号链接；目标已有的文件绝不覆盖（冲突跳过并提示）；
// 每个文件复制后校验内容。全部成功才写迁移标记，之后不再检查；有失败时保留旧目录、
// 返回警告，下次启动重试。返回给用户看的提示。
func Migrate() []string {
	home, err := Home()
	if err != nil {
		return nil
	}
	marker := filepath.Join(home, "state", migrateMarker)
	if _, err := os.Lstat(marker); err == nil {
		return nil
	}
	srcs, err := legacySources()
	if err != nil {
		return nil
	}

	var (
		total   migrateResult
		from    []string
		notes   []string
		touched bool
	)
	for _, s := range srcs {
		src := pickSource(s.candidates)
		if src == "" {
			continue
		}
		if samePath(src, s.dst) || within(s.dst, src) || within(src, s.dst) {
			// $MIAO_HOME 指到旧目录里面（或反过来）时不复制，免得自己复制自己
			notes = append(notes, fmt.Sprintf("跳过迁移 %s：与 %s 互相包含", tilde(src), tilde(s.dst)))
			continue
		}
		touched = true
		from = append(from, tilde(src))
		r := copyTreeNoOverwrite(src, s.dst)
		total.copied += r.copied
		total.conflicts = append(total.conflicts, r.conflicts...)
		total.skipped = append(total.skipped, r.skipped...)
		total.errs = append(total.errs, r.errs...)
	}

	if len(total.conflicts) > 0 {
		notes = append(notes, fmt.Sprintf("迁移时跳过了 %d 个已存在的文件（未覆盖）：%s", len(total.conflicts), brief(total.conflicts)))
	}
	if len(total.skipped) > 0 {
		notes = append(notes, fmt.Sprintf("迁移时没有复制 %d 个符号链接：%s", len(total.skipped), brief(total.skipped)))
	}
	if len(total.errs) > 0 {
		msgs := make([]string, 0, len(total.errs))
		for _, e := range total.errs {
			msgs = append(msgs, e.Error())
		}
		notes = append(notes, fmt.Sprintf("迁移到 %s 未完成（旧目录已保留，下次启动会重试）：%s", tilde(home), brief(msgs)))
		return notes
	}
	if err := writeMarker(marker); err != nil {
		notes = append(notes, fmt.Sprintf("写入迁移标记 %s 失败：%v", tilde(marker), err))
	}
	if touched {
		notes = append(notes, fmt.Sprintf("已把 %s 复制到 %s，旧目录确认无误后可手动删除", strings.Join(from, "、"), tilde(home)))
	}
	return notes
}

func pickSource(cands []string) string {
	for _, c := range cands {
		if info, err := os.Lstat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return ""
}

// within 报告 p 是否在 dir 里面（不含相等）。
func within(p, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(p))
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func brief(items []string) string {
	const max = 5
	if len(items) <= max {
		return strings.Join(items, "；")
	}
	return strings.Join(items[:max], "；") + fmt.Sprintf(" 等 %d 项", len(items))
}

func writeMarker(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte("ok\n"), 0o600)
}

// copyTreeNoOverwrite 把 src 递归复制到 dst：不跟随符号链接，已存在的目标不覆盖。
// 出错的条目记下来继续处理其它条目。
func copyTreeNoOverwrite(src, dst string) migrateResult {
	var r migrateResult
	if err := os.MkdirAll(dst, 0o700); err != nil {
		r.errs = append(r.errs, err)
		return r
	}
	_ = os.Chmod(dst, 0o700)
	walkErr := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			r.errs = append(r.errs, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			r.errs = append(r.errs, err)
			return nil
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			r.skipped = append(r.skipped, tilde(p))
		case d.IsDir():
			if rel == "." {
				return nil
			}
			info, err := os.Lstat(target)
			if err == nil && !info.IsDir() {
				r.conflicts = append(r.conflicts, tilde(target))
				return fs.SkipDir
			}
			if err := os.Mkdir(target, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				r.errs = append(r.errs, err)
				return fs.SkipDir
			}
		case d.Type().IsRegular():
			copied, conflict, err := copyFileVerified(p, target)
			switch {
			case err != nil:
				r.errs = append(r.errs, fmt.Errorf("%s: %w", tilde(p), err))
			case conflict:
				r.conflicts = append(r.conflicts, tilde(target))
			case copied:
				r.copied++
			}
		}
		return nil
	})
	if walkErr != nil {
		r.errs = append(r.errs, walkErr)
	}
	return r
}

// copyFileVerified 复制单个文件：目标已存在时内容相同算已完成，不同算冲突，都不覆盖。
// 先写同目录临时文件并 fsync，校验哈希后用硬链接落到目标名（目标被抢先创建时不会覆盖）。
func copyFileVerified(src, dst string) (copied, conflict bool, err error) {
	if info, err := os.Lstat(dst); err == nil {
		if info.Mode().IsRegular() && sameContent(src, dst) {
			return false, false, nil
		}
		return false, true, nil
	}
	in, err := os.Open(src)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = in.Close() }()
	st, err := in.Stat()
	if err != nil {
		return false, false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".migrate-*")
	if err != nil {
		return false, false, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), in); err != nil {
		_ = tmp.Close()
		return false, false, err
	}
	if err := tmp.Chmod(st.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return false, false, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, false, err
	}
	if err := tmp.Close(); err != nil {
		return false, false, err
	}
	want := h.Sum(nil)
	got, err := fileHash(tmpName)
	if err != nil {
		return false, false, err
	}
	if !bytes.Equal(want, got) {
		return false, false, errors.New("复制后校验不一致")
	}
	if err := os.Link(tmpName, dst); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, true, nil
		}
		return false, false, err
	}
	return true, false, nil
}

func fileHash(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func sameContent(a, b string) bool {
	ha, err := fileHash(a)
	if err != nil {
		return false
	}
	hb, err := fileHash(b)
	return err == nil && bytes.Equal(ha, hb)
}
