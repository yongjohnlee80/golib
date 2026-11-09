package kit

import (
	"math/big"
	"strconv"
	"strings"
)

// CompareCells orders two cells: as exact decimals when both are (ParseDecimal, as the WHERE
// filter reads numbers), else as text.
func CompareCells(a, b string) int { return ComparePlain(ReadPlainCell(a), ReadPlainCell(b)) }

// PlainCell is a cell without a kind as the sort reads it: its text, and its number when the text
// is a decimal.
type PlainCell struct {
	text string
	num  *big.Rat // nil: not a decimal
}

// ReadPlainCell is text as the sort reads it: its number too, when it is a decimal.
func ReadPlainCell(text string) PlainCell {
	n, _ := ParseDecimal(strings.TrimSpace(text))
	return PlainCell{text: text, num: n}
}

// ComparePlain orders two read cells: by number when both are decimals, else by text.
func ComparePlain(a, b PlainCell) int {
	if a.num != nil && b.num != nil {
		return a.num.Cmp(b.num)
	}
	return strings.Compare(a.text, b.text)
}

// decimalExpMax bounds a decimal's exponent: a big.Rat of 1e999999999 would take a gigabyte. It is
// far past any float64 and any number a cell holds in digits (those have no exponent).
const decimalExpMax = 10000

// DecimalEnd is where the decimal at s[i] ends: [sign] digits [. digits] [e [sign] digits], at
// least one digit before the exponent.
func DecimalEnd(s string, i int) int {
	digits := func() int {
		j := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i - j
	}
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	n := digits()
	if i < len(s) && s[i] == '.' {
		i++
		n += digits()
	}
	if n == 0 {
		return start
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		mark := i
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if digits() == 0 {
			i = mark
		}
	}
	return i
}

// ParseDecimal is s as an exact decimal, when the whole of s is one and its exponent is in range.
// Other syntaxes big.Rat reads (a fraction, a base prefix) are not decimals.
func ParseDecimal(s string) (*big.Rat, bool) {
	if s == "" || DecimalEnd(s, 0) != len(s) {
		return nil, false
	}
	if e := strings.IndexAny(s, "eE"); e >= 0 {
		exp := strings.TrimLeft(strings.TrimLeft(s[e+1:], "+-"), "0")
		if len(exp) > 5 {
			return nil, false
		}
		if n, _ := strconv.Atoi("0" + exp); n > decimalExpMax {
			return nil, false
		}
	}
	return new(big.Rat).SetString(s)
}
