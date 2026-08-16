package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// SHA256File computes the lowercase hex SHA-256 digest of the file at
// path, streaming its bytes. It is the canonical executable fingerprint
// of the trust model: `plugin add --approve` pins this digest, startup
// verifies freshly computed digests against the pin before spawning,
// and every report exposes it for the user to compare.
func SHA256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
