package wire_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/digineat/go-metatrader4/mt4"
)

// This experiment observes the actual upstream client on loopback. The local
// responder is deliberately not an MT4 server and never authenticates anyone.
func TestUpstreamWireBehavior(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		wantErr  bool
	}{
		{
			name:     "accepts_unauthenticated_local_stub",
			response: base64.StdEncoding.EncodeToString([]byte("LOCAL_STUB_ONLY")),
		},
		{
			name:     "rejects_invalid_base64",
			response: "%%%invalid-base64%%%",
			wantErr:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}

			type capture struct {
				firstLine string
				quitLine  string
				err       error
			}
			observed := make(chan capture, 1)
			go func() {
				var result capture
				defer func() { observed <- result }()
				conn, err := listener.Accept()
				if err != nil {
					result.err = err
					return
				}
				defer func() { _ = conn.Close() }()
				if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					result.err = err
					return
				}
				reader := bufio.NewReader(conn)
				result.firstLine, result.err = reader.ReadString('\n')
				if result.err != nil {
					return
				}
				result.quitLine, result.err = reader.ReadString('\n')
				if result.err != nil {
					return
				}
				_, result.err = fmt.Fprint(conn, tc.response)
			}()

			addr := listener.Addr().(*net.TCPAddr)
			client := mt4.NewClient("127.0.0.1", addr.Port)
			t.Cleanup(func() { _ = client.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			// Fictional fixtures only. Real account credentials must never be used
			// with this experiment or sent through this unaudited wire protocol.
			response, executeErr := client.Execute(ctx, "WWAPUSER", map[string]string{
				"login":    "10000000",
				"password": "local-fixture-only",
			})
			var result capture
			select {
			case result = <-observed:
			case <-ctx.Done():
				t.Fatal("local wire capture did not complete")
			}
			if result.err != nil {
				t.Fatalf("local responder: %v", result.err)
			}
			command, encoded, ok := strings.Cut(strings.TrimSuffix(result.firstLine, "\n"), " ")
			if !ok || command != "WWAPUSER" {
				t.Fatal("first client frame was not the expected plaintext command")
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("decode captured parameters: %v", err)
			}
			if string(decoded) != "login=10000000|password=local-fixture-only|" {
				t.Fatal("captured parameters did not recover the fictional credentials")
			}
			if result.quitLine != "QUIT\n" {
				t.Fatal("expected QUIT immediately after the command")
			}
			if tc.wantErr {
				if executeErr == nil {
					t.Fatal("malformed local response unexpectedly succeeded")
				}
			} else {
				if executeErr != nil {
					t.Fatalf("upstream Execute: %v", executeErr)
				}
				if response != "LOCAL_STUB_ONLY" {
					t.Fatal("upstream did not return the local stub marker")
				}
			}
			t.Log("plaintext_command=true fixture_password_recovered=true handshake_before_command=false live_mt4_verified=false")
		})
	}
}
