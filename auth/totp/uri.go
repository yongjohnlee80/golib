package totp

import (
	"strconv"
	"strings"
	"time"
)

// URI returns the otpauth://totp/ line an authenticator app reads from a QR
// code, with these Params' algorithm, digits and period:
//
//	otpauth://totp/Example%20Co:alice%40example.com?secret=...&issuer=Example%20Co&algorithm=SHA1&digits=6&period=30
//
// The label is "issuer:account", or the account alone when issuer is empty, in
// which case the issuer parameter is left out too. Issuer and account are
// percent-encoded byte by byte, everything but letters, digits and "-._~", so
// a colon, slash, "@", "&", "+" or space in either can neither split the label
// nor end a parameter, and a space is "%20" (never "+", which some apps keep as
// a plus).
func (p Params) URI(issuer, account string, secret []byte) string {
	var b strings.Builder
	b.WriteString("otpauth://totp/")
	if issuer != "" {
		b.WriteString(escape(issuer))
		b.WriteByte(':')
	}
	b.WriteString(escape(account))
	b.WriteString("?secret=")
	b.WriteString(Encode(secret))
	if issuer != "" {
		b.WriteString("&issuer=")
		b.WriteString(escape(issuer))
	}
	b.WriteString("&algorithm=")
	b.WriteString(p.Algorithm().String())
	b.WriteString("&digits=")
	b.WriteString(strconv.Itoa(p.Digits()))
	b.WriteString("&period=")
	b.WriteString(strconv.FormatInt(int64(p.Period()/time.Second), 10))
	return b.String()
}

// URI returns the otpauth://totp/ line for the defaults (SHA-1, 6 digits, 30
// seconds). See [Params.URI].
func URI(issuer, account string, secret []byte) string { return Params{}.URI(issuer, account, secret) }

// escape percent-encodes every byte of s but the unreserved ones: ASCII
// letters, digits and "-._~".
func escape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}
