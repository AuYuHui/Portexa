// Package support provides a loopback-only synthetic MT4 peer for tests.
// It contains no captured broker traffic or real credentials.
package support

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"testing"
	"time"

	"portexa/mt4/internal/protocol"
)

const (
	Login    = 123456
	Password = "fixture-only!"
	Server   = "FixtureBroker-Live1"
)

// Independent Python-generated synthetic wire; also covered by protocol golden tests.
const initialHex = "004714291ffa26eb14561c277f01d2ab76e382e7e5d57f4602b6fc275f01f2ab16a382e7e5355f4602d61ce7bf01d26b36a382a7a555bf4602f63ce7df01326b166382a7a575dfc6e179013049098ffc90be7c7f0f8e50b9bbd9dd880a6afda7142a1962c9384015d2e40c0c26ec8ec2"

type Options struct {
	Authenticate  bool
	History       bool
	Build         uint16
	InitialStatus byte
	HistoryPages  []HistoryPage
	HistoryBounds [][2]uint32
	AccountTail   []byte
}

type HistoryPage struct {
	Status       byte
	Records      []byte
	Disconnect   bool
	WaitForClose bool
	DeclaredSize uint32
}

// NewServer returns an ephemeral address and a channel reporting the peer result.
func NewServer(t testing.TB, options Options) (string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	completed := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			completed <- err
			return
		}
		defer conn.Close()
		defer listener.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		completed <- serve(conn, options)
	}()
	return listener.Addr().String(), completed
}

func serve(conn net.Conn, options Options) error {
	initial := make([]byte, 28)
	if _, err := io.ReadFull(conn, initial); err != nil {
		return err
	}
	want, _ := hex.DecodeString("0047f776a8793785fe3db9c69ba360f96993db1a90c9d5558338747c")
	if !bytes.Equal(initial, want) {
		return errors.New("initial request differs from independent golden vector")
	}
	response, _ := hex.DecodeString(initialHex)
	if options.InitialStatus != 0 {
		if _, err := conn.Write([]byte{options.InitialStatus}); err != nil {
			return err
		}
		return expectClose(conn)
	}
	if options.Build != 0 {
		state := protocol.StreamState{Key: protocol.PasswordDigest(Password)}
		state.Decode(response[1:])
		binary.LittleEndian.PutUint16(response[2:], options.Build)
		state = protocol.StreamState{Key: protocol.PasswordDigest(Password)}
		state.Encode(response[1:])
		if int16(options.Build) <= 1419 {
			response = response[:8]
		}
	}
	if _, err := conn.Write(response); err != nil {
		return err
	}
	if !options.Authenticate {
		return expectClose(conn)
	}
	state, err := protocol.ReadInitial(bytes.NewReader(response), Login, Password)
	if err != nil {
		return err
	}
	var authHeader [9]byte
	if _, err := io.ReadFull(conn, authHeader[:]); err != nil {
		return err
	}
	if authHeader[0] != 0x18 || binary.LittleEndian.Uint16(authHeader[3:]) != 2 || binary.LittleEndian.Uint16(authHeader[5:]) != 1 {
		return errors.New("invalid authentication envelope")
	}
	length := int(binary.LittleEndian.Uint16(authHeader[1:]))
	if length < 8 || length > 4096 {
		return errors.New("invalid authentication body length")
	}
	if _, err := io.CopyN(io.Discard, conn, int64(length)); err != nil {
		return err
	}
	read := func(count int) ([]byte, error) { return state.Tx.Read(conn, count) }
	write := func(value []byte) error {
		state.Rx.Encode(value)
		_, err := io.Copy(conn, bytes.NewReader(value))
		return err
	}
	if request, err := read(1); err != nil || request[0] != 0x10 {
		return fmt.Errorf("expected configuration request: %w", err)
	}
	configuration := make([]byte, 196)
	copy(configuration[4:], Server)
	if err := write(append([]byte{0, 0}, compressed(configuration)...)); err != nil {
		return err
	}
	for _, command := range []byte{0x08, 0x0a} {
		request, err := read(17)
		if err != nil || request[0] != command || !bytes.Equal(request[1:], make([]byte, 16)) {
			return fmt.Errorf("invalid table request 0x%02x: %w", command, err)
		}
		reply := []byte{1}
		if command == 0x08 {
			symbol := make([]byte, 1936)
			copy(symbol, "TEST.USD")
			binary.LittleEndian.PutUint64(symbol[0x6a8:], math.Float64bits(100000))
			binary.LittleEndian.PutUint64(symbol[0x6e8:], math.Float64bits(1))
			reply = append([]byte{0}, compressed(symbol)...)
		}
		if err := write(reply); err != nil {
			return err
		}
	}
	if request, err := read(1); err != nil || request[0] != 0x21 {
		return fmt.Errorf("expected optional table request: %w", err)
	}
	if err := write([]byte{1}); err != nil {
		return err
	}
	if options.History {
		pages := options.HistoryPages
		if pages == nil {
			pages = []HistoryPage{{Records: tradeRecord(true)}}
		}
		for index, page := range pages {
			request, err := read(9)
			if err != nil || request[0] != 0x22 || binary.LittleEndian.Uint32(request[1:]) >= binary.LittleEndian.Uint32(request[5:]) {
				return fmt.Errorf("invalid history interval: %w", err)
			}
			if index < len(options.HistoryBounds) && (binary.LittleEndian.Uint32(request[1:]) != options.HistoryBounds[index][0] || binary.LittleEndian.Uint32(request[5:]) != options.HistoryBounds[index][1]) {
				return errors.New("incorrect history window boundary")
			}
			if page.Disconnect {
				return nil
			}
			if page.WaitForClose {
				_, err := io.Copy(io.Discard, conn)
				return err
			}
			var reply []byte
			if page.Status != 0 {
				reply = []byte{page.Status}
			} else if page.DeclaredSize != 0 {
				reply = binary.LittleEndian.AppendUint32([]byte{0}, page.DeclaredSize)
			} else if len(page.Records) == 0 {
				reply = []byte{0, 0, 0, 0, 0}
			} else {
				reply = append([]byte{0}, compressed(page.Records)...)
			}
			if err := write(reply); err != nil {
				return err
			}
			if page.Status > 1 || page.DeclaredSize != 0 {
				return expectClose(conn)
			}
		}
	}
	request, err := read(77)
	if err != nil || request[0] != 0x1e || binary.LittleEndian.Uint32(request[1:]) != 2 || binary.LittleEndian.Uint32(request[13:]) != 2 || !bytes.Equal(request[17:], make([]byte, 60)) {
		return fmt.Errorf("invalid account request prefix: %w", err)
	}
	headers, err := read(16)
	if err != nil {
		return err
	}
	size := int(binary.LittleEndian.Uint16(headers[8:]))
	if size < 2 || size > 8192 || binary.LittleEndian.Uint16(headers[:]) != uint16(size+8) || binary.LittleEndian.Uint16(headers[2:]) != 2 || binary.LittleEndian.Uint16(headers[10:]) != 13 {
		return errors.New("invalid account metadata record headers")
	}
	metadata, err := read(size)
	if err != nil || !bytes.Equal(metadata[size-2:], []byte{0, 0}) {
		return fmt.Errorf("invalid account metadata body: %w", err)
	}
	account := make([]byte, 0x3600)
	binary.LittleEndian.PutUint32(account[0x44:], 100)
	binary.LittleEndian.PutUint64(account[0x48:], math.Float64bits(1234.5))
	binary.LittleEndian.PutUint64(account[0x50:], math.Float64bits(6.25))
	binary.LittleEndian.PutUint32(account[0x35d8:], 1)
	copy(account[0x34f4:], "USD")
	account = append(account, tradeRecord(false)...)
	account = append(account, options.AccountTail...)
	if err := write(append([]byte{0}, compressed(account)...)); err != nil {
		return err
	}
	return expectClose(conn)
}

// TradeBytes creates a complete synthetic history row for interval/replay tests.
func TradeBytes(ticket int32, closeTime uint32, profit float64) []byte {
	value := tradeRecord(true)
	binary.LittleEndian.PutUint32(value, uint32(ticket))
	binary.LittleEndian.PutUint32(value[0x40:], closeTime)
	binary.LittleEndian.PutUint64(value[0x80:], math.Float64bits(profit))
	return value
}

func expectClose(conn net.Conn) error {
	var extra [1]byte
	count, err := conn.Read(extra[:])
	if count != 0 || !errors.Is(err, io.EOF) {
		return errors.New("client did not close after its snapshot")
	}
	return nil
}

// Literal-only raw LZO1X block, encoded independently from the production decoder.
func compressed(data []byte) []byte {
	var block []byte
	if len(data) < 239 {
		block = append(block, byte(len(data)+17))
	} else {
		block = append(block, 0)
		length := len(data) - 18
		for length > 255 {
			block = append(block, 0)
			length -= 255
		}
		block = append(block, byte(length))
	}
	block = append(block, data...)
	block = append(block, 17, 0, 0)
	result := binary.LittleEndian.AppendUint32(nil, uint32(len(data)))
	result = binary.LittleEndian.AppendUint32(result, uint32(len(block)))
	return append(result, block...)
}

func tradeRecord(history bool) []byte {
	value := make([]byte, 224)
	binary.LittleEndian.PutUint32(value, 42)
	copy(value[8:], "TEST.USD")
	binary.LittleEndian.PutUint32(value[0x14:], 5)
	binary.LittleEndian.PutUint32(value[0x1c:], 125)
	binary.LittleEndian.PutUint32(value[0x20:], 1700000000)
	if history {
		binary.LittleEndian.PutUint32(value[0x18:], 1)
		binary.LittleEndian.PutUint32(value[0x40:], 1700003000)
	}
	for offset, money := range map[int]float64{0x28: 1.15, 0x30: 1.125, 0x38: 1.25, 0x60: -1.5, 0x70: -0.25, 0x78: 1.16, 0x80: 12.5, 0xc0: 1} {
		binary.LittleEndian.PutUint64(value[offset:], math.Float64bits(money))
	}
	return value
}
