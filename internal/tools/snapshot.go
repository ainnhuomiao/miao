package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// snapshot 记录一次写入前的文件状态，供 /undo 恢复。
type snapshot struct {
	rel     string      // 相对工作区路径
	existed bool        // 写入前文件是否存在；不存在则撤销时删除
	file    string      // 原内容快照文件（existed 时有效）
	perm    os.FileMode // 原权限
}

type snapState struct {
	mu    sync.Mutex
	dir   string // 快照目录；为空时只存内存
	stack []snapshot
	mem   map[string][]byte // dir 为空时的内存快照
}

// 用独立表挂在 Registry 上，避免改动 Registry 结构体（并行工具会并发访问，需加锁）。
var snaps sync.Map // *Registry -> *snapState

func (r *Registry) snaps() *snapState {
	v, _ := snaps.LoadOrStore(r, &snapState{mem: map[string][]byte{}})
	return v.(*snapState)
}

// SetSnapshotDir 设置快照目录（通常是会话目录下的 snapshots/<会话 id>），并清空撤销栈。
func (r *Registry) SetSnapshotDir(dir string) {
	s := r.snaps()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dir, s.stack, s.mem = dir, nil, map[string][]byte{}
}

// saveSnapshot 在写入 full 之前调用，记录原文件内容。
func (r *Registry) saveSnapshot(full string) error {
	rel, _ := filepath.Rel(r.workspace, full)
	sn := snapshot{rel: rel}
	data, err := os.ReadFile(full)
	if err == nil {
		info, _ := os.Stat(full)
		sn.existed, sn.perm = true, info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	s := r.snaps()
	s.mu.Lock()
	defer s.mu.Unlock()
	if sn.existed {
		key := strconv.FormatInt(time.Now().UnixNano(), 36)
		if s.dir != "" {
			if err := os.MkdirAll(s.dir, 0o700); err != nil {
				return err
			}
			sn.file = filepath.Join(s.dir, key)
			if err := os.WriteFile(sn.file, data, 0o600); err != nil {
				return err
			}
		} else {
			sn.file = key
			s.mem[key] = data
		}
	}
	s.stack = append(s.stack, sn)
	return nil
}

// UndoDepth 返回还能撤销的次数。
func (r *Registry) UndoDepth() int {
	s := r.snaps()
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.stack)
}

// Undo 撤销最近一次文件修改，返回被恢复的相对路径。可连续调用。
func (r *Registry) Undo() (string, error) {
	s := r.snaps()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stack) == 0 {
		return "", fmt.Errorf("没有可以撤销的修改")
	}
	sn := s.stack[len(s.stack)-1]
	full := filepath.Join(r.workspace, sn.rel)
	if !sn.existed {
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	} else {
		var data []byte
		if s.dir != "" {
			b, err := os.ReadFile(sn.file)
			if err != nil {
				return "", fmt.Errorf("读取快照失败: %w", err)
			}
			data = b
		} else {
			data = s.mem[sn.file]
		}
		if err := atomicWrite(full, string(data), sn.perm); err != nil {
			return "", err
		}
		if s.dir != "" {
			_ = os.Remove(sn.file)
		} else {
			delete(s.mem, sn.file)
		}
	}
	s.stack = s.stack[:len(s.stack)-1]
	return sn.rel, nil
}
