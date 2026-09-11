package validate

import (
	"crypto/sha256"
	"encoding/hex"
)

// stateIntegrityHashBytes computes the sha256 over the normalized state bytes.
func stateIntegrityHashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
