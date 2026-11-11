package protocol

import (
	"errors"
	"io"
	"sort"
)

type HistoryInterval struct {
	Start, End uint32
}

// A failed response never commits a window. Successful (including empty) windows are
// exposed separately so a caller can resume with an overlapping request safely.
func synchronizeHistory(conn io.ReadWriter, response *InitialResponse, start, end, window uint32) (TableResult, []HistoryInterval, error) {
	var covered []HistoryInterval
	byTicket := make(map[int32]TradeInfo)
	status := byte(0)
	snapshot := func() TableResult {
		records := make([]TradeInfo, 0, len(byTicket))
		for _, record := range byTicket {
			records = append(records, record)
		}
		sort.Slice(records, func(i, j int) bool { return records[i].Ticket < records[j].Ticket })
		return TableResult{Status: status, Count: len(records), Records: records}
	}
	for next := start; next < end; {
		stop := end
		if window != 0 && window < end-next {
			stop = next + window
		}
		request, err := HistoryRequest(next, stop)
		if err == nil {
			err = WriteCommand(conn, &response.Tx, request)
		}
		if err != nil {
			return snapshot(), covered, err
		}
		page, err := ReadHistory(conn, &response.Rx)
		status = page.Status
		if err != nil {
			return snapshot(), covered, err
		}
		// Validate a whole page before committing. Conflicting rows inside one
		// response cannot safely be interpreted as an update sequence.
		incoming := make(map[int32]TradeInfo, len(page.Records))
		newTickets := 0
		for _, record := range page.Records {
			previous, duplicate := incoming[record.Ticket]
			if record.Ticket <= 0 || (duplicate && previous != record) {
				return snapshot(), covered, errors.New("history has an invalid or conflicting ticket")
			}
			if _, exists := byTicket[record.Ticket]; !exists && !duplicate {
				newTickets++
			}
			incoming[record.Ticket] = record
		}
		if len(byTicket)+newTickets > SyncLimit/TradeRecordSize {
			return snapshot(), covered, ErrResourceLimit
		}
		for ticket, record := range incoming {
			// Later windows can contain a corrected version of a boundary row.
			byTicket[ticket] = record
		}
		covered = append(covered, HistoryInterval{Start: next, End: stop})
		if stop == end {
			break
		}
		// Overlap one second instead of assuming the server's end inclusivity.
		next = stop - 1
	}
	return snapshot(), covered, nil
}
