package vault

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// GCM's random-nonce construction permits at most 2^32 messages per key.
// Persist reservations before encryption, independently of database rollback.
const maxMessages uint64 = 1 << 32

func createUsage(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(make([]byte, 8))
	return errors.Join(err, file.Sync(), file.Close())
}

func readUsage(path string) (uint64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != 8 {
		return 0, ErrUnavailable
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) != 8 {
		return 0, ErrUnavailable
	}
	uses := binary.BigEndian.Uint64(data)
	if uses > maxMessages {
		return 0, ErrUnavailable
	}
	return uses, nil
}

func (v *Vault) reserve() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.usagePath == "" {
		if v.uses >= maxMessages {
			return ErrUnavailable
		}
		v.uses++
		return nil
	}
	lock, err := os.OpenFile(v.usagePath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return ErrUnavailable
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return ErrUnavailable
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	uses, err := readUsage(v.usagePath)
	if err != nil || uses >= maxMessages {
		return ErrUnavailable
	}
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, uses+1)
	temp, err := os.CreateTemp(filepath.Dir(v.usagePath), ".key-usage-")
	if err != nil {
		return ErrUnavailable
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(data)
	err = errors.Join(err, temp.Sync(), temp.Close())
	if err != nil {
		return ErrUnavailable
	}
	if err = os.Rename(temp.Name(), v.usagePath); err != nil {
		return ErrUnavailable
	}
	dir, err := os.Open(filepath.Dir(v.usagePath))
	if err != nil {
		return ErrUnavailable
	}
	if err = errors.Join(dir.Sync(), dir.Close()); err != nil {
		return ErrUnavailable
	}
	return nil
}
