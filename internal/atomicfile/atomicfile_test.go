package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWritePrivatePerm(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "a.json")
	if err := WritePrivate(p, []byte("x")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", info.Mode().Perm())
	}
}

func TestWritePrivateRefusesSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("orig"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "a.json")
	if err := os.Symlink(victim, p); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivate(p, []byte("evil")); !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v, want ErrSymlink", err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "orig" {
		t.Fatalf("victim changed: %q", b)
	}
}

func TestWritePrivateRefusesSymlinkDir(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivate(filepath.Join(link, "a.json"), []byte("x")); !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v, want ErrSymlink", err)
	}
}

func TestWritePrivateConcurrent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.json")
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- WritePrivate(p, []byte(fmt.Sprintf("v%d", i)))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("leftover temp files: %v", ents)
	}
}
