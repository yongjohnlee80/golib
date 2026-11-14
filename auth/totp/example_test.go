package totp_test

import (
	"fmt"
	"time"

	"github.com/yongjohnlee80/golib/auth/totp"
)

func Example() {
	// Enrolment: make a secret, store it encrypted, show the URI as a QR code.
	secret, err := totp.NewSecret()
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = totp.URI("Example Co", "alice@example.com", secret)

	// Login: check the code against the step last accepted for this subject,
	// and store the step returned in its place.
	var lastStep int64 = -1
	now := time.Now()
	code := totp.Code(secret, now) // what the app shows
	step, ok := totp.Verify(secret, code, now, lastStep)
	fmt.Println(ok, step > lastStep)

	_, again := totp.Verify(secret, code, now, step)
	fmt.Println(again)
	// Output:
	// true true
	// false
}
