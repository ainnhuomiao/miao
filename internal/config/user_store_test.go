package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ainnhuomiao/miao/internal/atomicfile"
)

func TestUserSettingEditsPreserveOtherSectionsAndExactNumbers(t *testing.T) {
	t.Setenv("MIAO_HOME", t.TempDir())
	writeUserConfig(t, `{"theme":"mocha","subagents":{"enabled":true,"timeout_seconds":45},"hooks":{"stop":[]},"custom_id":9007199254740993}`)
	if err := SetUserValue("sessions_enabled", json.RawMessage(`false`)); err != nil {
		t.Fatal(err)
	}
	if err := SetUserValue("allowed_tools", json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	path, _ := UserConfigPath()
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "9007199254740993") || !strings.Contains(string(data), `"theme": "mocha"`) {
		t.Fatalf("unrelated values or exact numeric IDs were lost: %s", data)
	}
	user, err := LoadUserFile()
	if err != nil || user.SessionsEnabled == nil || *user.SessionsEnabled || user.AllowedTools == nil || len(user.AllowedTools) != 0 {
		t.Fatalf("explicit false and empty allowlist were lost: %+v, %v", user, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("user config must be private: %v, %v", info, err)
	}
	if err := UnsetUserValue("sessions_enabled"); err != nil {
		t.Fatal(err)
	}
	user, _ = LoadUserFile()
	if user.SessionsEnabled != nil {
		t.Fatal("unset must restore the absence of a user override")
	}
}

func TestInvalidEditsNeverOverwriteOriginal(t *testing.T) {
	t.Setenv("MIAO_HOME", t.TempDir())
	const original = `{"max_retries":3,"theme":"mocha"}`
	writeUserConfig(t, original)
	for _, tc := range []struct{ key, value string }{
		{"api_key", `"private"`}, {"unknown_typo", `false`}, {"max_retries", `-1`},
		{"sessions_enabled", `"false"`}, {"mode", `"unknown"`}, {"allowed_tools", `false`},
	} {
		if err := SetUserValue(tc.key, json.RawMessage(tc.value)); err == nil {
			t.Fatalf("invalid edit accepted: %s", tc.key)
		}
		path, _ := UserConfigPath()
		if data, _ := os.ReadFile(path); string(data) != original {
			t.Fatal("failed edit changed the original config")
		}
	}
	for _, broken := range []string{"{broken", "null", "[]"} {
		writeUserConfig(t, broken)
		if err := SetUserValue("max_retries", json.RawMessage(`0`)); err == nil {
			t.Fatal("malformed documents must not be silently replaced")
		}
		path, _ := UserConfigPath()
		if data, _ := os.ReadFile(path); string(data) != broken {
			t.Fatal("malformed original was overwritten")
		}
	}
}

func TestUnsetRepairsInvalidKnownField(t *testing.T) {
	t.Setenv("MIAO_HOME", t.TempDir())
	writeUserConfig(t, `{"max_retries":-1,"model":"keep"}`)
	if err := UnsetUserValue("max_retries"); err != nil {
		t.Fatal(err)
	}
	user, err := LoadUserFile()
	if err != nil || user.MaxRetries != nil || user.Model != "keep" {
		t.Fatal("unset should be able to repair a bad override without losing other fields")
	}
}

func TestConfigEditsRefuseSymlinks(t *testing.T) {
	for _, target := range []string{"config.json", ".config.lock", "home"} {
		t.Run(target, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("MIAO_HOME", home)
			victim := filepath.Join(t.TempDir(), "victim")
			if err := os.WriteFile(victim, []byte(`{"private":"untouched"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(home, target)
			if target == "home" {
				link = filepath.Join(t.TempDir(), "linked-home")
				victim = home
				t.Setenv("MIAO_HOME", link)
			}
			if err := os.Symlink(victim, link); err != nil {
				t.Fatal(err)
			}
			if err := SetUserValue("bell_enabled", json.RawMessage(`false`)); !errors.Is(err, atomicfile.ErrSymlink) {
				t.Fatalf("symbolic links must be rejected: %v", err)
			}
			if target != "home" {
				if data, _ := os.ReadFile(victim); string(data) != `{"private":"untouched"}` {
					t.Fatal("symlink target changed")
				}
			}
		})
	}
}

func TestConcurrentUserConfigUpdatesKeepEveryField(t *testing.T) {
	t.Setenv("MIAO_HOME", t.TempDir())
	const count = 24
	errors := make(chan error, count)
	var workers sync.WaitGroup
	for i := range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			errors <- UpdateUserConfig(func(all map[string]json.RawMessage) error {
				all[fmt.Sprint(i)] = json.RawMessage(fmt.Sprint(i))
				return nil
			})
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	path, _ := UserConfigPath()
	data, err := os.ReadFile(path)
	var got map[string]int
	if err != nil || json.Unmarshal(data, &got) != nil || len(got) != count {
		t.Fatalf("concurrent read-modify-writes lost keys: %s, %v", data, err)
	}
}
