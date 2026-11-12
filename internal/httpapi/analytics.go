package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"portexa/internal/accounts"
)

func analyticsInput(r *http.Request, activity bool) (accounts.AnalyticsRequest, int, error) {
	query := r.URL.Query()
	input := accounts.AnalyticsRequest{Unit: "USD"}
	for key, values := range query {
		if len(values) != 1 || key != "from_ms" && key != "to_ms" && key != "account_id" && key != "unit" && !(activity && key == "offset") {
			return input, 0, &accounts.InputError{Message: "分析查询参数无效"}
		}
	}
	var err error
	input.FromMS, err = strconv.ParseInt(query.Get("from_ms"), 10, 64)
	if err == nil {
		input.ToMS, err = strconv.ParseInt(query.Get("to_ms"), 10, 64)
	}
	if err != nil {
		return input, 0, &accounts.InputError{Message: "请提供有效的分析起止时间"}
	}
	input.AccountID = query.Get("account_id")
	if query.Has("unit") {
		input.Unit = query.Get("unit")
	}
	offset := 0
	if activity && query.Has("offset") {
		offset, err = strconv.Atoi(query.Get("offset"))
		if err != nil {
			return input, 0, &accounts.InputError{Message: "明细分页参数无效"}
		}
	}
	return input, offset, nil
}

func (s *Server) analytics(w http.ResponseWriter, r *http.Request) {
	input, _, err := analyticsInput(r, false)
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := s.accounts.Analytics(ctx, input)
	if analyticsFailure(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) analyticsActivity(w http.ResponseWriter, r *http.Request) {
	input, offset, err := analyticsInput(r, true)
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := s.accounts.AnalyticsActivity(ctx, input, offset)
	if analyticsFailure(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) matchAnalyticsTransfer(w http.ResponseWriter, r *http.Request) {
	var input accounts.TransferInput
	if !decode(w, r, &input) {
		return
	}
	result, err := s.accounts.MatchAnalyticsTransfer(r.Context(), input)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func analyticsFailure(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		writeJSON(w, 503, map[string]string{"error": "分析查询超时，请缩短时间范围后重试"})
	} else if errors.Is(err, accounts.ErrAnalyticsChanged) {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
	} else {
		fail(w, err)
	}
	return true
}
