package mt5_test

import (
	"bytes"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gorilla/websocket"
	"portexa/mt5"
	"portexa/mt5/internal/terminal"
)

func TestHistoryResponsesUseBrokerTimeAndExactDealFields(t *testing.T) {
	const start, end = 1_780_000_000, 1_780_000_010
	for _, truncated := range []bool{false, true} {
		name := "valid"
		if truncated {
			name = "truncated historical order"
		}
		t.Run(name, func(t *testing.T) {
			backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				initial, _ := hex.DecodeString("02de02a1a65cc794684fcbea1ecb0fd74ae657e43662c11eee885d2fd64f4964")
				session := bytes.Repeat([]byte{43}, 32)
				exchange(t, conn, initial, 0, make([]byte, 64), append(make([]byte, 66), session...))
				exchange(t, conn, session, 28, nil, make([]byte, 168))
				account := make([]byte, 816)
				le.PutUint32(account[1:], 8)
				copy(account[25:], textBytes("USD"))
				copy(account[355:], textBytes("Synthetic-MT5"))
				exchange(t, conn, session, 3, nil, account)

				history := make([]byte, 8+3*356+356)
				le.PutUint32(history, 3)
				le.PutUint32(history[4+3*356:], 1)
				for i, seconds := range []uint32{start - 1, start, end} {
					d := history[4+i*356 : 4+(i+1)*356]
					le.PutUint64(d, 8_000_000_010+uint64(i))
					le.PutUint64(d[72:], 8_000_000_021)
					le.PutUint32(d[80:], seconds)
					copy(d[88:], textBytes("TESTUSD"))
					le.PutUint32(d[152:], uint32(mt5.DealSell))
					le.PutUint32(d[156:], uint32(mt5.EntryInOut))
					setFloat(d, 160, 1.2345)
					setFloat(d, 168, 9.99)
					le.PutUint64(d[192:], 123456)
					setFloat(d, 200, 123.4)
					setFloat(d, 224, -1.2)
					setFloat(d, 232, -0.6)
					le.PutUint64(d[248:], 8_000_000_031)
					copy(d[256:], textBytes("synthetic reversal"))
					le.PutUint32(d[328:], 4)
					le.PutUint32(d[340:], 789)
					setFloat(d, 348, -0.1)
				}
				if truncated {
					history = history[:len(history)-1]
				}
				expected := le.AppendUint32(nil, start-24*3600)
				expected = le.AppendUint32(expected, end+24*3600)
				exchange(t, conn, session, 5, expected, history)
			}))
			defer backend.Close()
			roots := x509.NewCertPool()
			roots.AddCert(backend.Certificate())
			ip, rawPort, _ := net.SplitHostPort(backend.Listener.Addr().String())
			port, _ := strconv.Atoi(rawPort)
			result, err := terminal.FetchHistory(t.Context(), terminal.Config{
				IPAddress: ip, Port: port, Hostname: "example.com", Server: "Synthetic-MT5",
				Login: 7_000_000_001, Password: "synthetic-pw",
			}, terminal.HistoryRange{Start: start, End: end}, roots)
			if truncated {
				if !errors.Is(err, mt5.ErrProtocol) || len(result.Deals) != 0 {
					t.Fatalf("truncated result passed: %+v, %v", result, err)
				}
				return
			}
			if err != nil || len(result.Deals) != 1 || result.Account.Currency != "USD" {
				t.Fatalf("history bounds or response invalid: %+v, %v", result, err)
			}
			d := result.Deals[0]
			if d.Ticket != 8_000_000_011 || d.Order != 8_000_000_021 || d.PositionID != 8_000_000_031 ||
				d.WallTimeMS != start*1000+789 || d.Type != mt5.DealSell || d.Entry != mt5.EntryInOut ||
				d.Price != 1.2345 || d.VolumeUnits != 123456 || d.Digits != 4 || d.Profit != 123.4 ||
				d.Commission != -1.2 || d.Swap != -0.6 || d.Fee != -0.1 || d.Comment != "synthetic reversal" {
				t.Fatalf("wrong deal fields: %+v", d)
			}
		})
	}
}
