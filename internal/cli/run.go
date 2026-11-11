// Package cli adapts local input and redacted reports to the standalone SDK.
// Protocol code never imports this package.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"math"
	"time"

	"portexa/mt4"
)

type inputConfig struct {
	Target       string           `json:"target"`
	Server       string           `json:"server"`
	Login        uint32           `json:"login"`
	Password     string           `json:"password"`
	PasswordType mt4.PasswordType `json:"password_type"`
	Expected     *expectedAccount `json:"expected_account,omitempty"`
}

type expectedAccount struct {
	Balance  *float64 `json:"balance,omitempty"`
	Credit   *float64 `json:"credit,omitempty"`
	Leverage *uint32  `json:"leverage,omitempty"`
	Equity   *float64 `json:"equity,omitempty"`
	Currency *string  `json:"currency,omitempty"`
	Margin   *float64 `json:"margin,omitempty"`
}

// Run handles one CLI invocation; it does not mutate global flags or process IO.
func Run(ctx context.Context, args []string, input io.Reader, output, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("mt4probe", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	authenticate := flags.Bool("authenticate", false, "authenticate and read protocol data")
	historyDays := flags.Int("history-days", 0, "request 1 to 31 days of history")
	historyAll := flags.Bool("history-all", false, "request history from Unix time zero, bounded to 16 MiB")
	historyWindow := flags.Uint("history-window-seconds", 0, "split history into overlapping windows of at least two seconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *historyDays < 0 || *historyDays > 31 {
		return errors.New("expected flags only; history-days must be between 0 and 31")
	}
	if *historyWindow == 1 || uint64(*historyWindow) > math.MaxInt32 {
		return errors.New("history-window-seconds must be zero or between 2 and 2147483647")
	}
	if *historyAll && *historyDays != 0 {
		return errors.New("history-all and history-days are mutually exclusive")
	}
	if !*authenticate && (*historyAll || *historyDays != 0 || *historyWindow != 0) {
		return errors.New("history queries require authenticate")
	}
	if *historyWindow != 0 && !*historyAll && *historyDays == 0 {
		return errors.New("history-window-seconds requires a history query")
	}
	var config inputConfig
	decoder := json.NewDecoder(io.LimitReader(input, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return errors.New("cannot decode credentials from stdin")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("expected exactly one local configuration")
	}
	settings := mt4.Config{Address: config.Target, Server: config.Server, Login: config.Login, Password: config.Password, PasswordType: config.PasswordType}
	var result mt4.Result
	var err error
	if *authenticate {
		var options mt4.Options
		options.HistoryWindowSeconds = uint32(*historyWindow)
		if *historyDays > 0 || *historyAll {
			interval := &mt4.HistoryRange{End: math.MaxInt32}
			if !*historyAll {
				interval.Start = uint32(time.Now().Unix()) - uint32(*historyDays)*86400
			}
			options.History = interval
		}
		result, err = mt4.Fetch(ctx, settings, options)
	} else {
		result, err = mt4.Probe(ctx, settings)
	}
	summary := Summary(result)
	summary["investor_password_declared"] = settings.PasswordType == mt4.InvestorPassword
	var failure *mt4.Error
	if errors.As(err, &failure) {
		summary["error_code"], summary["error_operation"] = failure.Code, failure.Operation
		summary["error_server_build"], summary["error_server_status"] = failure.ServerBuild, failure.Status
	}
	if result.AccountRead {
		matches := compareAccount(result.Account, config.Expected)
		summary["local_reference_matches"] = matches
		verified := true
		for _, key := range []string{"balance_at_two_decimals", "credit_at_two_decimals", "equity_at_two_decimals", "margin_at_two_decimals", "currency"} {
			verified = verified && matches[key]
		}
		summary["account_reference_verified"] = verified
	}
	if outputErr := json.NewEncoder(output).Encode(summary); outputErr != nil {
		return outputErr
	}
	return err
}
