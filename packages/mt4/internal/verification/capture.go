package verification

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"portexa/mt4/internal/protocol"
	"strings"
)

// protocol.CapturedDeviceInfo is used only for private offline reproduction. It does
// not expose the installation identifier through stdout or public fixtures.
func CapturedDeviceInfo(packet []byte, login uint32, response protocol.InitialResponse) ([]byte, int, error) {
	if len(packet) < 9 || packet[0] != 0x18 {
		return nil, 0, errors.New("captured authentication envelope is missing")
	}
	length := int(binary.LittleEndian.Uint16(packet[1:]))
	if length > len(packet)-9 || binary.LittleEndian.Uint16(packet[3:]) != 2 || binary.LittleEndian.Uint16(packet[5:]) != 1 {
		return nil, 0, errors.New("captured authentication envelope is invalid")
	}
	key, _ := protocol.AuthOuterKey(login, response.Field, response.Key)
	body, err := protocol.DecodePayload(packet[9:9+length], 1, key)
	if err != nil {
		return nil, 0, err
	}
	working := key
	var device []byte
	for len(body) > 0 {
		if len(body) < 8 {
			return nil, 0, errors.New("captured subrecord is truncated")
		}
		header := protocol.FeedbackDecode(body[:8], working[:])
		size := int(binary.LittleEndian.Uint16(header))
		kind := binary.LittleEndian.Uint16(header[2:])
		mode := binary.LittleEndian.Uint16(header[4:])
		body = body[8:]
		if size > len(body) {
			return nil, 0, errors.New("captured subrecord length is invalid")
		}
		data, err := protocol.DecodePayload(body[:size], mode, working)
		if err != nil {
			return nil, 0, err
		}
		if kind == 0 {
			device = data
		}
		if kind == 3 {
			if len(data) == 0 {
				return nil, 0, errors.New("empty key-changing subrecord")
			}
			copy(working[:], protocol.FeedbackEncode(working[:], data))
		}
		body = body[size:]
	}
	if len(device) != 68 {
		return nil, 0, errors.New("captured device profile is absent")
	}
	return device, 9 + length, nil
}

// protocol.ReadCapturedAccount consumes the captured request sequence to determine
// response boundaries. Captured cache digests/metadata stay private; they are
// never reused for live requests.
func ReadCapturedAccount(client, server []byte, response *protocol.InitialResponse, expectedServer string) (protocol.SessionResult, error) {
	var result protocol.SessionResult
	response.Tx.Decode(client)
	tx := bytes.NewReader(client)
	rx := bytes.NewReader(server)
	for tx.Len() > 0 {
		command, _ := tx.ReadByte()
		switch command {
		case 0x10:
			var err error
			result.Configuration, err = protocol.ReadServerConfiguration(rx, &response.Rx, expectedServer)
			if err != nil {
				return result, err
			}
		case 0x08, 0x0a:
			if tx.Len() < 16 {
				return result, io.ErrUnexpectedEOF
			}
			tx.Seek(16, io.SeekCurrent)
			stride := 1936
			if command == 0x0a {
				stride = 80
			}
			table, err := protocol.ReadTable(rx, &response.Rx, stride)
			if err != nil {
				return result, err
			}
			if command == 0x08 {
				result.Symbols = table
			} else {
				result.Groups = table
			}
		case 0x21:
			var err error
			result.Auxiliary, err = protocol.ReadAuxiliary(rx, &response.Rx)
			if err != nil {
				return result, err
			}
		case 0x22:
			var interval [8]byte
			if _, err := io.ReadFull(tx, interval[:]); err != nil {
				return result, err
			}
			if _, err := protocol.HistoryRequest(binary.LittleEndian.Uint32(interval[:4]), binary.LittleEndian.Uint32(interval[4:])); err != nil {
				return result, err
			}
			var err error
			result.History, err = protocol.ReadHistory(rx, &response.Rx)
			if err != nil {
				return result, err
			}
			result.HistoryRead = true
		case 0x1e:
			var prefix [76]byte // flags4 and prefix72.
			if _, err := io.ReadFull(tx, prefix[:]); err != nil {
				return result, err
			}
			if binary.LittleEndian.Uint32(prefix[12:]) != 2 || !bytes.Equal(prefix[16:], make([]byte, 60)) {
				return result, errors.New("captured account request prefix is invalid")
			}
			if response.ServerBuild >= 1362 {
				var headers [16]byte
				if _, err := io.ReadFull(tx, headers[:]); err != nil {
					return result, err
				}
				size := int(binary.LittleEndian.Uint16(headers[8:]))
				if binary.LittleEndian.Uint16(headers[:]) != uint16(size+8) || binary.LittleEndian.Uint16(headers[2:]) != 2 || binary.LittleEndian.Uint16(headers[10:]) != 13 || size < 2 || size%2 != 0 || size > 8192 || size > tx.Len() {
					return result, errors.New("captured account metadata boundary is invalid")
				}
				metadata := make([]byte, size)
				io.ReadFull(tx, metadata)
				if !bytes.Equal(metadata[size-2:], []byte{0, 0}) || !strings.Contains(protocol.DecodeUTF16(metadata), "package=") {
					return result, errors.New("captured account metadata schema is invalid")
				}
			}
			var err error
			result.Account, err = protocol.ReadAccount(rx, &response.Rx)
			result.AccountRead = err == nil
			return result, err
		default:
			return result, fmt.Errorf("unexpected captured initialization command 0x%02x", command)
		}
	}
	return result, errors.New("capture contains no complete account exchange")
}
