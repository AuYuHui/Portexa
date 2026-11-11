package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"portexa/internal/accounts"
)

func trendInput(r *http.Request, portfolio bool) (accounts.TrendRequest, error) {
	query := r.URL.Query()
	input := accounts.TrendRequest{Unit: "native"}
	if portfolio {
		input.Unit = "USD"
	}
	for key, values := range query {
		if key != "from_ms" && key != "to_ms" && key != "unit" || len(values) != 1 {
			return input, &accounts.InputError{Message: "走势查询参数无效"}
		}
	}
	var err error
	input.FromMS, err = strconv.ParseInt(query.Get("from_ms"), 10, 64)
	if err == nil {
		input.ToMS, err = strconv.ParseInt(query.Get("to_ms"), 10, 64)
	}
	if err != nil {
		return input, &accounts.InputError{Message: "请提供有效的走势起止时间"}
	}
	if query.Has("unit") {
		input.Unit = query.Get("unit")
	}
	return input, nil
}

func (s *Server) assetTrend(w http.ResponseWriter, r *http.Request) {
	portfolio := r.PathValue("id") == ""
	input, err := trendInput(r, portfolio)
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var result accounts.TrendSeries
	if portfolio {
		result, err = s.accounts.PortfolioTrend(ctx, input)
	} else {
		result, err = s.accounts.AccountTrend(ctx, r.PathValue("id"), input)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "走势查询超时，请稍后重试"})
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
