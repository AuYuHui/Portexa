package app_test

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"portexa/internal/vault"
)

func TestPersistentVaultReservationsAndTamperResistance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "credentials.key")
	if _, err := vault.Load(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing key cause is not preserved", err)
	}
	key, err := vault.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	aad := vault.AAD("a", "s", "e", 1)
	first, err := key.Seal("synthetic", aad)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := vault.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loaded.Open(first, loaded.ID(), vault.AAD("a", "s", "other-endpoint", 1)); err == nil {
		t.Fatal("wrong AAD accepted")
	}
	first[len(first)-1] ^= 1
	if _, err = loaded.Open(first, loaded.ID(), aad); err == nil {
		t.Fatal("tampered envelope accepted")
	}
	if _, err = loaded.Seal("synthetic-2", aad); err != nil {
		t.Fatal(err)
	}
	count, err := os.ReadFile(path + ".usage")
	if err != nil || binary.BigEndian.Uint64(count) != 2 {
		t.Fatal("reservation not durable", err)
	}
	binary.BigEndian.PutUint64(count, 1<<32)
	if err = os.WriteFile(path+".usage", count, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loaded.Seal("must-not-encrypt", aad); err == nil {
		t.Fatal("GCM message limit ignored")
	}
	if err = os.Remove(path + ".usage"); err != nil {
		t.Fatal(err)
	}
	if _, err = vault.Load(path); err == nil {
		t.Fatal("missing counter silently reset")
	}
}
