package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

// HasSnapshot 报告撤销栈里是否有 rel 这个文件的快照。
func (r *Registry) HasSnapshot(rel string) bool {
	rel = filepath.Clean(filepath.FromSlash(rel))
	s := r.snaps()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sn := range s.stack {
		if filepath.Clean(sn.rel) == rel {
			return true
		}
	}
	return false
}

// UndoFile 把 rel 恢复到本会话第一次改它之前的样子，并从撤销栈中移除它的全部快照。
// 其他文件的快照不受影响。
func (r *Registry) UndoFile(rel string) error {
	rel = filepath.Clean(filepath.FromSlash(rel))
	s := r.snaps()
	s.mu.Lock()
	defer s.mu.Unlock()
	first := -1
	for i, sn := range s.stack {
		if filepath.Clean(sn.rel) == rel {
			first = i
			break
		}
	}
	if first < 0 {
		return fmt.Errorf("这个文件没有本会话的修改快照")
	}
	sn := s.stack[first]
	full := filepath.Join(r.workspace, rel)
	if err := r.checkNoSymlinkEscape(full); err != nil {
		return err
	}
	if !sn.existed {
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		var data []byte
		if s.dir != "" {
			b, err := os.ReadFile(sn.file)
			if err != nil {
				return fmt.Errorf("读取快照失败: %w", err)
			}
			data = b
		} else {
			data = s.mem[sn.file]
		}
		if err := atomicWrite(full, string(data), sn.perm); err != nil {
			return err
		}
	}
	kept := s.stack[:0]
	for _, x := range s.stack {
		if filepath.Clean(x.rel) == rel {
			if x.existed {
				if s.dir != "" {
					_ = os.Remove(x.file)
				} else {
					delete(s.mem, x.file)
				}
			}
			continue
		}
		kept = append(kept, x)
	}
	s.stack = kept
	return nil
}
