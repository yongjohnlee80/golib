package web

import (
	"testing"
	"time"
)

// setClock makes now the clock an Admission and its gate age their slots by.
func (a *Admission) setClock(now func() time.Time) {
	a.now = now
	a.gate.now = now
}

// admissionFor attaches a fresh Admission to an SSO built without one, as NewSSO does when its
// SSOConfig carries an Admission: the Admission's cap is the park's.
func admissionFor[T any](t testing.TB, s *SSO[T]) *Admission {
	t.Helper()
	a := NewAdmission(AdmissionCap(s.max))
	if err := a.attach(s); err != nil {
		t.Fatal(err)
	}
	s.adm = a
	return a
}
