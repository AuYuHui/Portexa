package analytics

import (
	"context"
	"github.com/shopspring/decimal"
	"sort"
	"time"
)

var beijing = time.FixedZone("Asia/Shanghai", 8*3600)

func monthly(days []Day) []Month {
	months := []Month{}
	for start := 0; start < len(days); {
		month := days[start].Date[:7]
		end, sum := start, decimal.Zero
		reasons := []string{}
		for end < len(days) && days[end].Date[:7] == month {
			if days[end].NetProfit.Value == nil {
				reasons = append(reasons, days[end].NetProfit.Reasons...)
			} else {
				sum = sum.Add(*days[end].NetProfit.Value)
			}
			end++
		}
		date, _ := time.ParseInLocation("2006-01", month, beijing)
		value := known(sum)
		if len(reasons) > 0 {
			value = unavailable(reasons...)
		}
		months = append(months, Month{Month: month, NetProfit: value, Partial: days[start].FromMS != date.UnixMilli() || days[end-1].ToMS != date.AddDate(0, 1, 0).UnixMilli()})
		start = end
	}
	return months
}

func calendar(ctx context.Context, input Input) ([]Day, error) {
	days := []Day{}
	if input.ToMS <= input.FromMS {
		return days, nil
	}
	start := time.UnixMilli(input.FromMS).In(beijing)
	date := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, beijing)
	for date.UnixMilli() < input.ToMS {
		end := date.AddDate(0, 0, 1)
		from, to := max(date.UnixMilli(), input.FromMS), min(end.UnixMilli(), input.ToMS)
		a := sort.Search(len(input.Samples), func(i int) bool { return input.Samples[i].TimeMS >= from })
		b := sort.Search(len(input.Samples), func(i int) bool { return input.Samples[i].TimeMS > to })
		part := input
		part.FromMS, part.ToMS = from, to
		part.Samples, part.Coverage = input.Samples[a:b], []bool{}
		if b-a > 1 {
			part.Coverage = input.Coverage[a : b-1]
		}
		part.Events = eventsBetween(input.Events, from, to)
		part.Unclassified = 0
		for _, event := range part.Events {
			if event.Kind == "unclassified" || event.Kind == "adjustment" {
				part.Unclassified++
			}
		}
		part.RawSamples = 0
		result, err := Calculate(ctx, part, false)
		if err != nil {
			return days, err
		}
		var trades *int
		if result.Metrics.Realized.Value != nil {
			value := totalEvents(part.Events).trades
			trades = &value
		}
		days = append(days, Day{Date: date.Format("2006-01-02"), FromMS: from, ToMS: to,
			Partial: from != date.UnixMilli() || to != end.UnixMilli(), NetProfit: result.Metrics.NetProfit,
			Realized: result.Metrics.Realized, FloatingChange: result.Metrics.FloatingChange,
			OtherIncome: result.Metrics.OtherIncome, Costs: result.Metrics.Costs, Trades: trades})
		date = end
	}
	return days, nil
}

// Select complete display evidence after all financial computations. Every
// bucket retains endpoints, gaps and extrema of each financial series.
func reducePoints(points []Point) ([]Point, bool) {
	if len(points) <= 2400 {
		return points, false
	}
	selected := map[int]bool{}
	for bucket := 0; bucket < 240; bucket++ {
		a, b := bucket*len(points)/240, (bucket+1)*len(points)/240
		selected[a], selected[b-1] = true, true
		minProfit, maxProfit, minReturn, maxReturn, maxDD := -1, -1, -1, -1, -1
		for i := a; i < b; i++ {
			p := points[i]
			if p.NetProfit != nil {
				if minProfit < 0 || p.NetProfit.LessThan(*points[minProfit].NetProfit) {
					minProfit = i
				}
				if maxProfit < 0 || p.NetProfit.GreaterThan(*points[maxProfit].NetProfit) {
					maxProfit = i
				}
			}
			if p.ReturnPct != nil {
				if minReturn < 0 || p.ReturnPct.LessThan(*points[minReturn].ReturnPct) {
					minReturn = i
				}
				if maxReturn < 0 || p.ReturnPct.GreaterThan(*points[maxReturn].ReturnPct) {
					maxReturn = i
				}
			}
			if p.DrawdownPct != nil && (maxDD < 0 || p.DrawdownPct.GreaterThan(*points[maxDD].DrawdownPct)) {
				maxDD = i
			}
		}
		for _, i := range []int{minProfit, maxProfit, minReturn, maxReturn, maxDD} {
			if i >= 0 {
				selected[i] = true
			}
		}
	}
	// Segment IDs survive reduction. Renderers must insert a gap whenever the
	// ID changes, even when the intervening missing minute was not selected.
	out := make([]Point, 0, len(selected))
	for i, p := range points {
		if selected[i] {
			out = append(out, p)
		}
	}
	return out, true
}
