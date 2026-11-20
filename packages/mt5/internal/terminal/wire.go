package terminal

// Wire layout derived from metatrader5-go v0.2.0 (MIT).

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"

	"github.com/gorilla/websocket"
)

var le = binary.LittleEndian

// The Web Terminal bootstrap key is public. TLS authenticates the endpoint.
func initialKey() []byte {
	const key = "02de02a1a65cc794684fcbea1ecb0fd74ae657e43662c11eee885d2fd64f4964"
	data, err := hex.DecodeString(key)
	if err != nil {
		panic(err)
	}
	return data
}

type session struct {
	conn *websocket.Conn
	key  []byte
}

func (s session) request(command uint16, payload []byte) ([]byte, error) {
	plain := make([]byte, 4+len(payload))
	_, _ = rand.Read(plain[:2])
	le.PutUint16(plain[2:], command)
	copy(plain[4:], payload)
	frame := encodeFrame(s.key, plain)
	clear(plain)
	if err := s.conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return nil, err
	}
	for {
		kind, response, err := s.conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		if kind != websocket.BinaryMessage {
			return nil, ErrProtocol
		}
		data, err := decodeFrame(s.key, response)
		if err != nil {
			return nil, err
		}
		// Unsolicited notifications are not a reply to this command.
		if le.Uint16(data[2:4]) != command {
			continue
		}
		if data[4] != 0 {
			return nil, ErrRejected
		}
		return data[5:], nil
	}
}

func encodeFrame(key, plain []byte) []byte {
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	n := len(plain) + padding
	frame := make([]byte, 8+n)
	le.PutUint32(frame, uint32(n))
	le.PutUint32(frame[4:], 1)
	body := frame[8:]
	copy(body, plain)
	for i := len(plain); i < n; i++ {
		body[i] = byte(padding)
	}
	block, _ := aes.NewCipher(key)
	var iv [aes.BlockSize]byte
	cipher.NewCBCEncrypter(block, iv[:]).CryptBlocks(body, body)
	return frame
}

func decodeFrame(key, frame []byte) ([]byte, error) {
	if len(frame) < 8+aes.BlockSize || len(frame[8:])%aes.BlockSize != 0 ||
		int(le.Uint32(frame)) != len(frame[8:]) {
		return nil, ErrProtocol
	}
	block, _ := aes.NewCipher(key)
	var iv [aes.BlockSize]byte
	body := frame[8:]
	cipher.NewCBCDecrypter(block, iv[:]).CryptBlocks(body, body)
	padding := int(body[len(body)-1])
	if padding < 1 || padding > aes.BlockSize || len(body)-padding < 5 {
		return nil, ErrProtocol
	}
	for _, b := range body[len(body)-padding:] {
		if int(b) != padding {
			return nil, ErrProtocol
		}
	}
	return body[:len(body)-padding], nil
}
