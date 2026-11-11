package httpapi

import (
	"net/http"

	"portexa/internal/accounts"
)

func (s *Server) collectionSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.accounts.CollectionSettings(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) updateCollectionSettings(w http.ResponseWriter, r *http.Request) {
	var input accounts.CollectionInput
	if !decode(w, r, &input) {
		return
	}
	settings, err := s.accounts.UpdateCollectionSettings(r.Context(), input)
	if err != nil {
		fail(w, err)
		return
	}
	s.scheduler.Wake()
	writeJSON(w, http.StatusOK, settings)
}
