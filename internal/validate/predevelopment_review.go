package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func digestValue(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func shortDigest(digest string) string {
	if len(digest) <= 16 {
		return digest
	}
	return digest[:16]
}
