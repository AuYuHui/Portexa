// Package cli adapts local input and redacted reports to the standalone SDK.
// Protocol code never imports this package.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"time"

	"portexa/mt4"
)

type inputConfig struct {
	Target   string           `json:"target"`
	Server   string           `json:"server"`
	Login    uint32           `json:"login"`
	Password string           `json:"password"`
	Expected *expectedAccount `json:"expected_account,omitempty"`
}

type expectedAccount struct {
	Balance  *float64 `json:"balance,omitempty"`
	Credit   *float64 `json:"credit,omitempty"`
	Leverage *uint32  `json:"leverage,omitempty"`
}

// Run handles one CLI invocation; it does not mutate global flags or process IO.
func Run(ctx context.Context, args []string, input io.Reader, output, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("mt4probe", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	authenticate := flags.Bool("authenticate", false, "authenticate and read protocol data")
	historyDays := flags.Int("history-days", 0, "request 1 to 31 days of history")
	historyAll := flags.Bool("history-all", false, "request history from Unix time zero, bounded to 16 MiB")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *historyDays < 0 || *historyDays > 31 {
		return errors.New("expected flags only; history-days must be between 0 and 31")
	}
	var config inputConfig
	decoder := json.NewDecoder(io.LimitReader(input, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return errors.New("cannot decode credentials from stdin")
	}
	settings := mt4.Config{Address: config.Target, Server: config.Server, Login: config.Login, Password: config.Password}
	var result mt4.Result
	var err error
	if *authenticate {
		var options mt4.Options
		if *historyDays > 0 || *historyAll {
			interval := &mt4.HistoryRange{End: uint32(time.Now().Unix())}
			if !*historyAll {
				interval.Start = interval.End - uint32(*historyDays)*86400
			}
			options.History = interval
		}
		result, err = mt4.Fetch(ctx, settings, options)
	} else {
		result, err = mt4.Probe(ctx, settings)
	}
	if err != nil && !result.FirstResponseDecoded {
		return err
	}
	summary := Summary(result)
	if result.AccountRead {
		summary["local_reference_matches"] = compareAccount(result.Account, config.Expected)
	}
	if outputErr := json.NewEncoder(output).Encode(summary); outputErr != nil {
		return outputErr
	}
	return err
}
