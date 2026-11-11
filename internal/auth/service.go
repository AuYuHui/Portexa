// Package auth provides single-password authentication and revocable sessions.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"time"

	"portexa/internal/storage"
)

type Options struct {
	Now             func() time.Time
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
}

type Service struct {
	store *storage.Store
	now   func() time.Time
	idle  time.Duration
	abs   time.Duration
}

type Principal struct {
	Session storage.Session
	CSRF    string
}

func New(store *storage.Store, options Options) *Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = 30 * time.Minute
	}
	if options.AbsoluteTimeout <= 0 {
		options.AbsoluteTimeout = 8 * time.Hour
	}
	if options.IdleTimeout > options.AbsoluteTimeout {
		options.IdleTimeout = options.AbsoluteTimeout
	}
	return &Service{store: store, now: options.Now, idle: options.IdleTimeout, abs: options.AbsoluteTimeout}
}

func (s *Service) Initialize(ctx context.Context, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.store.SetPassword(ctx, hash, nil, s.now().UnixMilli())
}

func (s *Service) Login(ctx context.Context, password string) (string, Principal, error) {
	settings, err := s.store.Auth(ctx)
	if err != nil || !VerifyPassword(password, settings.PasswordHash) {
		return "", Principal{}, ErrUnauthorized
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", Principal{}, err
	}
	hash := sha256.Sum256(raw)
	now := s.now()
	session := storage.Session{
		TokenSHA256: hash[:], AuthVersion: settings.AuthVersion,
		CreatedAtMS: now.UnixMilli(), LastInteractiveAtMS: now.UnixMilli(),
		IdleExpiresAtMS: now.Add(s.idle).UnixMilli(), ExpiresAtMS: now.Add(s.abs).UnixMilli(),
	}
	if err = s.store.CreateSession(ctx, session); err != nil {
		return "", Principal{}, ErrUnauthorized
	}
	return base64.RawURLEncoding.EncodeToString(raw), Principal{Session: session, CSRF: csrf(raw)}, nil
}

func (s *Service) Check(ctx context.Context, token string) (Principal, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return Principal{}, ErrUnauthorized
	}
	hash := sha256.Sum256(raw)
	session, err := s.store.Session(ctx, hash[:], s.now().UnixMilli())
	if err != nil {
		return Principal{}, ErrUnauthorized
	}
	return Principal{Session: session, CSRF: csrf(raw)}, nil
}

func (s *Service) ValidCSRF(principal Principal, token string) bool {
	return len(token) > 0 && subtle.ConstantTimeCompare([]byte(principal.CSRF), []byte(token)) == 1
}

func (s *Service) Logout(ctx context.Context, principal Principal) error {
	return s.store.RevokeSession(ctx, principal.Session.TokenSHA256, s.now().UnixMilli())
}

func (s *Service) Touch(ctx context.Context, principal Principal) error {
	now := s.now()
	return s.store.TouchSession(ctx, principal.Session, now.UnixMilli(), now.Add(s.idle).UnixMilli())
}

func (s *Service) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	settings, err := s.store.Auth(ctx)
	if err != nil || !VerifyPassword(oldPassword, settings.PasswordHash) {
		return ErrUnauthorized
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.store.SetPassword(ctx, hash, &settings.AuthVersion, s.now().UnixMilli())
}

func (s *Service) ResetPassword(ctx context.Context, password string) error {
	settings, err := s.store.Auth(ctx)
	if err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.store.SetPassword(ctx, hash, &settings.AuthVersion, s.now().UnixMilli())
}

// The token is derived from the opaque, random session secret. Only its digest
// is persisted; the CSRF value is never usable as an authentication cookie.
func csrf(raw []byte) string {
	mac := hmac.New(sha256.New, raw)
	mac.Write([]byte("portexa:csrf:v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
