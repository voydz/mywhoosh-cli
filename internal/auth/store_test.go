package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func session() Session { return Session{Version: 1, Token: "private-token", WhooshID: "00042", DeviceID: "device"} }

func TestStoreRejectsInsecureAndSymlinkTargets(t *testing.T) {
	for _, kind := range []string{"directory_mode", "directory_symlink", "file_mode", "file_symlink", "malformed", "version", "nonregular", "lock_symlink"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "config")
			s := Store{Dir: dir}
			if err := s.Save(session()); err != nil { t.Fatal(err) }
			path := filepath.Join(dir, "session.json")
			switch kind {
			case "directory_mode": _ = os.Chmod(dir, 0755)
			case "directory_symlink":
				_ = os.Rename(dir, dir+"-real")
				_ = os.Symlink(dir+"-real", dir)
			case "file_mode": _ = os.Chmod(path, 0644)
			case "file_symlink":
				_ = os.Rename(path, path+"-real")
				_ = os.Symlink(path+"-real", path)
			case "malformed": _ = os.WriteFile(path, []byte(`{"access_token":"private-token"}`), 0600)
			case "version": _ = os.WriteFile(path, []byte(`{"version":2,"access_token":"private-token","whoosh_id":"x","device_id":"y"}`), 0600)
			case "nonregular":
				_ = os.Remove(path)
				_ = os.Mkdir(path, 0600)
			case "lock_symlink":
				_ = os.Remove(filepath.Join(dir, "session.lock"))
				_ = os.Symlink(path, filepath.Join(dir, "session.lock"))
			}
			if kind == "lock_symlink" {
				if err := s.Save(session()); err == nil { t.Fatal("accepted symlinked lock") }
				return
			}
			_, err := s.Load()
			if err == nil || strings.Contains(err.Error(), "private-token") { t.Fatalf("unsafe load: %v", err) }
			if kind != "malformed" && kind != "version" {
				if err := s.Save(session()); err == nil { t.Fatal("accepted insecure write") }
			}
		})
	}
}

func TestStoreMissingAndLockContentionPreserveSession(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "config")}
	if _, err := s.Load(); !errors.Is(err, ErrMissing) { t.Fatal(err) }
	if err := s.Delete(); err != nil { t.Fatal(err) }
	if err := s.Save(session()); err != nil { t.Fatal(err) }
	f, err := os.OpenFile(filepath.Join(s.Dir, "session.lock"), os.O_RDWR, 0600)
	if err != nil { t.Fatal(err) }
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil { t.Fatal(err) }
	changed := session()
	changed.Token = "replacement"
	if err := s.Save(changed); err == nil { t.Fatal("write ignored advisory lock") }
	got, err := s.Load()
	if err != nil || got != session() { t.Fatal("failed update changed session") }
	entries, err := os.ReadDir(s.Dir)
	if err != nil { t.Fatal(err) }
	for _, e := range entries { if strings.HasPrefix(e.Name(), ".session-") { t.Fatal("temporary file leaked") } }
}
