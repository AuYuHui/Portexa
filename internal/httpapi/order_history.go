package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"portexa/internal/accounts"
)

func (s *Server) orderHistory(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	invalid := &accounts.InputError{Message: "订单历史筛选或页码无效"}
	if err != nil {
		fail(w, invalid)
		return
	}
	for k, v := range q {
		switch k {
		case "from", "to", "symbol", "side", "result", "ticket", "page":
		default:
			fail(w, invalid)
			return
		}
		if len(v) != 1 {
			fail(w, invalid)
			return
		}
	}
	filter := accounts.OrderHistoryFilter{From: q.Get("from"), To: q.Get("to"), Side: q.Get("side"), Result: q.Get("result"), Symbol: strings.TrimSpace(q.Get("symbol")), Page: 1}
	if q.Has("page") {
		filter.Page, err = strconv.Atoi(q.Get("page"))
		if err != nil || filter.Page < 1 {
			fail(w, invalid)
			return
		}
	}
	if ticket := q.Get("ticket"); ticket != "" {
		filter.Ticket, err = strconv.ParseInt(ticket, 10, 64)
		if err != nil || filter.Ticket < 1 {
			fail(w, invalid)
			return
		}
	}
	page, err := s.accounts.OrderHistory(r.Context(), r.PathValue("id"), filter)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
