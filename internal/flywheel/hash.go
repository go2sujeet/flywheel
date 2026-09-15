package flywheel

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// contentSHA returns the sha256 hex of b with every \r\n replaced by \n; a
// lone \r is kept. Dispatch hashes the prompt through contentSHA and T1
// compares through it, so a CRLF checkout of an unchanged brief still
// verifies.
func contentSHA(b []byte) string {
	norm := strings.ReplaceAll(string(b), "\r\n", "\n")
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}
