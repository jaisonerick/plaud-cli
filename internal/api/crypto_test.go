package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// The server never tells us whether an envelope was malformed or the password
// was simply wrong: both come back as a failed login. So the scheme is pinned
// here against a vector produced by eciesjs, the library the web client ships,
// configured exactly as the bundle configures it (secp256k1, uncompressed
// ephemeral and HKDF keys, AES-256-GCM, 16-byte nonce).
const (
	referencePrivKey   = "f9e7d2a02c6ceefd4590f8b79b6af668b13ff5140bcaea5e41db7cc2a4827351"
	referencePubKey    = "03565039cd9ca53ca784f7d692fa133cf89d3924325cce282f822305a46a8e3660"
	referencePlaintext = `{"pass":"s3nha-de-teste!","time":1786000000}`
	referenceEnvelope  = "BEyG417VRylogNJPeoZ7GSC8OW91Td9bKvWAyphMSQX2kzj4qQ4HAp+1i6I3neCqpXuGITk2L/pnWQla8D0GvuyaNl36aIHr4rUn/gkaFgfGvnwX65owGkpaf1RZr2PhVjHIC63o0pW5DnBVVSJ8dFL79MUw4WDZYNbpYbE4CESgPPWoAx7jtqCgmJGq"
)

func TestEncryptPasswordMatchesReferenceImplementation(t *testing.T) {
	plaintext, err := decryptEnvelope(referencePrivKey, referenceEnvelope)
	if err != nil {
		t.Fatalf("decrypting the eciesjs vector: %v", err)
	}
	if string(plaintext) != referencePlaintext {
		t.Fatalf("decrypted %q, want %q", plaintext, referencePlaintext)
	}
}

func TestEncryptPasswordRoundTrip(t *testing.T) {
	envelope, err := EncryptPassword(referencePubKey, "s3nha-de-teste!")
	if err != nil {
		t.Fatalf("EncryptPassword: %v", err)
	}
	plaintext, err := decryptEnvelope(referencePrivKey, envelope)
	if err != nil {
		t.Fatalf("decrypting our own envelope: %v", err)
	}
	var payload passwordPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		t.Fatalf("payload is not the JSON the server expects: %v (%s)", err, plaintext)
	}
	if payload.Pass != "s3nha-de-teste!" {
		t.Errorf("password came back as %q", payload.Pass)
	}
	if payload.Time == 0 {
		t.Error("payload carries no timestamp, so the server cannot reject a replay")
	}
}

func TestEncryptPasswordIsNeverDeterministic(t *testing.T) {
	first, err := EncryptPassword(referencePubKey, "same-password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncryptPassword(referencePubKey, "same-password")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("two envelopes for the same password are identical, so the ephemeral key is not ephemeral")
	}
}

// decryptEnvelope is the inverse of EncryptPassword. Only the tests need it:
// the CLI never decrypts, since the private key lives on the server.
func decryptEnvelope(privKeyHex, envelopeB64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(envelopeB64)
	if err != nil {
		return nil, err
	}
	ephemeralBytes := raw[:ephemeralKeySize]
	nonce := raw[ephemeralKeySize : ephemeralKeySize+nonceSize]
	tag := raw[ephemeralKeySize+nonceSize : ephemeralKeySize+nonceSize+tagSize]
	ciphertext := raw[ephemeralKeySize+nonceSize+tagSize:]

	privBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return nil, err
	}
	priv := secp256k1.PrivKeyFromBytes(privBytes)
	ephemeral, err := secp256k1.ParsePubKey(ephemeralBytes)
	if err != nil {
		return nil, err
	}

	var ephemeralPoint, shared secp256k1.JacobianPoint
	ephemeral.AsJacobian(&ephemeralPoint)
	secp256k1.ScalarMultNonConst(&priv.Key, &ephemeralPoint, &shared)
	shared.ToAffine()

	material := make([]byte, 0, 2*ephemeralKeySize)
	material = append(material, ephemeral.SerializeUncompressed()...)
	material = append(material, secp256k1.NewPublicKey(&shared.X, &shared.Y).SerializeUncompressed()...)
	key, err := hkdf.Key(sha256.New, material, nil, "", 32)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, nonceSize)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, append(append([]byte{}, ciphertext...), tag...), nil)
}
