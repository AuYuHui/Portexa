// Package discovery retrieves MT4 connection candidates without account credentials.
// It never authenticates to trading servers or changes the frozen protocol SDK.
package discovery

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const SourceURL = "https://updates.metaquotes.net/public/mt4/network"
const SourceName = "MetaQuotes MT4 服务器目录"
const MaxBody = 2 << 20

var ErrUnavailable = errors.New("服务器目录暂不可用，请稍后重试")
var ErrQuery = errors.New("请输入2至80个字符的经纪商或服务器名称")
var ErrBusy = errors.New("服务器搜索繁忙，请稍后重试")
var ErrExpired = errors.New("候选配置已过期，请重新搜索后选择")

// Public terminal compatibility bytes, not a Portexa secret or an identity proof.
// Only the directory query is signed. No account, password or device data is sent.
var directorySalt = [...]byte{61, 123, 21, 22, 214, 234, 187, 52, 217, 214, 99, 227, 98, 62, 27, 215, 251, 220, 174, 244, 87, 59, 223, 53, 127, 168, 207, 11, 190, 173, 146, 127}

type Fetcher func(context.Context, string) ([]byte, error)

func HTTPFetcher(client *http.Client) Fetcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrUnavailable }
	return func(ctx context.Context, query string) ([]byte, error) {
		// The upstream signature uses decoded values in this exact order. Encode
		// the wire form separately so spaces, '&' and Unicode remain one value.
		inner := md5.Sum([]byte("company=" + query + "&code=mt4"))
		signatureInput := append(inner[:], directorySalt[:]...)
		signature := md5.Sum(signatureInput)
		// Preserve the terminal-compatible company/code field ordering rather
		// than alphabetizing the wire form with url.Values.Encode.
		form := "company=" + url.QueryEscape(query) + "&code=mt4&signature=" + hex.EncodeToString(signature[:]) + "&ver=2"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, SourceURL, strings.NewReader(form))
		if err != nil {
			return nil, ErrUnavailable
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "MetaTrader 4 Terminal/4.1492 (Windows NT 10.0.22621; x64)")
		response, err := bounded.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrUnavailable
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, ErrUnavailable
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, MaxBody+1))
		if err != nil || len(body) > MaxBody {
			return nil, ErrUnavailable
		}
		return body, nil
	}
}
