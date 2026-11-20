package mt5_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"unicode/utf16"

	"github.com/gorilla/websocket"
	"portexa/mt5"
	"portexa/mt5/internal/terminal"
)

var le = binary.LittleEndian

const positionID = 8_000_000_001

func TestReadOnlySnapshotOverPinnedTLS(t *testing.T) {
	for _, tc := range []struct {
		name           string
		reportedServer string
		investor       bool
		truncated      bool
		hostname       string
		want           error
	}{
		{name: "complete", reportedServer: "Synthetic-MT5", investor: true},
		{name: "master account", reportedServer: "Synthetic-MT5", want: mt5.ErrReadOnly},
		{name: "different trade server", reportedServer: "Other-Synthetic", investor: true, want: mt5.ErrServerMismatch},
		{name: "truncated positions", reportedServer: "Synthetic-MT5", investor: true, truncated: true, want: mt5.ErrProtocol},
		{name: "wrong TLS identity", hostname: "wrong.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commandCount atomic.Int32
			backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/terminal" || r.TLS.ServerName != "example.com" {
					t.Error("unexpected path or TLS hostname")
					http.Error(w, "invalid test request", http.StatusBadRequest)
					return
				}
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				initial, _ := hex.DecodeString("02de02a1a65cc794684fcbea1ecb0fd74ae657e43662c11eee885d2fd64f4964")
				session := bytes.Repeat([]byte{42}, 32)
				exchange(t, conn, initial, 0, make([]byte, 64), append(make([]byte, 66), session...))
				commandCount.Add(1)

				login := exchange(t, conn, session, 28, nil, make([]byte, 168))
				commandCount.Add(1)
				if len(login) != 920 || le.Uint64(login[736:744]) != 7_000_000_001 ||
					le.Uint32(login[476:480]) != 11 || !bytes.Equal(login[480:502], textBytes("example.com")) ||
					!bytes.Equal(login[4:28], textBytes("synthetic-pw")) {
					t.Error("login did not encode the expected account and hostname")
				}
				account := make([]byte, 816)
				if tc.investor {
					le.PutUint32(account[1:], 8)
				}
				copy(account[25:], textBytes("USD"))
				copy(account[355:], textBytes(tc.reportedServer))
				setFloat(account, 9, 1000)
				setFloat(account, 17, 12)
				setFloat(account, 772, 22)
				le.PutUint32(account[93:], 100)
				exchange(t, conn, session, 3, nil, account)
				commandCount.Add(1)
				if !tc.investor || tc.reportedServer != "Synthetic-MT5" {
					if _, _, err := conn.ReadMessage(); err == nil {
						t.Error("account validation must stop further commands")
					}
					return
				}
				positions := make([]byte, 4+344+4+356)
				le.PutUint32(positions, 1)
				p := positions[4:]
				le.PutUint64(p, positionID)
				le.PutUint32(p[16:], 1_780_000_000)
				copy(p[24:], textBytes("TESTUSD"))
				le.PutUint32(p[88:], 1)
				setFloat(p, 92, 1.234)
				setFloat(p, 100, 1.235)
				le.PutUint64(p[124:], 123456)
				setFloat(p, 132, 5.5)
				setFloat(p, 156, -0.2)
				setFloat(p, 164, -0.1)
				le.PutUint32(p[260:], 3)
				le.PutUint32(positions[348:], 1)
				order := positions[352:]
				le.PutUint64(order, positionID)
				copy(order[72:], textBytes("TESTUSD"))
				le.PutUint32(order[136:], 1_780_000_001)
				le.PutUint32(order[148:], 6)
				setFloat(order, 164, 1.2)
				setFloat(order, 172, 1.3)
				le.PutUint64(order[204:], 200000)
				le.PutUint64(order[212:], 123456)
				le.PutUint32(order[312:], 3)
				if tc.truncated {
					positions = positions[:100]
				}
				exchange(t, conn, session, 4, nil, positions)
				commandCount.Add(1)
			}))
			defer backend.Close()
			roots := x509.NewCertPool()
			roots.AddCert(backend.Certificate())
			ip, portText, _ := net.SplitHostPort(backend.Listener.Addr().String())
			port, _ := strconv.Atoi(portText)
			host := tc.hostname
			if host == "" {
				host = "example.com"
			}
			result, err := terminal.Fetch(t.Context(), terminal.Config{
				IPAddress: ip, Port: port, Hostname: host, Server: "Synthetic-MT5",
				Login: 7_000_000_001, Password: "synthetic-pw",
			}, roots)
			if tc.hostname != "" {
				var verification x509.HostnameError
				if !errors.As(err, &verification) || commandCount.Load() != 0 {
					t.Fatalf("incorrect certificate was not rejected before protocol traffic: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("fetch error = %v, want %v", err, tc.want)
			}
			if err != nil {
				if len(result.Positions) != 0 || result.Account.Currency != "" {
					t.Fatal("failed account read returned partial data")
				}
				return
			}
			if result.Account.Balance != 1000 || result.Account.Credit != 12 || result.Account.Margin != 22 ||
				result.Account.Currency != "USD" || result.Account.Leverage != 100 || len(result.Positions) != 1 {
				t.Fatalf("unexpected account snapshot: %+v", result)
			}
			if len(result.Orders) != 1 {
				t.Fatal("pending order section was not decoded", result.Orders)
			}
			o := result.Orders[0]
			if o.Ticket != positionID || o.Symbol != "TESTUSD" || o.Type != 6 || o.Digits != 3 ||
				o.Price != 1.2 || o.TriggerPrice != 1.3 || o.RemainingVolumeUnits != 123456 || o.SetupTime != 1_780_000_001 {
				t.Fatalf("unexpected pending order: %+v", o)
			}
			p := result.Positions[0]
			if p.ID != positionID || p.Symbol != "TESTUSD" || p.Side != 1 || p.Digits != 3 ||
				p.VolumeUnits != 123456 || p.OpenTime != 1_780_000_000 || p.CurrentPrice != 1.235 ||
				p.OpenPrice != 1.234 || p.Profit != 5.5 || p.Commission != -0.2 || p.Swap != -0.1 {
				t.Fatalf("unexpected position: %+v", p)
			}
		})
	}
}

func TestInvalidConfigurationStopsBeforeDial(t *testing.T) {
	_, err := mt5.Fetch(context.Background(), mt5.Config{
		IPAddress: "203.0.113.9", Port: 443, Hostname: "https://mt5.broker.example/terminal",
		Server: "Synthetic-MT5", Login: 1, Password: "synthetic-pw",
	})
	if !errors.Is(err, mt5.ErrConfig) {
		t.Fatalf("invalid hostname error = %v", err)
	}
}

func textBytes(value string) []byte {
	units := utf16.Encode([]rune(value))
	data := make([]byte, 2*len(units))
	for i, unit := range units {
		le.PutUint16(data[2*i:], unit)
	}
	return data
}

func setFloat(b []byte, offset int, value float64) {
	le.PutUint64(b[offset:], math.Float64bits(value))
}

func exchange(t *testing.T, conn *websocket.Conn, key []byte, command uint16, expected, reply []byte) []byte {
	t.Helper()
	kind, frame, err := conn.ReadMessage()
	if err != nil {
		t.Error(err)
		return nil
	}
	if kind != websocket.BinaryMessage || len(frame) < 24 || len(frame[8:])%16 != 0 ||
		int(le.Uint32(frame)) != len(frame[8:]) || le.Uint32(frame[4:]) != 1 {
		t.Error("invalid outbound frame")
		return nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Error(err)
		return nil
	}
	iv := make([]byte, 16)
	ciphertext := frame[8:]
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(ciphertext, ciphertext)
	padding := int(ciphertext[len(ciphertext)-1])
	if padding < 1 || padding > 16 || len(ciphertext)-padding < 4 || le.Uint16(ciphertext[2:]) != command {
		t.Error("invalid command")
		return nil
	}
	payload := ciphertext[4 : len(ciphertext)-padding]
	if !bytes.Equal(payload, expected) && expected != nil {
		t.Error("unexpected command payload")
	}
	plain := make([]byte, 5, 5+len(reply)+16)
	le.PutUint16(plain[2:], command)
	plain = append(plain, reply...)
	pad := 16 - len(plain)%16
	plain = append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(plain, plain)
	frame = make([]byte, 8, 8+len(plain))
	le.PutUint32(frame, uint32(len(plain)))
	if err := conn.WriteMessage(websocket.BinaryMessage, append(frame, plain...)); err != nil {
		t.Error(err)
	}
	return payload
}
