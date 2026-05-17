package signature

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/sigstore/sigstore/pkg/cryptoutils"
	sigsig "github.com/sigstore/sigstore/pkg/signature"
)

// VerifyCosign verifies a cosign key-based blob signature.
//
// key may be a PEM public key, a PEM X.509 certificate, or a base64-encoded
// version of either (as produced by `cosign sign-blob --key` or Kubernetes
// release artifacts). sig is the base64-encoded detached signature.
func VerifyCosign(key, sig, data []byte) error {
	pubKey, err := resolvePublicKey(key)
	if err != nil {
		return err
	}

	verifier, err := sigsig.LoadVerifier(pubKey, crypto.SHA256)
	if err != nil {
		return fmt.Errorf("load verifier: %w", err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	if err := verifier.VerifySignature(bytes.NewReader(sigBytes), bytes.NewReader(data)); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}
	return nil
}

// resolvePublicKey extracts a crypto.PublicKey from key bytes.
// It accepts a PEM public key, a PEM X.509 certificate, or a base64-encoded
// version of either.
func resolvePublicKey(key []byte) (crypto.PublicKey, error) {
	pemData := maybeBase64Decode(key)

	if pubKey, err := cryptoutils.UnmarshalPEMToPublicKey(pemData); err == nil {
		return pubKey, nil
	}

	certs, err := cryptoutils.UnmarshalCertificatesFromPEM(pemData)
	if err != nil || len(certs) == 0 {
		return nil, fmt.Errorf("parse public key: not a valid PEM public key or certificate")
	}
	return certs[0].PublicKey.(crypto.PublicKey), nil
}

// maybeBase64Decode returns the base64-decoded bytes of b when b is not already
// PEM-encoded; otherwise returns b as-is.
func maybeBase64Decode(b []byte) []byte {
	trimmed := bytes.TrimSpace(b)
	if bytes.HasPrefix(trimmed, []byte("-----")) {
		return trimmed
	}
	// Strip newlines that may appear in multi-line base64 blobs.
	stripped := strings.ReplaceAll(string(trimmed), "\n", "")
	stripped = strings.ReplaceAll(stripped, "\r", "")
	decoded, err := base64.StdEncoding.DecodeString(stripped)
	if err != nil {
		return trimmed
	}
	return decoded
}
