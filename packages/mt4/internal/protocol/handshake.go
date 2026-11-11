package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
)

const ClientBuild uint16 = 1478

var InitialTable = []byte{65, 182, 127, 88, 56, 12, 240, 45, 123, 57, 8, 254, 33, 187, 65, 88}

func FeedbackEncode(input, key []byte) []byte {
	out := make([]byte, len(input))
	var previous byte
	for i, value := range input {
		out[i] = value ^ (key[i%len(key)] + previous)
		previous = out[i]
	}
	return out
}

func FeedbackDecode(input, key []byte) []byte {
	out := make([]byte, len(input))
	var previous byte
	for i, value := range input {
		out[i] = value ^ (key[i%len(key)] + previous)
		previous = value
	}
	return out
}

func PasswordDigest(password string) [16]byte {
	value := []byte(password)
	if len(value) > 16 {
		value = value[:16]
	}
	return md5.Sum(value)
}

func InitialPacket(login uint32, password string, mode uint8) []byte {
	digest := PasswordDigest(password)
	out := make([]byte, 28)
	if mode == 1 {
		out[0] = 11
	}
	if mode == 2 {
		out[27] = 1
	}
	out[1] = byte(uint32(ClientBuild) + uint32(out[0]) + login + uint32(out[27]))
	out[2] = 10
	for i, value := range digest {
		var previous byte
		if i != 0 {
			previous = digest[i-1]
		}
		out[3+i] = value ^ (value + previous)
	}
	binary.LittleEndian.PutUint32(out[19:23], login)
	binary.LittleEndian.PutUint16(out[23:25], 400)
	binary.LittleEndian.PutUint16(out[25:27], ClientBuild)
	copy(out[1:], FeedbackEncode(out[1:], InitialTable))
	return out
}

// StreamState operates on the outer stream. Its previous byte is the
// outer-stream plaintext, which may still contain an encrypted Record body.
type StreamState struct {
	Key      [16]byte
	Counter  uint32
	Previous byte
}

func (s *StreamState) Decode(value []byte) {
	for i, c := range value {
		p := c ^ (s.Key[s.Counter&15] + s.Previous)
		value[i] = p
		s.Previous = p
		s.Counter++
	}
}

func (s *StreamState) Encode(value []byte) {
	for i, p := range value {
		value[i] = p ^ (s.Key[s.Counter&15] + s.Previous)
		s.Previous = p
		s.Counter++
	}
}

func (s *StreamState) Read(reader io.Reader, count int) ([]byte, error) {
	if count < 0 || count > 32<<20 {
		return nil, errors.New("stream read exceeds experiment limit")
	}
	value := make([]byte, count)
	if _, err := io.ReadFull(reader, value); err != nil {
		return nil, err
	}
	s.Decode(value)
	return value, nil
}

type LegacyRandom uint32

func (r *LegacyRandom) Next() uint32 {
	*r = LegacyRandom(uint32(*r)*214013 + 2531011)
	return (uint32(*r) >> 16) & 0x7fff
}

// RawDigest16 uses SHA-1 compression with LE message words, zero padding,
// no length suffix, and LE state output. It is not crypto/sha1.Sum.
func RawDigest16(input []byte) [16]byte {
	state := [5]uint32{0x67452301, 0xefcdab89, 0x98badcfe, 0x10325476, 0xc3d2e1f0}
	count := max(1, (len(input)+63)/64)
	for block := 0; block < count; block++ {
		var buffer [64]byte
		if start := block * 64; start < len(input) {
			copy(buffer[:], input[start:min(start+64, len(input))])
		}
		var words [80]uint32
		for i := range 16 {
			words[i] = binary.LittleEndian.Uint32(buffer[i*4:])
		}
		for i := 16; i < 80; i++ {
			words[i] = bits.RotateLeft32(words[i-3]^words[i-8]^words[i-14]^words[i-16], 1)
		}
		a, b, c, d, e := state[0], state[1], state[2], state[3], state[4]
		for i, word := range words {
			var f, k uint32
			switch {
			case i < 20:
				f, k = (b&c)|(^b&d), 0x5a827999
			case i < 40:
				f, k = b^c^d, 0x6ed9eba1
			case i < 60:
				f, k = (b&c)|(b&d)|(c&d), 0x8f1bbcdc
			default:
				f, k = b^c^d, 0xca62c1d6
			}
			a, b, c, d, e = bits.RotateLeft32(a, 5)+f+e+k+word, a, bits.RotateLeft32(b, 30), c, d
		}
		for i, value := range [5]uint32{a, b, c, d, e} {
			state[i] += value
		}
	}
	var out [16]byte
	for i := range 4 {
		binary.LittleEndian.PutUint32(out[i*4:], state[i])
	}
	return out
}

func ChallengeKey(login uint32, digest [16]byte, field uint32, block [64]byte) ([16]byte, error) {
	key := digest
	if field == 0 {
		return key, fmt.Errorf("%w: zero-field challenge", ErrUnsupportedProtocol)
	}
	random := LegacyRandom(login + ((field >> 8) & 255) + 10)
	for range 4 {
		switch random.Next() & 7 {
		case 0:
			var table [16]byte
			for i := range table {
				table[i] = block[i*4]
			}
			copy(key[:], FeedbackEncode(key[:], table[:]))
		case 1:
			for i := 0; i < 16; i += 2 {
				key[i], key[i+1] = key[i+1], key[i]
			}
		case 2:
			value := append(append(append([]byte{}, key[:]...), block[:]...), []byte("MTRandom\x00")...)
			key = md5.Sum(value)
		case 3:
			value := append([]byte{}, key[:]...)
			value = binary.LittleEndian.AppendUint32(value, field)
			key = md5.Sum(append(value, []byte("MTSession\x00")...))
		case 4:
			for i := range key {
				key[i] = ^key[i]
			}
		case 5:
			for i := 0; i < 16; i += 2 {
				key[i] = ^key[i]
			}
		case 6:
			value := binary.LittleEndian.AppendUint16([]byte{10}, ClientBuild)
			value = append(value, block[:]...)
			value = append(value, key[:]...)
			value = binary.LittleEndian.AppendUint32(value, login)
			key = RawDigest16(append(value, []byte("MTComplex\x00")...))
		case 7:
			value := append([]byte("MTLogin\x00"), key[:]...)
			key = RawDigest16(binary.LittleEndian.AppendUint32(value, login))
		}
	}
	return key, nil
}

type Record struct {
	Kind, Mode uint16
	Data       []byte
}

func DecodePayload(input []byte, mode uint16, key [16]byte) ([]byte, error) {
	switch mode {
	case 0:
		return FeedbackDecode(input, key[:]), nil
	case 1:
		out := make([]byte, len(input))
		aligned := len(input) & ^15
		block, err := aes.NewCipher(key[:])
		if err != nil {
			return nil, err
		}
		if aligned > 0 {
			cipher.NewCBCDecrypter(block, make([]byte, 16)).CryptBlocks(out[:aligned], input[:aligned])
		}
		copy(out[aligned:], FeedbackDecode(input[aligned:], key[:]))
		return out, nil
	case 2:
		return append([]byte{}, input...), nil
	default:
		return nil, fmt.Errorf("%w: record mode", ErrUnsupportedProtocol)
	}
}

func PackRecord(value Record, random uint16, key [16]byte) ([]byte, error) {
	if len(value.Data) > 65535 || value.Mode > 2 {
		return nil, errors.New("invalid Record")
	}
	header := make([]byte, 8)
	binary.LittleEndian.PutUint16(header, uint16(len(value.Data)))
	binary.LittleEndian.PutUint16(header[2:], value.Kind)
	binary.LittleEndian.PutUint16(header[4:], value.Mode)
	binary.LittleEndian.PutUint16(header[6:], random)
	if value.Kind != 2 {
		header = FeedbackEncode(header, key[:])
	}
	var body []byte
	switch value.Mode {
	case 0:
		body = FeedbackEncode(value.Data, key[:])
	case 1:
		body = make([]byte, len(value.Data))
		aligned := len(value.Data) & ^15
		block, _ := aes.NewCipher(key[:])
		if aligned > 0 {
			cipher.NewCBCEncrypter(block, make([]byte, 16)).CryptBlocks(body[:aligned], value.Data[:aligned])
		}
		copy(body[aligned:], FeedbackEncode(value.Data[aligned:], key[:]))
	case 2:
		body = append([]byte{}, value.Data...)
	}
	return append(header, body...), nil
}

func UnpackInner(input []byte, key [16]byte) ([]Record, error) {
	var records []Record
	for len(input) > 0 {
		if len(input) < 8 {
			return nil, errors.New("truncated inner Record header")
		}
		header := FeedbackDecode(input[:8], key[:])
		size := int(binary.LittleEndian.Uint16(header))
		kind := binary.LittleEndian.Uint16(header[2:])
		mode := binary.LittleEndian.Uint16(header[4:])
		input = input[8:]
		if size > len(input) {
			return nil, errors.New("inner Record exceeds response boundary")
		}
		value, err := DecodePayload(input[:size], mode, key)
		if err != nil {
			return nil, err
		}
		records = append(records, Record{Kind: kind, Mode: mode, Data: value})
		input = input[size:]
	}
	return records, nil
}

type InitialResponse struct {
	ServerBuild uint16
	Field       uint32
	Key         [16]byte
	Records     []Record
	Rx, Tx      StreamState
}

func ReadInitial(reader io.Reader, login uint32, password string) (InitialResponse, error) {
	var result InitialResponse
	var status [1]byte
	if _, err := io.ReadFull(reader, status[:]); err != nil {
		return result, err
	}
	if status[0] != 0 {
		return result, &StatusError{Status: status[0]}
	}
	digest := PasswordDigest(password)
	result.Rx.Key = digest
	prefix, err := result.Rx.Read(reader, 7)
	if err != nil {
		return result, err
	}
	result.ServerBuild = binary.LittleEndian.Uint16(prefix[1:3])
	result.Field = binary.LittleEndian.Uint32(prefix[3:7])
	// The reference client's challenge branch is selected by signed build >1419.
	// Newer builds use the same bounded capability/layout checks; broker names
	// and individually enumerated build numbers do not select an implementation.
	if int16(result.ServerBuild) <= 1419 {
		return result, ErrUnsupportedBuild
	}
	if result.Field == 0 {
		return result, fmt.Errorf("%w: zero-field challenge", ErrUnsupportedProtocol)
	}
	challenge, err := result.Rx.Read(reader, 64)
	if err != nil {
		return result, err
	}
	var block [64]byte
	copy(block[:], challenge)
	result.Key, err = ChallengeKey(login, digest, result.Field, block)
	if err != nil {
		return result, err
	}
	header, err := result.Rx.Read(reader, 8)
	if err != nil {
		return result, err
	}
	length := int(binary.LittleEndian.Uint16(header))
	kind := binary.LittleEndian.Uint16(header[2:])
	mode := binary.LittleEndian.Uint16(header[4:])
	if length < 1 || length > 4095 || kind != 2 {
		return result, errors.New("unexpected initial Record boundary")
	}
	if mode > 2 {
		return result, fmt.Errorf("%w: initial record mode", ErrUnsupportedProtocol)
	}
	body, err := result.Rx.Read(reader, length)
	if err != nil {
		return result, err
	}
	body, err = DecodePayload(body, mode, result.Key)
	if err != nil {
		return result, err
	}
	result.Records, err = UnpackInner(body, result.Key)
	if err != nil {
		return result, err
	}
	// The key changes, but the outer receive counters are retained.
	result.Rx.Key = result.Key
	result.Tx.Key = result.Key
	return result, nil
}
