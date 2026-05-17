package signature

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"

	sigsig "github.com/sigstore/sigstore/pkg/signature"
)

// sigstoreBundle is the subset of the Sigstore bundle format we need for verification.
// Full spec: https://github.com/sigstore/protobuf-specs
type sigstoreBundle struct {
	VerificationMaterial struct {
		Certificate struct {
			RawBytes string `json:"rawBytes"` // base64-encoded DER certificate
		} `json:"certificate"`
	} `json:"verificationMaterial"`
	MessageSignature struct {
		Signature string `json:"signature"` // base64-encoded signature
	} `json:"messageSignature"`
}

// VerifyBundle verifies a Sigstore bundle (.bundle) against data.
// It extracts the ephemeral certificate from the bundle, derives the public key,
// and verifies the detached signature — without requiring a Rekor transparency log check.
func VerifyBundle(bundleJSON, data []byte) error {
	var b sigstoreBundle
	if err := json.Unmarshal(bundleJSON, &b); err != nil {
		return fmt.Errorf("parse bundle: %w", err)
	}

	certDER, err := base64.StdEncoding.DecodeString(b.VerificationMaterial.Certificate.RawBytes)
	if err != nil {
		return fmt.Errorf("decode certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("parse certificate: %w", err)
	}

	verifier, err := sigsig.LoadVerifier(cert.PublicKey.(crypto.PublicKey), crypto.SHA256)
	if err != nil {
		return fmt.Errorf("load verifier: %w", err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(b.MessageSignature.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	if err := verifier.VerifySignature(bytes.NewReader(sigBytes), bytes.NewReader(data)); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}
	return nil
}
