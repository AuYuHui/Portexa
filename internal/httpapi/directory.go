package httpapi

import "net/http"

func (s *Server) searchDirectory(w http.ResponseWriter, r *http.Request) {
	for key := range r.URL.Query() {
		if key != "q" {
			writeJSON(w, 400, map[string]string{"error": "只支持经纪商或服务器名称搜索"})
			return
		}
	}
	list, err := s.config.Directory.Search(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, list)
}

type directorySelection struct {
	CandidateID   string `json:"candidate_id"`
	EndpointIndex int    `json:"endpoint_index"`
	AcceptRisk    bool   `json:"accept_risk"`
}

func (s *Server) createDiscovered(w http.ResponseWriter, r *http.Request) {
	var input directorySelection
	if !decode(w, r, &input) {
		return
	}
	candidate, err := s.config.Directory.Candidate(input.CandidateID)
	if err != nil {
		fail(w, err)
		return
	}
	server, endpoint, err := s.accounts.CreateDiscovered(r.Context(), candidate, input.EndpointIndex, input.AcceptRisk)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"server": server, "endpoint": endpoint})
}

func (s *Server) newDiscoveredEndpoint(w http.ResponseWriter, r *http.Request) {
	var input directorySelection
	if !decode(w, r, &input) {
		return
	}
	candidate, err := s.config.Directory.Candidate(input.CandidateID)
	if err != nil {
		fail(w, err)
		return
	}
	endpoint, paused, err := s.accounts.NewDiscoveredEndpoint(r.Context(), r.PathValue("id"), candidate, input.EndpointIndex, input.AcceptRisk)
	if err != nil {
		fail(w, err)
		return
	}
	for _, id := range paused {
		s.scheduler.Cancel(id)
	}
	writeJSON(w, 201, endpoint)
}
