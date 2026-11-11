// Package verification handles private captured vectors only. The runtime SDK
// does not import it, and it never opens a network connection.
package verification

import (
	"bytes"
	"errors"

	"portexa/mt4/internal/protocol"
)

type Credentials struct {
	Login    uint32
	Password string
	Server   string
}

type Vector struct {
	ClientInitial []byte `json:"client_initial"`
	ServerInitial []byte `json:"server_initial"`
	ClientFull    []byte `json:"client_full"`
	ServerFull    []byte `json:"server_full"`
}

// Report deliberately excludes credentials, keys, IDs, values and payloads.
type Report struct {
	Mode                  string `json:"mode"`
	InitialMatches        bool   `json:"initial_packet_matches_capture"`
	AuthenticationMatches bool   `json:"authentication_packet_matches_capture"`
	ServerBuild           uint16 `json:"server_build"`
	BrokerVerified        bool   `json:"broker_name_verified"`
	ConfigBytes           int    `json:"server_config_bytes"`
	AccessPoints          int    `json:"server_config_access_points"`
	AccountDecoded        bool   `json:"account_response_decoded"`
	AccountBaseBytes      int    `json:"account_base_bytes"`
	InvestorIndicator     bool   `json:"investor_indicator_set"`
	SymbolRecords         int    `json:"symbol_records"`
	GroupRecords          int    `json:"group_records"`
	HistoryDecoded        bool   `json:"history_response_decoded"`
	HistoryRecords        int    `json:"history_records"`
	TradeRecords          int    `json:"trade_order_records"`
	TradeTrailingBytes    int    `json:"trade_order_trailing_bytes"`
}

func Verify(config Credentials, vector Vector) (Report, error) {
	var result Report
	packet := protocol.InitialPacket(config.Login, config.Password, 0)
	if !bytes.Equal(packet, vector.ClientInitial) || !bytes.HasPrefix(vector.ClientFull, packet) || !bytes.HasPrefix(vector.ServerFull, vector.ServerInitial) {
		return result, errors.New("captured initial exchange or stream prefix does not match")
	}
	response, err := protocol.ReadInitial(bytes.NewReader(vector.ServerInitial), config.Login, config.Password)
	if err != nil {
		return result, err
	}
	device, count, err := CapturedDeviceInfo(vector.ClientFull[len(packet):], config.Login, response)
	if err != nil {
		return result, err
	}
	auth, _, err := protocol.AuthenticationPacket(response, config.Login, device)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(auth, vector.ClientFull[len(packet):len(packet)+count]) {
		return result, errors.New("generated authentication packet does not match the private vector")
	}
	session, err := ReadCapturedAccount(bytes.Clone(vector.ClientFull[len(packet)+count:]), vector.ServerFull[len(vector.ServerInitial):], &response, config.Server)
	if err != nil {
		return result, err
	}
	return Report{
		Mode: "offline", InitialMatches: true, AuthenticationMatches: true,
		ServerBuild: response.ServerBuild, BrokerVerified: session.Configuration.Name != "",
		ConfigBytes: len(session.Configuration.Data), AccessPoints: session.Configuration.AccessCount,
		AccountDecoded: session.AccountRead, AccountBaseBytes: min(protocol.AccountBaseSize, len(session.Account.Raw)),
		InvestorIndicator: session.Account.InvestorIndicator != 0,
		SymbolRecords:     session.Symbols.Count, GroupRecords: session.Groups.Count,
		HistoryDecoded: session.HistoryRead, HistoryRecords: session.History.Count,
		TradeRecords: session.Account.TradeRecords, TradeTrailingBytes: session.Account.TrailingBytes,
	}, nil
}
