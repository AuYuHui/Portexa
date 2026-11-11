package app_test

import (
	"context"
	"testing"
	"time"

	"portexa/internal/auth"
	"portexa/internal/storage"
	"portexa/tests/testutil"
)

func testAuth(store *storage.Store) *auth.Service { return auth.New(store, auth.Options{}) }

func TestPasswordAndSessionLifecycle(t *testing.T) {
	if auth.ValidatePassword("123456789012345") == nil {
		t.Fatal("short password accepted")
	}
	if auth.ValidatePassword("一二三四五六七八九十一二三四五六") != nil {
		t.Fatal("unicode password rejected")
	}
	f := testutil.New(t, true)
	ctx := context.Background()
	now := time.Now()
	service := auth.New(f.Store, auth.Options{Now: func() time.Time { return now }, IdleTimeout: time.Minute, AbsoluteTimeout: 3 * time.Minute})
	token, p, err := service.Login(ctx, testutil.Password)
	if err != nil {
		t.Fatal(err)
	}
	if !service.ValidCSRF(p, p.CSRF) || service.ValidCSRF(p, token) {
		t.Fatal("invalid CSRF binding")
	}
	now = now.Add(50 * time.Second)
	if _, err = service.Check(ctx, token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	if _, err = service.Check(ctx, token); err == nil {
		t.Fatal("polling extended idle timeout")
	}
	token, p, err = service.Login(ctx, testutil.Password)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(50 * time.Second)
	if err = service.Touch(ctx, p); err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * time.Second)
	if _, err = service.Check(ctx, token); err != nil {
		t.Fatal("interaction did not extend idle", err)
	}
	if err = service.ChangePassword(ctx, testutil.Password, "new-synthetic-login-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Check(ctx, token); err == nil {
		t.Fatal("password change did not revoke session")
	}
	if _, _, err = service.Login(ctx, testutil.Password); err == nil {
		t.Fatal("old password accepted")
	}
	newToken, newP, err := service.Login(ctx, "new-synthetic-login-password")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Logout(ctx, newP); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Check(ctx, newToken); err == nil {
		t.Fatal("logout failed")
	}
}
