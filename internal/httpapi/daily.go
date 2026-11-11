package httpapi

import (
	"net/http"

	"portexa/internal/accounts"
)

func (s *Server) confirmServerTime(w http.ResponseWriter, r *http.Request) {
	var input accounts.ServerTimeInput
	if !decode(w, r, &input) {
		return
	}
	_, err := s.accounts.ConfirmServerTime(r.Context(), r.PathValue("id"), input)
	if err != nil {
		fail(w, err)
		return
	}
	s.scheduler.Wake()
	writeJSON(w, 200, map[string]bool{"confirmed": true})
}

func (s *Server) syncDaily(w http.ResponseWriter, r *http.Request) {
	if err := s.accounts.RequestDaily(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	s.scheduler.Wake()
	writeJSON(w, 202, map[string]string{"state": "queued"})
}
