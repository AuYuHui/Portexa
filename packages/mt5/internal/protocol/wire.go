package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	HeaderSize = 9

	FlagCompressed = 1
	FlagFinal      = 2

	MaxFrameSize = 16 * 1024 * 1024
)

var (
	ErrPeerClosed       = errors.New("mt5: peer closed connection")
	ErrFrameTooLarge    = errors.New("mt5: frame exceeds size limit")
	ErrTruncatedFrame   = errors.New("mt5: truncated wire frame")
	ErrProtocolMismatch = errors.New("mt5: unexpected message type or sequence")
	ErrAuthFailed       = errors.New("mt5: server proof authentication failed")
)

type FrameHeader struct {
	Type     uint8
	Length   uint32
	Sequence uint16
	Flags    uint16
}

func ReadHeader(r io.Reader) (FrameHeader, error) {
	var buf [HeaderSize]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return FrameHeader{}, ErrPeerClosed
		}
		return FrameHeader{}, err
	}
	return FrameHeader{
		Type:     buf[0],
		Length:   binary.LittleEndian.Uint32(buf[1:5]),
		Sequence: binary.LittleEndian.Uint16(buf[5:7]),
		Flags:    binary.LittleEndian.Uint16(buf[7:9]),
	}, nil
}

func PackHeader(h FrameHeader) [HeaderSize]byte {
	var buf [HeaderSize]byte
	buf[0] = h.Type
	binary.LittleEndian.PutUint32(buf[1:5], h.Length)
	binary.LittleEndian.PutUint16(buf[5:7], h.Sequence)
	binary.LittleEndian.PutUint16(buf[7:9], h.Flags)
	return buf
}

func WriteFrame(w io.Writer, h FrameHeader, payload []byte) error {
	if uint32(len(payload)) != h.Length {
		return fmt.Errorf("payload size %d does not match header length %d", len(payload), h.Length)
	}
	hdr := PackHeader(h)
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func ReadFramePayload(r io.Reader, length uint32) ([]byte, error) {
	if length > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrPeerClosed
		}
		return nil, err
	}
	return payload, nil
}
