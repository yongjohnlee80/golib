package mtls

import (
	"crypto/tls"
	"crypto/x509"

	"github.com/yongjohnlee80/golib/auth"
)

// FromConnectionState projects a completed TLS handshake into the view auth
// uses. crypto/tls is imported HERE rather than in the core, so a caller who
// never uses mTLS does not pull it into their build.
//
// Only VerifiedChains is carried across. PeerCertificates is deliberately NOT
// projected: passing it on would invite a future reader to authenticate from an
// unverified certificate, which is the mistake this package exists to prevent.
func FromConnectionState(cs *tls.ConnectionState) *auth.TLSState {
	if cs == nil {
		return nil
	}
	out := &auth.TLSState{}
	for _, chain := range cs.VerifiedChains {
		converted := make([]auth.Certificate, 0, len(chain))
		for _, c := range chain {
			converted = append(converted, FromX509(c))
		}
		out.VerifiedChains = append(out.VerifiedChains, converted)
	}
	return out
}

// FromX509 projects one certificate.
func FromX509(c *x509.Certificate) auth.Certificate {
	if c == nil {
		return auth.Certificate{}
	}
	uris := make([]string, 0, len(c.URIs))
	for _, u := range c.URIs {
		uris = append(uris, u.String())
	}
	out := auth.Certificate{
		CommonName:     c.Subject.CommonName,
		DNSNames:       c.DNSNames,
		EmailAddresses: c.EmailAddresses,
		URIs:           uris,
		NotBefore:      c.NotBefore,
		NotAfter:       c.NotAfter,
	}
	if c.SerialNumber != nil {
		out.SerialNumber = c.SerialNumber.String()
	}
	for _, eku := range c.ExtKeyUsage {
		if eku == x509.ExtKeyUsageClientAuth {
			out.IsClientAuth = true
			break
		}
	}
	return out
}
