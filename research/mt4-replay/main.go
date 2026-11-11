// This experiment replays a captured initial client flight and records only
// the initial server response. It does not implement MT4 authentication.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

type probeInput struct {
	Target        string `json:"target"`
	InitialFlight []byte `json:"initial_flight"`
}

func run(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return errors.New("cannot read local probe input")
	}
	var input probeInput
	if err := json.Unmarshal(data, &input); err != nil {
		return errors.New("invalid local probe input")
	}
	// Accept only a short, separately extracted initial prefix. Its length
	// alone does not establish the protocol meaning of those bytes.
	if len(input.InitialFlight) == 0 || len(input.InitialFlight) > 64 {
		return errors.New("initial flight must contain 1 to 64 bytes")
	}
	host, _, err := net.SplitHostPort(input.Target)
	if err != nil || net.ParseIP(host) == nil {
		return errors.New("target must be the observed IP address and port")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", input.Target)
	if err != nil {
		return errors.New("TCP connection failed")
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return errors.New("cannot set socket deadline")
	}
	for sent := 0; sent < len(input.InitialFlight); {
		n, err := conn.Write(input.InitialFlight[sent:])
		if err != nil || n == 0 {
			return errors.New("initial flight write failed")
		}
		sent += n
	}
	// Bound the response and wait briefly for split TCP segments. Neither
	// payload nor a reusable session token is printed to the terminal.
	response := make([]byte, 0, 8192)
	buffer := make([]byte, 2048)
	for len(response) < cap(response) {
		n, readErr := conn.Read(buffer[:min(len(buffer), cap(response)-len(response))])
		if n > 0 {
			response = append(response, buffer[:n]...)
			if err := conn.SetReadDeadline(minDeadline(time.Now().Add(time.Second), deadline)); err != nil {
				return errors.New("cannot update response deadline")
			}
		}
		if readErr != nil {
			var timeout net.Error
			if !errors.Is(readErr, io.EOF) && !(errors.As(readErr, &timeout) && timeout.Timeout()) {
				return errors.New("initial response read failed")
			}
			break
		}
	}
	if len(response) == 0 {
		return errors.New("server returned no initial response")
	}
	file, err := os.CreateTemp(filepath.Dir(configPath), "probe-response-*.bin")
	if err != nil {
		return errors.New("cannot create private response file")
	}
	_, writeErr := file.Write(response)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("cannot save private response file")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"tcp_connected":           true,
		"initial_bytes_sent":      len(input.InitialFlight),
		"server_bytes_received":   len(response),
		"server_response_saved":   true,
		"server_response_file":    file.Name(),
		"authentication_verified": false,
	})
}

func minDeadline(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run . /absolute/path/to/private/probe-input.json")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
