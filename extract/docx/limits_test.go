package docx

import "testing"

// TestTheDefaultLimitsFitADocumentNotABomb: an entry decompresses to 32 MiB at most and the
// archive to 64 MiB, a document's own XML with room to spare.
func TestTheDefaultLimitsFitADocumentNotABomb(t *testing.T) {
	if DefaultMaxEntry != 32<<20 || DefaultMaxTotal != 64<<20 {
		t.Errorf("defaults %d and %d, want 32 MiB and 64 MiB", DefaultMaxEntry, DefaultMaxTotal)
	}
}
