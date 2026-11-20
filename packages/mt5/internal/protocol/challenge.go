package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var (
	challengeComputeMu   sync.Mutex
	challengeOverrideFunc func(challenge []byte) (uint64, error)
)

// SetChallengeOverride allows tests or mock environments to inject expected results.
func SetChallengeOverride(fn func(challenge []byte) (uint64, error)) {
	challengeComputeMu.Lock()
	defer challengeComputeMu.Unlock()
	challengeOverrideFunc = fn
}

// ComputeChallenge executes the ARM64 arithmetic worker for Extension 28.
func ComputeChallenge(challenge []byte) (uint64, error) {
	challengeComputeMu.Lock()
	override := challengeOverrideFunc
	challengeComputeMu.Unlock()
	if override != nil {
		return override(challenge)
	}

	if len(challenge) < 24 || len(challenge) > 4096 || len(challenge)%8 != 0 {
		return 0, errors.New("unsupported server challenge size")
	}

	// Look for local compute binary
	binaryPath := findComputeWorker()
	if binaryPath == "" {
		return 0, errors.New("mt5: challenge computation worker not found")
	}

	imagePath := os.Getenv("MT5_TERMINAL_IMAGE")
	if imagePath == "" {
		imagePath = "/Applications/MetaTrader5Terminal.app/Wrapper/MetaTrader5Terminal.app/MetaTrader5Terminal"
	}
	if _, err := os.Stat(imagePath); err != nil {
		return 0, fmt.Errorf("mt5: terminal image missing for challenge: %w", err)
	}

	cmd := exec.Command(binaryPath, imagePath)
	cmd.Stdin = bytes.NewReader(prepareWorkerInput(challenge))
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("mt5: challenge worker failed: %w", err)
	}
	if len(out) < 12 {
		return 0, errors.New("mt5: short challenge worker output")
	}
	ready := binary.LittleEndian.Uint32(out[:4])
	if ready != 1 {
		return 0, fmt.Errorf("mt5: unexpected worker ready signal: %d", ready)
	}
	return binary.LittleEndian.Uint64(out[4:12]), nil
}

func prepareWorkerInput(challenge []byte) []byte {
	buf := make([]byte, 4+len(challenge))
	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(challenge)/8))
	copy(buf[4:], challenge)
	return buf
}

func findComputeWorker() string {
	if env := os.Getenv("MT5_COMPUTE_WORKER"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
	}
	cwd, err := os.Getwd()
	if err == nil {
		p := filepath.Join(cwd, ".local", "mt5-sync-timing", "compute")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		p = filepath.Join(cwd, "..", "..", ".local", "mt5-sync-timing", "compute")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	candidate := "/Users/dabu/Desktop/work/Portexa/.local/mt5-sync-timing/compute"
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
