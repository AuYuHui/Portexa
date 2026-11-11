package httpapi

import (
	"net/http"
	"strconv"

	"portexa/internal/accounts"
)

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeJSON(w, 400, map[string]string{"error": "总览不接受排序参数"})
		return
	}
	view, err := s.accounts.Overview(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) servers(w http.ResponseWriter, r *http.Request) {
	list, err := s.accounts.Servers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) createServer(w http.ResponseWriter, r *http.Request) {
	var input accounts.ServerInput
	if !decode(w, r, &input) {
		return
	}
	server, endpoint, err := s.accounts.CreateServer(r.Context(), input)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"server": server, "endpoint": endpoint})
}

func (s *Server) newEndpoint(w http.ResponseWriter, r *http.Request) {
	var input accounts.EndpointInput
	if !decode(w, r, &input) {
		return
	}
	endpoint, paused, err := s.accounts.NewEndpoint(r.Context(), r.PathValue("id"), input)
	if err != nil {
		fail(w, err)
		return
	}
	for _, id := range paused {
		s.scheduler.Cancel(id)
	}
	writeJSON(w, 201, endpoint)
}

func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	var input accounts.ReviewInput
	if !decode(w, r, &input) {
		return
	}
	endpoint, err := s.accounts.Review(r.Context(), r.PathValue("id"), input)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, endpoint)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeJSON(w, 400, map[string]string{"error": "账户列表不接受排序参数"})
		return
	}
	list, err := s.accounts.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var input accounts.AccountInput
	if !decode(w, r, &input) {
		return
	}
	account, err := s.accounts.CreateAccount(r.Context(), input)
	if err != nil {
		fail(w, err)
		return
	}
	s.scheduler.Wake()
	view, err := s.accounts.Detail(r.Context(), account.ID, "")
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 201, view.Account)
}

func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	for key := range query {
		if key != "sort" {
			writeJSON(w, 400, map[string]string{"error": "不支持此查询参数"})
			return
		}
	}
	view, err := s.accounts.Detail(r.Context(), r.PathValue("id"), query.Get("sort"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) patchAccount(w http.ResponseWriter, r *http.Request) {
	var patch accounts.AccountPatch
	if !decode(w, r, &patch) {
		return
	}
	if patch.ConfigVersion == nil {
		writeJSON(w, 400, map[string]string{"error": "缺少配置版本，请刷新后重试"})
		return
	}
	id := r.PathValue("id")
	if err := s.accounts.UpdateAccount(r.Context(), id, patch); err != nil {
		fail(w, err)
		return
	}
	s.scheduler.Cancel(id)
	view, err := s.accounts.Detail(r.Context(), id, "")
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, view.Account)
}

func (s *Server) syncAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.accounts.RequestSync(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	s.scheduler.Wake()
	writeJSON(w, 202, map[string]string{"state": "queued"})
}

func (s *Server) snapshots(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	for key := range query {
		if key != "before" && key != "limit" {
			writeJSON(w, 400, map[string]string{"error": "只支持采样时间分页"})
			return
		}
	}
	limit, before := 60, int64(0)
	var err error
	if raw := query.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err == nil && query.Get("before") != "" {
		before, err = strconv.ParseInt(query.Get("before"), 10, 64)
	}
	if err != nil || before < 0 {
		writeJSON(w, 400, map[string]string{"error": "分页参数无效"})
		return
	}
	list, err := s.accounts.Snapshots(r.Context(), r.PathValue("id"), before, limit)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, list)
}
