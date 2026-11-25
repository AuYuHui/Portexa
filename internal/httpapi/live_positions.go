package httpapi

import (
	"net/http"

	"portexa/internal/accounts"
)

func (s *Server) livePositions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	for key, values := range query {
		if len(values) != 1 || (key != "symbol" && key != "side" && key != "loss_only") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "不支持此查询参数"})
			return
		}
	}
	if query.Has("loss_only") && query.Get("loss_only") != "true" && query.Get("loss_only") != "false" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "不支持此查询参数"})
		return
	}
	view, err := s.accounts.LivePositions(r.Context(), r.PathValue("id"), accounts.PositionFilter{
		Side: query.Get("side"), Symbol: query.Get("symbol"), LossOnly: query.Get("loss_only") == "true",
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
