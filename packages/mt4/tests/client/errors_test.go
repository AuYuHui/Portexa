package client_test

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"portexa/mt4"
	"portexa/mt4/tests/support"
)

func TestBuildCapabilitiesDoNotRequireBrokerOrBuildAllowlists(t *testing.T) {
	for _, build := range []uint16{1470, 1492, 1600} {
		address, completed := support.NewServer(t, support.Options{Authenticate: true, Build: build})
		result, err := mt4.Fetch(context.Background(), fixtureConfig(address), mt4.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := <-completed; err != nil {
			t.Fatal(err)
		}
		if !result.AccountRead || result.ServerBuild != build {
			t.Fatal("compatible build rejected or build metadata lost")
		}
	}
	address, completed := support.NewServer(t, support.Options{Build: 1419})
	_, err := mt4.Probe(context.Background(), fixtureConfig(address))
	requireCode(t, err, mt4.UnsupportedBuild, "initial")
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	var failure *mt4.Error
	if !errors.As(err, &failure) || failure.ServerBuild != 1419 {
		t.Fatal("unsupported build omitted diagnostic version")
	}
}

func TestInvestorDeclarationAndServerRejectionsAreDistinct(t *testing.T) {
	for _, passwordType := range []mt4.PasswordType{"", "master", "unknown"} {
		config := fixtureConfig("127.0.0.1:1")
		config.PasswordType = passwordType
		_, err := mt4.Fetch(context.Background(), config, mt4.Options{})
		requireCode(t, err, mt4.ReadOnlyRequired, "config")
	}
	address, completed := support.NewServer(t, support.Options{InitialStatus: 5})
	_, err := mt4.Probe(context.Background(), fixtureConfig(address))
	requireCode(t, err, mt4.ServerRejected, "initial")
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	var failure *mt4.Error
	if !errors.As(err, &failure) || failure.Status != 5 {
		t.Fatal("rejection lost the raw status")
	}
	for _, private := range []string{address, support.Password, support.Server} {
		if strings.Contains(err.Error(), private) {
			t.Fatal("public error exposed connection or credential data")
		}
	}
}

func TestTimeoutAndTruncatedConnectionRetainErrorIdentity(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			_, _ = io.CopyN(io.Discard, conn, 28)
			if disconnect {
				_, _ = conn.Write([]byte{0, 1})
				return
			}
			_, _ = io.Copy(io.Discard, conn)
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		_, err = mt4.Probe(ctx, fixtureConfig(listener.Addr().String()))
		cancel()
		listener.Close()
		<-done
		if disconnect {
			requireCode(t, err, mt4.ConnectionFailed, "initial")
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatal("truncation identity lost")
			}
		} else {
			requireCode(t, err, mt4.Timeout, "initial")
			var network net.Error
			if !errors.Is(err, context.DeadlineExceeded) && !(errors.As(err, &network) && network.Timeout()) {
				t.Fatal("deadline identity lost")
			}
		}
	}
}
