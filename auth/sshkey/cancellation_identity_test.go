package sshkey

import (
	"context"
	"errors"
	"testing"
)

// A caller that cancels its own verification must be able to tell that apart
// from a verifier that broke.
//
// Both conditions refuse admission, and both are ErrVerifierUnavailable by
// design — the sentinel's contract is "could not reach a verdict", which a
// cancellation is. What was missing is the caller's own cause: the context
// error was rendered into text with %v, so errors.Is(err, context.Canceled)
// answered FALSE for a cancellation the caller had just requested. It could
// not distinguish its own cancel from an outage worth paging someone about.
func TestVerifiers_CancellationKeepsTheContextCause(t *testing.T) {
	bin := sshKeygen(t)
	dir := t.TempDir()
	alice := genKey(t, bin, dir, "alice@host")
	signersPath, allowedSet := allowedSigners(t, dir, [][2]string{{alice.comment, alice.pubLine}})
	sig := signWith(t, bin, dir, alice, testNS, []byte("m"))

	openssh, err := NewOpenSSH(signersPath, Binary(bin))
	if err != nil {
		t.Fatal(err)
	}
	pureGo, err := NewPureGo(allowedSet)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for name, v := range map[string]Verifier{"OpenSSH": openssh, "PureGo": pureGo} {
		t.Run(name, func(t *testing.T) {
			// Positive control: the same inputs verify under a live context, so
			// only the cancellation can be responsible for the refusal below.
			if err := v.VerifySignature(context.Background(), []byte("m"), sig, testNS, alice.comment); err != nil {
				t.Fatalf("fixture does not verify under a live context: %v", err)
			}

			err := v.VerifySignature(ctx, []byte("m"), sig, testNS, alice.comment)
			if err == nil {
				t.Fatal("a cancelled context must refuse admission")
			}

			// UNCHANGED, and it must stay that way: every caller that denies on
			// an unavailable verifier still denies on a cancellation, and the
			// documented contract says a cancelled context is one of these.
			if !errors.Is(err, ErrVerifierUnavailable) {
				t.Errorf("must still satisfy ErrVerifierUnavailable — removing this "+
					"would let a caller that branches on it fall through on a "+
					"cancellation; got %v", err)
			}

			// NEW: the caller's own cause survives, so it can tell "I cancelled
			// this" from "the verifier is broken" without reading the sentence.
			if !errors.Is(err, context.Canceled) {
				t.Errorf("must satisfy context.Canceled so the caller can recognise "+
					"its OWN cancellation; got %v", err)
			}

			// And the change is purely ADDITIVE: %w renders an error exactly as
			// %v did, so nothing reading these messages sees a difference. This
			// is asserted rather than argued, because "we only added an
			// identity" is the claim the whole change rests on.
			const want = "sshkey: verifier unavailable: verification cancelled by the caller: context canceled"
			if err.Error() != want {
				t.Errorf("the message changed:\n got  %q\n want %q", err.Error(), want)
			}
		})
	}
}
