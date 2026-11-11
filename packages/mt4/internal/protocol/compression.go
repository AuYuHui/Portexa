package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"

	"github.com/anchore/go-lzo"
)

func DecodeLZO(input []byte, expected, limit int) ([]byte, error) {
	if expected < 1 || expected > limit || len(input) < 1 || len(input) > 2*limit {
		return nil, errors.New("compressed block exceeds experiment limits")
	}
	output := make([]byte, expected)
	count, err := lzo.Decompress(input, output)
	if err != nil || count != expected {
		return nil, errors.New("LZO output does not match the declared size")
	}
	return output, nil
}

func ReadCompressed(reader io.Reader, state *StreamState, limit int) ([]byte, error) {
	value, err := state.Read(reader, 4)
	if err != nil {
		return nil, err
	}
	expected := int(binary.LittleEndian.Uint32(value))
	if expected == 0 {
		return nil, nil
	}
	if expected > limit {
		return nil, errors.New("declared output exceeds experiment limit")
	}
	value, err = state.Read(reader, 4)
	if err != nil {
		return nil, err
	}
	count := int(binary.LittleEndian.Uint32(value))
	if count < 1 || count > 2*limit {
		return nil, errors.New("declared compressed input exceeds experiment limit")
	}
	value, err = state.Read(reader, count)
	if err != nil {
		return nil, err
	}
	return DecodeLZO(value, expected, limit)
}

type ServerConfiguration struct {
	Name        string
	AccessCount int
	Data        []byte
}

func DecodeUTF16(value []byte) string {
	var words []uint16
	for len(value) >= 2 {
		word := binary.LittleEndian.Uint16(value)
		if word == 0 {
			break
		}
		words = append(words, word)
		value = value[2:]
	}
	return string(utf16.Decode(words))
}

func ValidateServerConfiguration(value []byte, expectedServer string) (ServerConfiguration, error) {
	var result ServerConfiguration
	if len(value) < 196 || (len(value)-196)%160 != 0 {
		return result, errors.New("server configuration structure is invalid")
	}
	result.AccessCount = (len(value) - 196) / 160
	nameOffset := 4 + result.AccessCount*160
	name := value[nameOffset : nameOffset+64]
	end := bytes.IndexByte(name, 0)
	if end < 0 {
		return result, errors.New("server configuration name is not terminated")
	}
	result.Name = string(name[:end])
	expectedPrefix := strings.SplitN(expectedServer, "-", 2)[0]
	actualPrefix := strings.SplitN(result.Name, "-", 2)[0]
	if expectedPrefix == "" || actualPrefix == "" || !strings.EqualFold(expectedPrefix, actualPrefix) {
		return result, errors.New("returned configuration does not match the requested broker")
	}
	result.Data = value
	return result, nil
}

func ReadServerConfiguration(reader io.Reader, state *StreamState, expectedServer string) (ServerConfiguration, error) {
	var result ServerConfiguration
	status, err := state.Read(reader, 1)
	if err != nil {
		return result, err
	}
	if status[0] != 0 {
		return result, fmt.Errorf("server configuration status %d", status[0])
	}
	if _, err := state.Read(reader, 1); err != nil {
		return result, err
	}
	value, err := ReadCompressed(reader, state, 65535)
	if err != nil {
		return result, err
	}
	return ValidateServerConfiguration(value, expectedServer)
}
