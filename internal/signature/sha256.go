package signature

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// VerifySHA256 verifies data against a SHA256 checksum file.
//
// checksum may be a bare hex digest or a sha256sum(1)-format file with one or
// more lines of the form:
//
//	<hash>  <filename>
//	<hash> *<filename>
//
// When checksum contains multiple entries, filename is used to select the
// correct hash. When there is only one entry, filename is ignored.
func VerifySHA256(checksum, data []byte, filename string) error {
	entries := parseChecksumFile(checksum)
	if len(entries) == 0 {
		return fmt.Errorf("empty checksum file")
	}

	var expected string
	if filename != "" {
		expected = entries[filename]
	}
	if expected == "" {
		if len(entries) > 1 {
			return fmt.Errorf("checksum file has %d entries but filename %q not found", len(entries), filename)
		}
		for _, hash := range entries {
			expected = hash
		}
	}

	digest := sha256.Sum256(data)
	actual := hex.EncodeToString(digest[:])
	if actual != expected {
		return fmt.Errorf("SHA256 mismatch: got %s, want %s", actual, expected)
	}
	return nil
}

// parseChecksumFile parses a sha256sum(1)-format file into a map of filename → hash.
// Lines with only a hash (no filename) are stored under the empty string key.
func parseChecksumFile(data []byte) map[string]string {
	entries := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		switch len(fields) {
		case 1:
			if len(fields[0]) == 64 {
				entries[""] = strings.ToLower(fields[0])
			}
		case 2:
			hash := strings.ToLower(fields[0])
			name := strings.TrimPrefix(fields[1], "*")
			if len(hash) == 64 {
				entries[name] = hash
			}
		}
	}
	return entries
}
