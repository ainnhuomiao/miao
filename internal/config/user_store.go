package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"

	"github.com/ainnhuomiao/miao/internal/atomicfile"
)

var userConfigMu sync.Mutex

// SettingSpec 描述可通过命令行修改的核心配置字段。
type SettingSpec struct {
	Key, Kind string
}

// UserSettings 从实际的配置类型生成字段表，避免额外维护一份字段白名单。
func UserSettings() []SettingSpec {
	typ := reflect.TypeFor[UserFile]()
	fields := make([]SettingSpec, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if key == "" || key == "-" {
			continue
		}
		kind := field.Type
		if kind.Kind() == reflect.Pointer {
			kind = kind.Elem()
		}
		name := kind.Kind().String()
		if kind.Kind() == reflect.Slice {
			name = "array"
		}
		fields = append(fields, SettingSpec{Key: key, Kind: name})
	}
	return fields
}

// LoadUserFile 读取保存的核心设置；未配置的指针字段保持 nil。
func LoadUserFile() (UserFile, error) { return loadUserFile() }

func knownUserSetting(key string) bool {
	for _, spec := range UserSettings() {
		if spec.Key == key {
			return true
		}
	}
	return false
}

// SetUserValue 修改一个核心字段。value 必须是 JSON；保留主题、hooks 和未知扩展键。
func SetUserValue(key string, value json.RawMessage) error {
	if !knownUserSetting(key) {
		return errors.New("未知核心配置字段；运行 miao config keys 查看可修改字段")
	}
	if !json.Valid(value) {
		return errors.New("配置值必须是合法 JSON")
	}
	return UpdateUserConfig(func(all map[string]json.RawMessage) error {
		all[key] = value
		return validateUserDocument(all)
	})
}

// UnsetUserValue 删除一个核心覆盖值，恢复其它配置来源或内置默认值。
func UnsetUserValue(key string) error {
	if !knownUserSetting(key) {
		return errors.New("未知核心配置字段；运行 miao config keys 查看可修改字段")
	}
	return UpdateUserConfig(func(all map[string]json.RawMessage) error {
		delete(all, key)
		return validateUserDocument(all)
	})
}

func validateUserDocument(all map[string]json.RawMessage) error {
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	var user UserFile
	if err := json.Unmarshal(data, &user); err != nil {
		return fmt.Errorf("配置字段类型不正确: %w", err)
	}
	return user.validate()
}

// UpdateUserConfig 串行读改写用户配置，使用进程间锁和私有原子写入。
// change 只在持锁时调用；不得递归调用其它配置保存函数。
func UpdateUserConfig(change func(map[string]json.RawMessage) error) error {
	userConfigMu.Lock()
	defer userConfigMu.Unlock()
	path, err := UserConfigPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := rejectConfigLink(dir); err != nil {
		return err
	}
	lockPath := filepath.Join(dir, ".config.lock")
	if err := rejectConfigLink(lockPath); err != nil {
		return err
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if info, err := lock.Stat(); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return errors.New("配置锁不是普通文件")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	if err := rejectConfigLink(path); err != nil {
		return err
	}
	all := make(map[string]json.RawMessage)
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	var data []byte
	if err == nil {
		defer func() { _ = f.Close() }()
		if info, err := f.Stat(); err != nil {
			return err
		} else if !info.Mode().IsRegular() {
			return errors.New("用户配置不是普通文件")
		}
		data, err = io.ReadAll(f)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &all); err != nil || all == nil {
			return errors.New("用户配置必须是合法的 JSON 对象；已保留原文件")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := change(all); err != nil {
		return err
	}
	data, err = json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WritePrivate(path, append(data, '\n'))
}

func rejectConfigLink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s: %w", path, atomicfile.ErrSymlink)
	}
	return nil
}
