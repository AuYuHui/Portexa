package accounts

import (
	"regexp"
	"strings"

	"github.com/shopspring/decimal"
	"portexa/internal/storage"
)

const FundingRuleVersion = "balance-comment-v3"

var fundCommentRules = []struct {
	category, label string
	pattern         *regexp.Regexp
}{
	{"deposit", "券商入金编号", regexp.MustCompile(`(?i)^D\(DIN[0-9]+\)$`)},
	{"withdrawal", "券商出金编号", regexp.MustCompile(`(?i)^W\(DOUT[0-9]+\)$`)},
	{"deposit", "DPrime 入金标识", regexp.MustCompile(`(?i)^dbu(?:[ \t]+#IT[0-9]+)?$`)},
	{"withdrawal", "DPrime 出金标识", regexp.MustCompile(`(?i)^wbu(?:[ \t]+#IT[0-9]+)?$`)},
	{"profit_share_expense", "DPrime 跟单分润支出标识", regexp.MustCompile(`(?i)^-PF(?:\|[0-9]+\|&[0-9]+)?$`)},
	{"deposit", "入金标识", regexp.MustCompile(`(?i)\bdeposit(?:s|ed)?\b|入金|存款`)},
	{"withdrawal", "出金标识", regexp.MustCompile(`(?i)\bwithdraw(?:al|als|n)?\b|出金|提款|取款|提现`)},
	{"rebate", "返佣标识", regexp.MustCompile(`(?i)\brebates?\b|\bcashback\b|返佣`)},
	{"fee", "费用标识", regexp.MustCompile(`(?i)\bfees?\b|手续费|费用`)},
	{"profit_share", "分成标识", regexp.MustCompile(`(?i)\bprofit[ _-]?shar(?:e|ing)\b|分成`)},
	{"transfer", "内部转账标识", regexp.MustCompile(`(?i)\binternal[ _-]?transfer\b|\btransfer[ _-]?(?:in|out)\b|内部转[入出账]`)},
	{"adjustment", "调整或赠金标识", regexp.MustCompile(`(?i)\b(?:bonus|credit|adjustment)\b|赠金|信用金|调整`)},
}

var uncertainFundComment = regexp.MustCompile(`(?i)\b(?:cancel(?:led|ed)?|reversal|reversed|refund|pending|test|interest)\b|撤销|冲正|退回|待处理|利息|\x{FFFD}`)

// Rules act on explicit source annotations, never on direction alone. The
// protocol only decodes text; this policy belongs to the application layer.
func automaticFundCategory(comment string, net, commission, swap, taxes decimal.Decimal) (string, string) {
	comment = strings.TrimSpace(comment)
	if comment == "" || uncertainFundComment.MatchString(comment) || !commission.IsZero() || !swap.IsZero() || !taxes.IsZero() {
		return "", ""
	}
	category, label := "", ""
	for _, rule := range fundCommentRules {
		if !rule.pattern.MatchString(comment) {
			continue
		}
		if category != "" {
			return "", ""
		}
		category, label = rule.category, rule.label
	}
	sign := net.Sign()
	switch category {
	case "deposit", "rebate":
		if sign <= 0 {
			return "", ""
		}
	case "withdrawal", "fee", "profit_share_expense":
		if sign >= 0 {
			return "", ""
		}
	case "profit_share":
		if sign > 0 {
			category = "profit_share_income"
		} else if sign < 0 {
			category = "profit_share_expense"
		} else {
			return "", ""
		}
	case "transfer":
		if sign > 0 {
			category = "transfer_in"
		} else if sign < 0 {
			category = "transfer_out"
		} else {
			return "", ""
		}
	case "adjustment":
	default:
		return "", ""
	}
	return category, "备注含" + label
}

// Automatic results are derived data. A rule upgrade can reinterpret retained
// source annotations without modifying source revisions or refetching MT4.
func currentAutomaticFundResult(order storage.HistoryOrder) (string, string, string) {
	if order.AutomaticRuleVersion == FundingRuleVersion {
		return order.AutomaticCategory, order.AutomaticReason, order.AutomaticRuleVersion
	}
	category, reason := automaticFundCategory(order.Comment, order.NetProfit, order.Commission, order.Swap, order.Taxes)
	return category, reason, FundingRuleVersion
}

// Manual corrections apply only to the exact source revision. Otherwise a
// newly observed source annotation is evaluated under the named rule version.
func effectiveFundClassification(order storage.HistoryOrder, manual *storage.CashFlowClassification) (*storage.CashFlowClassification, bool) {
	if order.Command != 6 {
		return nil, false
	}
	if manual != nil && manual.BasedTradeRevision == order.Revision {
		value := *manual
		value.Source = "manual"
		return &value, false
	}
	category, reason, version := currentAutomaticFundResult(order)
	if category != "" {
		value := storage.CashFlowClassification{AccountID: order.AccountID, Ticket: order.Ticket, BasedTradeRevision: order.Revision,
			Category: category, Reason: reason, Source: "automatic", RuleVersion: version}
		if manual != nil {
			value.Revision = manual.Revision
		}
		return &value, false
	}
	if manual != nil {
		value := *manual
		value.Source = "manual"
		return &value, true
	}
	return nil, false
}
