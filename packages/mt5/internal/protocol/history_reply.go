package protocol

import (
	"encoding/binary"
	"io"
	"time"
)

// A broker can send complete month groups even for a short requested interval.
// Keep the wire-frame limit and bound the full expanded reply independently.
const maxHistoryReplySize = 128 * 1024 * 1024

type historyReplyReader struct {
	conn     io.Reader
	state    *RollingPayload
	sequence uint16
	chunk    []byte
	received int
	final    bool
}

func (r *historyReplyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.chunk) == 0 && !r.final {
		if err := r.readFrame(); err != nil {
			return 0, err
		}
	}
	if len(r.chunk) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunk)
	r.chunk = r.chunk[n:]
	return n, nil
}

func (r *historyReplyReader) readFrame() error {
	if conn, ok := r.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		if err := conn.SetReadDeadline(time.Now().Add(4 * time.Second)); err != nil {
			return err
		}
	}
	header, err := ReadHeader(r.conn)
	if err != nil {
		return err
	}
	raw, err := ReadFramePayload(r.conn, header.Length)
	if err != nil {
		return err
	}
	compressed := header.Flags&FlagCompressed != 0
	chunk := r.state.Decode(raw, compressed)
	// Notifications advance the rolling state without contributing reply bytes.
	if header.Type != TypeHistory || header.Sequence != r.sequence {
		return nil
	}
	if compressed {
		chunk, err = DecompressLZO(chunk)
		if err != nil {
			return err
		}
	}
	if len(chunk) > maxHistoryReplySize-r.received {
		return ErrFrameTooLarge
	}
	r.received += len(chunk)
	r.chunk = chunk
	r.final = header.Flags&FlagFinal != 0
	return nil
}

func parseHistoryReply(reply io.Reader, login uint64, start, end uint32) ([]DealRecord, error) {
	var header [9]byte
	if _, err := io.ReadFull(reply, header[:]); err != nil {
		return nil, err
	}
	if header[0] != dealHistoryCommand {
		return nil, ErrProtocolMismatch
	}
	var group [12]byte
	groups := binary.LittleEndian.Uint32(header[5:])
	if groups > uint32(maxHistoryReplySize/(len(group)+4)) {
		return nil, ErrProtocolMismatch
	}
	var deals []DealRecord
	seen := make(map[uint64]struct{})
	var row [DealRecordSize]byte
	for range groups {
		if _, err := io.ReadFull(reply, group[:]); err != nil {
			return nil, err
		}
		var manifest map[uint64]struct{}
		switch binary.LittleEndian.Uint32(group[8:]) {
		case 0:
			var err error
			manifest, err = readHistoryManifest(reply)
			if err != nil {
				return nil, err
			}
		case 2, 3, 4:
			// Full records without a ticket manifest.
		default:
			return nil, ErrProtocolMismatch
		}
		count, err := readHistoryCount(reply)
		if err != nil {
			return nil, err
		}
		if count > maxHistoryReplySize/DealRecordSize {
			return nil, ErrFrameTooLarge
		}
		if manifest != nil && len(manifest) != int(count) {
			return nil, ErrProtocolMismatch
		}
		for range count {
			if _, err := io.ReadFull(reply, row[:]); err != nil {
				return nil, err
			}
			deal, err := ParseDealRecord(row[:], login)
			if err != nil {
				return nil, err
			}
			if manifest != nil {
				if _, present := manifest[deal.Ticket]; !present {
					return nil, ErrProtocolMismatch
				}
				delete(manifest, deal.Ticket)
			}
			if deal.Time < int64(start)*1000 || deal.Time >= int64(end)*1000 {
				continue
			}
			if _, duplicate := seen[deal.Ticket]; duplicate {
				return nil, ErrProtocolMismatch
			}
			seen[deal.Ticket] = struct{}{}
			deals = append(deals, deal)
		}
		if manifest != nil {
			var footer [16]byte
			if _, err := io.ReadFull(reply, footer[:]); err != nil {
				return nil, err
			}
		}
	}
	// Verify the exact message boundary before reporting any deals as complete.
	if _, err := io.ReadFull(reply, row[:1]); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, ErrProtocolMismatch
	}
	return deals, nil
}

func readHistoryManifest(reply io.Reader) (map[uint64]struct{}, error) {
	count, err := readHistoryCount(reply)
	if err != nil {
		return nil, err
	}
	if count > maxHistoryReplySize/(8+DealRecordSize) {
		return nil, ErrFrameTooLarge
	}
	tickets := make(map[uint64]struct{})
	var raw [8]byte
	for range count {
		if _, err := io.ReadFull(reply, raw[:]); err != nil {
			return nil, err
		}
		ticket := binary.LittleEndian.Uint64(raw[:])
		if _, duplicate := tickets[ticket]; duplicate {
			return nil, ErrProtocolMismatch
		}
		tickets[ticket] = struct{}{}
	}
	return tickets, nil
}

func readHistoryCount(reply io.Reader) (uint32, error) {
	var count [4]byte
	_, err := io.ReadFull(reply, count[:])
	return binary.LittleEndian.Uint32(count[:]), err
}
