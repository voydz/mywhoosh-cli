// Package auth stores one account's bearer session, never its password.
package auth

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/voydz/mywhoosh-cli/internal/fault"
)

type Session struct {
	Version  int    `json:"version"`
	Token    string `json:"access_token"`
	WhooshID string `json:"whoosh_id"`
	DeviceID string `json:"device_id"`
}

var ErrMissing = errors.New("session missing")

type Store struct{ Dir string }

func configError() error {
	return fault.New("invalid_config", 2, "session store is malformed, inaccessible, symlinked or insecure; use a private directory (0700) and session file (0600)")
}

func (s Store) directory(create bool) error {
	info, err := os.Lstat(s.Dir)
	if os.IsNotExist(err) && create {
		if err := os.MkdirAll(s.Dir, 0700); err != nil {
			return configError()
		}
		info, err = os.Lstat(s.Dir)
	}
	if os.IsNotExist(err) {
		return ErrMissing
	}
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return configError()
	}
	return nil
}

func privateFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil, ErrMissing
		}
		return nil, configError()
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		_ = f.Close()
		return nil, configError()
	}
	return f, nil
}

func (s Store) Load() (Session, error) {
	var session Session
	if err := s.directory(false); err != nil {
		return session, err
	}
	f, err := privateFile(filepath.Join(s.Dir, "session.json"))
	if err != nil {
		return session, err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 65537))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&session) != nil || decoder.Decode(new(any)) != io.EOF || !valid(session) {
		return Session{}, configError()
	}
	return session, nil
}

func valid(s Session) bool {
	return s.Version == 1 && s.Token != "" && s.WhooshID != "" && s.DeviceID != ""
}

// lock covers only local mutations; it is never held during network access.
func (s Store) lock() (*os.File, error) {
	if err := s.directory(true); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Dir, "session.lock")
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, configError()
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		_ = f.Close()
		return nil, configError()
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		_ = f.Close()
		return nil, fault.New("session_busy", 1, "another process is updating the session; retry later")
	}
	return f, nil
}

func (s Store) Save(session Session) error {
	if !valid(session) {
		return configError()
	}
	lock, err := s.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	path := filepath.Join(s.Dir, "session.json")
	if f, err := privateFile(path); err == nil {
		_ = f.Close()
	} else if !errors.Is(err, ErrMissing) {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".session-*")
	if err != nil {
		return configError()
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := json.NewEncoder(f).Encode(session); err != nil {
		return configError()
	}
	if err := f.Sync(); err != nil {
		return configError()
	}
	if err := f.Close(); err != nil {
		return configError()
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return configError()
	}
	return nil
}

func (s Store) Delete() error {
	if err := s.directory(false); errors.Is(err, ErrMissing) {
		return nil
	} else if err != nil {
		return err
	}
	lock, err := s.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	f, err := privateFile(filepath.Join(s.Dir, "session.json"))
	if errors.Is(err, ErrMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = f.Close()
	if os.Remove(filepath.Join(s.Dir, "session.json")) != nil {
		return configError()
	}
	return nil
}
