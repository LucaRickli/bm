package signature

import (
	"fmt"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
)

var pgp = crypto.PGP()

func VerifyPGP(key, sig, data []byte) error {
	keyObj, err := crypto.NewKey(key)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	verifier, err := pgp.Verify().VerificationKey(keyObj).New()
	if err != nil {
		return fmt.Errorf("create verifier: %w", err)
	}

	result, err := verifier.VerifyDetached(data, sig, crypto.Armor)
	if err != nil {
		return fmt.Errorf("verify detached: %w", err)
	}

	if err := result.SignatureError(); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	return nil
}
