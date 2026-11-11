package httpapi

import (
	"net/http"
	"strconv"

	"portexa/internal/accounts"
)

func (s *Server) syncHistory(w http.ResponseWriter, r *http.Request) {
	if err := s.accounts.RequestHistory(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	s.scheduler.Wake()
	writeJSON(w, 202, map[string]string{"state": "queued"})
}

func (s *Server) funds(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("sort") != "" {
		fail(w, &accounts.InputError{Message: "资金流水仅支持服务器时间和订单号倒序"})
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			fail(w, &accounts.InputError{Message: "查询条数无效"})
			return
		}
		limit = parsed
	}
	filter := r.URL.Query().Get("unclassified")
	if filter != "" && filter != "0" && filter != "1" {
		fail(w, &accounts.InputError{Message: "流水筛选无效"})
		return
	}
	page, err := s.accounts.Funds(r.Context(), r.PathValue("id"), r.URL.Query().Get("cursor"), limit, filter == "1")
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, page)
}

func (s *Server) classifyFund(w http.ResponseWriter, r *http.Request) {
	ticket, err := strconv.ParseInt(r.PathValue("ticket"), 10, 32)
	if err != nil || ticket <= 0 {
		fail(w, &accounts.InputError{Message: "流水订单号无效"})
		return
	}
	var input accounts.ClassificationInput
	if !decode(w, r, &input) {
		return
	}
	result, err := s.accounts.ClassifyFund(r.Context(), r.PathValue("id"), int32(ticket), input)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, result)
}
