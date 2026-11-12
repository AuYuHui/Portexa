// Package httpapi exposes authenticated, same-origin product APIs.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/auth"
	"portexa/internal/discovery"
	"portexa/internal/storage"
	"portexa/internal/vault"
)

type Scheduler interface {
	Wake()
	Cancel(string)
}

type Config struct {
	Origin          string
	DevelopmentHTTP bool
	Static          fs.FS
	Logger          *slog.Logger
	Directory       *discovery.Catalog
}

type Server struct {
	auth       *auth.Service
	accounts   *accounts.Service
	scheduler  Scheduler
	config     Config
	host       string
	cookieName string
	limiter    *loginLimiter
	hashSlots  chan struct{}
	mux        *http.ServeMux
}

type principalKey struct{}

func New(a *auth.Service, service *accounts.Service, scheduler Scheduler, config Config) (*Server, error) {
	u, err := url.Parse(config.Origin)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, errors.New("origin must be an exact scheme and host")
	}
	if config.DevelopmentHTTP {
		if u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" && u.Hostname() != "localhost") {
			return nil, errors.New("development HTTP requires a loopback origin")
		}
	} else if u.Scheme != "https" {
		return nil, errors.New("HTTPS origin required")
	}
	if config.Directory == nil {
		config.Directory = discovery.New(discovery.Options{AllowTarget: service.AllowTarget})
	}
	s := &Server{auth: a, accounts: service, scheduler: scheduler, config: config, host: u.Host,
		cookieName: "__Host-portexa", limiter: newLoginLimiter(), hashSlots: make(chan struct{}, 2), mux: http.NewServeMux()}
	if config.DevelopmentHTTP {
		s.cookieName = "portexa_dev"
	}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.config.Logger != nil {
		observed := &responseLog{ResponseWriter: w}
		w = observed
		started := time.Now()
		defer func() {
			if failure := recover(); failure != nil {
				observed.status = http.StatusInternalServerError
				s.logRequest(r, observed, started)
				panic(failure)
			}
			s.logRequest(r, observed, started)
		}()
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if !s.config.DevelopmentHTTP {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	if !strings.EqualFold(r.Host, s.host) {
		writeJSON(w, http.StatusMisdirectedRequest, map[string]string{"error": "访问地址与配置不一致"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if r.Header.Get("Origin") != s.config.Origin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "请求来源无效"})
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) })
	s.mux.HandleFunc("GET /api/auth/session", s.session)
	s.mux.HandleFunc("POST /api/auth/login", s.login)
	s.mux.HandleFunc("POST /api/auth/logout", s.protected(s.logout))
	s.mux.HandleFunc("POST /api/auth/touch", s.protected(s.touch))
	s.mux.HandleFunc("PUT /api/auth/password", s.protected(s.password))
	s.mux.HandleFunc("GET /api/overview", s.protected(s.overview))
	s.mux.HandleFunc("GET /api/analytics", s.protected(s.analytics))
	s.mux.HandleFunc("GET /api/analytics/activity", s.protected(s.analyticsActivity))
	s.mux.HandleFunc("POST /api/analytics/transfers", s.protected(s.matchAnalyticsTransfer))
	s.mux.HandleFunc("GET /api/portfolio/equity-history", s.protected(s.assetTrend))
	s.mux.HandleFunc("GET /api/settings/collection", s.protected(s.collectionSettings))
	s.mux.HandleFunc("PUT /api/settings/collection", s.protected(s.updateCollectionSettings))
	s.mux.HandleFunc("GET /api/servers", s.protected(s.servers))
	s.mux.HandleFunc("GET /api/server-directory", s.protected(s.searchDirectory))
	s.mux.HandleFunc("POST /api/servers/discovered", s.protected(s.createDiscovered))
	s.mux.HandleFunc("POST /api/servers/{id}/discovered-endpoints", s.protected(s.newDiscoveredEndpoint))
	s.mux.HandleFunc("POST /api/servers", s.protected(s.createServer))
	s.mux.HandleFunc("POST /api/servers/{id}/endpoints", s.protected(s.newEndpoint))
	s.mux.HandleFunc("PUT /api/servers/{id}/time-zone", s.protected(s.confirmServerTime))
	s.mux.HandleFunc("POST /api/endpoints/{id}/review", s.protected(s.review))
	s.mux.HandleFunc("GET /api/accounts", s.protected(s.list))
	s.mux.HandleFunc("POST /api/accounts", s.protected(s.createAccount))
	s.mux.HandleFunc("GET /api/accounts/{id}", s.protected(s.detail))
	s.mux.HandleFunc("PATCH /api/accounts/{id}", s.protected(s.patchAccount))
	s.mux.HandleFunc("POST /api/accounts/{id}/sync", s.protected(s.syncAccount))
	s.mux.HandleFunc("POST /api/accounts/{id}/daily/sync", s.protected(s.syncDaily))
	s.mux.HandleFunc("POST /api/accounts/{id}/history/sync", s.protected(s.syncHistory))
	s.mux.HandleFunc("GET /api/accounts/{id}/funds", s.protected(s.funds))
	s.mux.HandleFunc("PUT /api/accounts/{id}/funds/{ticket}/classification", s.protected(s.classifyFund))
	s.mux.HandleFunc("GET /api/accounts/{id}/snapshots", s.protected(s.snapshots))
	s.mux.HandleFunc("GET /api/accounts/{id}/equity-history", s.protected(s.assetTrend))
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		s.protected(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 404, map[string]string{"error": "接口不存在"})
		})(w, r)
	})
	s.mux.HandleFunc("/", s.static)
}

func (s *Server) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := s.principal(r)
		if err != nil {
			s.clearCookie(w)
			writeJSON(w, 401, map[string]string{"error": "请登录后继续"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.auth.ValidCSRF(principal, r.Header.Get("X-CSRF-Token")) {
			writeJSON(w, 403, map[string]string{"error": "会话校验失败，请刷新页面"})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	}
}

func (s *Server) principal(r *http.Request) (auth.Principal, error) {
	cookie, err := r.Cookie(s.cookieName)
	if err != nil {
		return auth.Principal{}, auth.ErrUnauthorized
	}
	return s.auth.Check(r.Context(), cookie.Value)
}

func principal(r *http.Request) auth.Principal {
	return r.Context().Value(principalKey{}).(auth.Principal)
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeJSON(w, 415, map[string]string{"error": "请使用 JSON 请求"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		writeJSON(w, 400, map[string]string{"error": "请求字段或格式无效"})
		return false
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		writeJSON(w, 400, map[string]string{"error": "请求只能包含一个 JSON 对象"})
		return false
	}
	return true
}

func fail(w http.ResponseWriter, err error) {
	code, message := 500, "操作暂时失败，请稍后重试"
	var input *accounts.InputError
	switch {
	case errors.As(err, &input):
		code, message = 400, input.Message
	case errors.Is(err, auth.ErrPassword):
		code, message = 400, err.Error()
	case errors.Is(err, auth.ErrUnauthorized), errors.Is(err, storage.ErrDenied):
		code, message = 401, "登录已失效或密码错误"
	case errors.Is(err, storage.ErrNotFound):
		code, message = 404, "记录不存在"
	case errors.Is(err, storage.ErrConflict):
		code, message = 409, "配置已变化，请刷新后重试"
	case errors.Is(err, discovery.ErrQuery):
		code, message = 400, err.Error()
	case errors.Is(err, discovery.ErrExpired):
		code, message = 409, err.Error()
	case errors.Is(err, discovery.ErrBusy):
		code, message = 429, err.Error()
	case errors.Is(err, discovery.ErrUnavailable):
		code, message = 502, err.Error()
	case errors.Is(err, accounts.ErrEndpoint), errors.Is(err, accounts.ErrTarget), errors.Is(err, accounts.ErrPaused):
		code, message = 409, err.Error()
	case errors.Is(err, accounts.ErrServerTime):
		code, message = 409, err.Error()
	case errors.Is(err, vault.ErrUnavailable), errors.Is(err, vault.ErrDecrypt):
		code, message = 503, "凭据密钥不可用，已停止接入；请检查独立密钥文件"
	default:
		// Expose a stable duplicate-identity result without reflecting SQL or data.
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			code, message = 409, "该服务器或账户已存在"
		}
	}
	writeJSON(w, code, map[string]string{"error": message})
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(405)
		return
	}
	if s.config.Static == nil {
		http.Error(w, "前端尚未构建，请运行 make web-build", 503)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if !fs.ValidPath(path) || strings.HasPrefix(path, ".") {
		http.NotFound(w, r)
		return
	}
	if _, err := fs.Stat(s.config.Static, path); err != nil {
		if strings.Contains(path, ".") || strings.HasPrefix(path, "assets/") {
			http.NotFound(w, r)
			return
		}
		path = "index.html"
	}
	data, err := fs.ReadFile(s.config.Static, path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if path == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, path, time.Time{}, strings.NewReader(string(data)))
}
