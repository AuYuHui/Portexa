package accounts

import (
	"strings"

	"portexa/mt5"
)

const maxBrowserInteger = 1<<53 - 1

func endpointHostname(protocol, value string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(value))
	switch protocol {
	case "mt4":
		if host != "" {
			return "", &InputError{"MT4 接入点不使用 Web 终端域名"}
		}
	case "mt5":
		if !mt5.ValidHostname(host) {
			return "", &InputError{"请填写有效的 MT5 Web 终端域名"}
		}
	default:
		return "", &InputError{"请选择 MT4 或 MT5"}
	}
	return host, nil
}

func checkProtocolPassword(protocol, password string) error {
	if protocol == "mt5" {
		if !mt5.ValidPassword(password) {
			return &InputError{"MT5 投资者密码需要1至32个UTF-16代码单元"}
		}
		return nil
	}
	return checkPassword(password)
}
