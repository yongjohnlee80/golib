package vegalite

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// SCALES. A quantitative field is on a linear scale, a temporal one on a time scale (its values
// milliseconds since 1970, UTC, as Vega-Lite's are), and a nominal or ordinal one on a band
// scale (a bar's) or a point scale (a line's or a point's). Ticks are d3's: a linear scale's at
// 1, 2 or 5 times a power of ten, a time scale's at the calendar's boundaries.

// numberOf is v as a quantitative value: a number, or a string that is one. ok is false for
// anything else (null, a word), which Vega-Lite leaves out of a continuous scale.
func numberOf(v any) (float64, bool) {
	var f float64
	var err error
	switch t := v.(type) {
	case json.Number:
		f, err = t.Float64()
	case float64:
		f = t
	case string:
		f, err = strconv.ParseFloat(strings.TrimSpace(t), 64)
	default:
		return 0, false
	}
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// dateLayouts are the forms a date string is read in: ISO 8601's and a few written ones.
var dateLayouts = []string{
	time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02 15:04",
	"2006-01-02", "2006/01/02", "2006-01", "2006/01", "2006", "Jan 2, 2006", "January 2, 2006", "2 Jan 2006", "Jan 2006", "January 2006",
}

// timeOf is v as a temporal value, in milliseconds since 1970 UTC: a number is already one, a
// string is read in dateLayouts (as UTC when it names no zone).
func timeOf(v any) (float64, bool) {
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		for _, layout := range dateLayouts {
			if t, err := time.Parse(layout, s); err == nil {
				return float64(t.UnixMilli()), true
			}
		}
		return 0, false
	}
	return numberOf(v)
}

// textOf is v as a discrete value: its text.
func textOf(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// tickStep is d3's tick increment: about n ticks over [lo, hi], at 1, 2 or 5 times a power of ten.
func tickStep(lo, hi float64, n int) float64 {
	span := hi - lo
	if span <= 0 || n < 1 {
		return 1
	}
	raw := span / float64(n)
	p := math.Pow(10, math.Floor(math.Log10(raw)))
	switch e := raw / p; {
	case e >= math.Sqrt(50):
		return 10 * p
	case e >= math.Sqrt(10):
		return 5 * p
	case e >= math.Sqrt(2):
		return 2 * p
	}
	return p
}

// niceDomain extends [lo, hi] out to its ticks' step, as d3's nice does.
func niceDomain(lo, hi float64, n int) (float64, float64) {
	for range 3 { // the step of the extended domain can be larger once more
		step := tickStep(lo, hi, n)
		nlo, nhi := math.Floor(lo/step)*step, math.Ceil(hi/step)*step
		if nlo == lo && nhi == hi {
			break
		}
		lo, hi = nlo, nhi
	}
	return lo, hi
}

// linearTicks are about n ticks in [lo, hi], and the step between them.
func linearTicks(lo, hi float64, n int) ([]float64, float64) {
	step := tickStep(lo, hi, n)
	var ticks []float64
	for i := math.Ceil(lo / step); i*step <= hi+step*1e-9; i++ {
		v := i * step
		if math.Abs(v) < step*1e-9 {
			v = 0
		}
		ticks = append(ticks, v)
		if len(ticks) > 1000 {
			break
		}
	}
	return ticks, step
}

// formatNumber is v as an axis labels it: thousands grouped, with as many decimals as step needs.
func formatNumber(v, step float64) string {
	decimals := 0
	if step > 0 && step < 1 {
		decimals = int(math.Ceil(-math.Log10(step) - 1e-9))
	}
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	if neg && strings.Trim(s, "0.") != "" {
		b.WriteString("−")
	}
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if frac != "" {
		b.WriteString("." + frac)
	}
	return b.String()
}

// formatPercent is a share (1 is all) as a normalized axis labels it.
func formatPercent(v, step float64) string {
	return formatNumber(v*100, step*100) + "%"
}

// timeUnit is a calendar unit a time scale's ticks fall on.
type timeUnit uint8

const (
	unitSecond timeUnit = iota
	unitMinute
	unitHour
	unitDay
	unitWeek
	unitMonth
	unitYear
)

// timeInterval is a tick interval: every n units.
type timeInterval struct {
	unit timeUnit
	n    int
	ms   float64 // about how long it is
}

const (
	msSecond = 1000.0
	msMinute = 60 * msSecond
	msHour   = 60 * msMinute
	msDay    = 24 * msHour
	msWeek   = 7 * msDay
	msMonth  = 30 * msDay
	msYear   = 365 * msDay
)

// timeIntervals are d3's, shortest first; past a year, years at a linear scale's step.
var timeIntervals = []timeInterval{
	{unitSecond, 1, msSecond}, {unitSecond, 5, 5 * msSecond}, {unitSecond, 15, 15 * msSecond}, {unitSecond, 30, 30 * msSecond},
	{unitMinute, 1, msMinute}, {unitMinute, 5, 5 * msMinute}, {unitMinute, 15, 15 * msMinute}, {unitMinute, 30, 30 * msMinute},
	{unitHour, 1, msHour}, {unitHour, 3, 3 * msHour}, {unitHour, 6, 6 * msHour}, {unitHour, 12, 12 * msHour},
	{unitDay, 1, msDay}, {unitDay, 2, 2 * msDay}, {unitWeek, 1, msWeek},
	{unitMonth, 1, msMonth}, {unitMonth, 3, 3 * msMonth}, {unitYear, 1, msYear},
}

// pickInterval is the interval giving about n ticks over [lo, hi], as d3's ticks picks it.
func pickInterval(lo, hi float64, n int) timeInterval {
	target := (hi - lo) / float64(max(n, 1))
	for i, iv := range timeIntervals {
		if iv.ms >= target {
			if i > 0 && target/timeIntervals[i-1].ms < iv.ms/target {
				return timeIntervals[i-1]
			}
			return iv
		}
	}
	years := tickStep(lo/msYear, hi/msYear, n)
	return timeInterval{unitYear, max(int(years), 1), math.Max(years, 1) * msYear}
}

// floorTo is t at or before it on iv's boundaries: a minute's 0th second, a 3-hour's 0, 3, 6…
func floorTo(t time.Time, iv timeInterval) time.Time {
	y, mo, d := t.Date()
	switch iv.unit {
	case unitSecond:
		return t.Truncate(time.Duration(iv.n) * time.Second)
	case unitMinute:
		return time.Date(y, mo, d, t.Hour(), t.Minute()/iv.n*iv.n, 0, 0, time.UTC)
	case unitHour:
		return time.Date(y, mo, d, t.Hour()/iv.n*iv.n, 0, 0, 0, time.UTC)
	case unitDay:
		return time.Date(y, mo, (d-1)/iv.n*iv.n+1, 0, 0, 0, 0, time.UTC)
	case unitWeek: // d3's weeks start on Sunday
		return time.Date(y, mo, d-int(t.Weekday()), 0, 0, 0, 0, time.UTC)
	case unitMonth:
		return time.Date(y, (mo-1)/time.Month(iv.n)*time.Month(iv.n)+1, 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(y/iv.n*iv.n, 1, 1, 0, 0, 0, 0, time.UTC)
}

// step is t one interval on.
func step(t time.Time, iv timeInterval) time.Time {
	switch iv.unit {
	case unitSecond:
		return t.Add(time.Duration(iv.n) * time.Second)
	case unitMinute:
		return t.Add(time.Duration(iv.n) * time.Minute)
	case unitHour:
		return t.Add(time.Duration(iv.n) * time.Hour)
	case unitDay:
		next := t.AddDate(0, 0, iv.n)
		if next.Month() != t.Month() { // d3's every-n-days restarts each month
			next = time.Date(next.Year(), next.Month(), 1, 0, 0, 0, 0, time.UTC)
		}
		return next
	case unitWeek:
		return t.AddDate(0, 0, 7)
	case unitMonth:
		return t.AddDate(0, iv.n, 0)
	}
	return t.AddDate(iv.n, 0, 0)
}

// timeTicks are about n ticks in [lo, hi] (milliseconds), on the calendar's boundaries.
func timeTicks(lo, hi float64, n int) []float64 {
	iv := pickInterval(lo, hi, n)
	var ticks []float64
	for t := floorTo(time.UnixMilli(int64(math.Floor(lo))).UTC(), iv); float64(t.UnixMilli()) <= hi; t = step(t, iv) {
		if ms := float64(t.UnixMilli()); ms >= lo {
			ticks = append(ticks, ms)
		}
		if len(ticks) > 1000 {
			break
		}
	}
	return ticks
}

// formatTime is a time tick's label, by the coarsest boundary it is on, as d3's multi-scale
// format writes it: a year, a month, a day, a time, or a second.
func formatTime(ms float64) string {
	t := time.UnixMilli(int64(ms)).UTC()
	switch {
	case t.Nanosecond() != 0:
		return t.Format(".000")
	case t.Second() != 0:
		return t.Format(":05")
	case t.Minute() != 0 || t.Hour() != 0:
		return t.Format("15:04")
	case t.Day() != 1:
		return t.Format("Jan 02")
	case t.Month() != time.January:
		return t.Format("Jan")
	}
	return t.Format("2006")
}
