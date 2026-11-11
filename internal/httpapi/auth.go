package httpapi

import (
	"errors"
	"net"
	"net/http"
	"portexa/internal/auth"
	"sync"
	"time"
)

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	principal, err := s.principal(r)
	if err != nil {
		s.clearCookie(w)
		writeJSON(w, 200, map[string]bool{"authenticated": false})
		return
	}
	writeJSON(w, 200, map[string]any{"authenticated": true, "csrf_token": principal.CSRF, "expires_at_ms": principal.Session.ExpiresAtMS, "idle_expires_at_ms": principal.Session.IdleExpiresAtMS})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(r.RemoteAddr) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, 429, map[string]string{"error": "尝试过于频繁，请稍后再试"})
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	select {
	case s.hashSlots <- struct{}{}:
		defer func() { <-s.hashSlots }()
	default:
		writeJSON(w, 429, map[string]string{"error": "登录繁忙，请稍后再试"})
		return
	}
	token, principal, err := s.auth.Login(r.Context(), input.Password)
	if err != nil {
		fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: token, Path: "/", HttpOnly: true, Secure: !s.config.DevelopmentHTTP,
		SameSite: http.SameSiteStrictMode, Expires: time.UnixMilli(principal.Session.ExpiresAtMS), MaxAge: int(time.Until(time.UnixMilli(principal.Session.ExpiresAtMS)).Seconds())})
	writeJSON(w, 200, map[string]any{"authenticated": true, "csrf_token": principal.CSRF, "expires_at_ms": principal.Session.ExpiresAtMS, "idle_expires_at_ms": principal.Session.IdleExpiresAtMS})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName, Path: "/", HttpOnly: true, Secure: !s.config.DevelopmentHTTP, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Logout(r.Context(), principal(r)); err != nil {
		fail(w, err)
		return
	}
	s.clearCookie(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) touch(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Touch(r.Context(), principal(r)); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) password(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(r.RemoteAddr) {
		writeJSON(w, 429, map[string]string{"error": "尝试过于频繁，请稍后再试"})
		return
	}
	var input struct {
		Old string `json:"old_password"`
		New string `json:"new_password"`
	}
	if !decode(w, r, &input) {
		return
	}
	select {
	case s.hashSlots <- struct{}{}:
		defer func() { <-s.hashSlots }()
	default:
		writeJSON(w, 429, map[string]string{"error": "服务繁忙"})
		return
	}
	if err := s.auth.ChangePassword(r.Context(), input.Old, input.New); err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			writeJSON(w, 400, map[string]string{"error": "当前密码不正确"})
			return
		}
		fail(w, err)
		return
	}
	s.clearCookie(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type attempt struct {
	count int
	until time.Time
}
type loginLimiter struct {
	mu     sync.Mutex
	ips    map[string]attempt
	global attempt
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{ips: map[string]attempt{}} }

func (l *loginLimiter) allow(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, entry := range l.ips {
		if now.After(entry.until) {
			delete(l.ips, ip)
		}
	}
	if now.After(l.global.until) {
		l.global = attempt{until: now.Add(time.Minute)}
	}
	if l.global.count >= 20 {
		return false
	}
	entry := l.ips[host]
	if now.After(entry.until) {
		entry = attempt{until: now.Add(5 * time.Minute)}
	}
	if entry.count >= 5 || (len(l.ips) >= 4096 && entry.count == 0) {
		return false
	}
	entry.count++
	l.global.count++
	l.ips[host] = entry
	return true
}
