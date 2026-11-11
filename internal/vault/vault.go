// Package vault protects investor credentials with an independently stored key.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrUnavailable = errors.New("credential key unavailable")
var ErrDecrypt = errors.New("credential could not be decrypted")

type Vault struct {
	aead      cipher.AEAD
	id        string
	mu        sync.Mutex
	uses      uint64
	usagePath string
}

func New(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrUnavailable
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	hash := sha256.Sum256(key)
	return &Vault{aead: aead, id: hex.EncodeToString(hash[:16])}, nil
}

func Load(path string) (*Vault, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrUnavailable
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrUnavailable
	}
	v, err := New(key)
	if err != nil {
		return nil, err
	}
	v.usagePath = path + ".usage"
	if _, err = readUsage(v.usagePath); err != nil {
		return nil, ErrUnavailable
	}
	return v, nil
}

func Create(path string) (*Vault, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, ErrUnavailable
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, ErrUnavailable
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, ErrUnavailable
	}
	_, writeErr := file.Write(key)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return nil, ErrUnavailable
	}
	if err := createUsage(path + ".usage"); err != nil {
		return nil, ErrUnavailable
	}
	return Load(path)
}

func (v *Vault) Ready() bool { return v != nil && v.aead != nil }

func (v *Vault) ID() string {
	if !v.Ready() {
		return ""
	}
	return v.id
}

func AAD(account, server, endpoint string, version int64) []byte {
	return []byte(fmt.Sprintf("portexa:credential:v1:%s:%s:%s:%d", account, server, endpoint, version))
}

func (v *Vault) Seal(password string, aad []byte) ([]byte, error) {
	if !v.Ready() {
		return nil, ErrUnavailable
	}
	if err := v.reserve(); err != nil {
		return nil, err
	}
	return v.aead.Seal(nil, nil, []byte(password), aad), nil
}

func (v *Vault) Open(envelope []byte, keyID string, aad []byte) (string, error) {
	if !v.Ready() {
		return "", ErrUnavailable
	}
	if keyID != v.id {
		return "", ErrDecrypt
	}
	plaintext, err := v.aead.Open(nil, nil, envelope, aad)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plaintext), nil
}
