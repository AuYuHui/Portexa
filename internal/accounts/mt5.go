package accounts

import "portexa/mt5"

const maxBrowserInteger = 1<<53 - 1

func checkProtocolPassword(protocol, password string) error {
	if protocol == "mt5" {
		if !mt5.ValidPassword(password) {
			return &InputError{"MT5 投资者密码需要1至32个UTF-16代码单元"}
		}
		return nil
	}
	return checkPassword(password)
}
