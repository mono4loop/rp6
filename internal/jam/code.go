package jam

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

// codeAlphabet is Crockford-ish lowercase base32 without padding, chosen to be
// easy to read aloud and type.
var codeAlphabet = base32.NewEncoding("abcdefghijkmnpqrstuvwxyz23456789").WithPadding(base32.NoPadding)

// NewCode returns a random, shareable session code (e.g.
// "k7pq-2m9x-hf3t-rw8n") carrying ~80 bits of entropy. Peers that join with the
// same code land in the same session: because the code travels in the signaling
// URL path (/s/<code>) it is the sole capability gating who can connect, so it
// is deliberately high-entropy (guessing is infeasible even against a public
// server) rather than merely memorable.
func NewCode() string {
	var b [10]byte // 10 bytes -> 16 base32 chars -> grouped 4x4
	_, _ = rand.Read(b[:])
	s := codeAlphabet.EncodeToString(b[:])
	var out strings.Builder
	for i := 0; i < len(s); i += 4 {
		if i > 0 {
			out.WriteByte('-')
		}
		end := min(i+4, len(s))
		out.WriteString(s[i:end])
	}
	return out.String()
}

// NormalizeCode canonicalizes a code so minor formatting differences (case,
// surrounding whitespace) still match when comparing / joining.
func NormalizeCode(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
