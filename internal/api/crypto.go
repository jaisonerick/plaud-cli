package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// Sizes of the envelope produced by EncryptPassword, fixed by the scheme the
// server accepts.
const (
	ephemeralKeySize = 65 // uncompressed secp256k1 public key
	nonceSize        = 16 // AES-GCM with a non-default nonce length
	tagSize          = 16
)

// passwordPayload is what actually gets encrypted. The timestamp is there so
// the server can reject an envelope that was captured and replayed later.
type passwordPayload struct {
	Pass string `json:"pass"`
	Time int64  `json:"time"`
}

// EncryptPassword seals a password for the Plaud auth endpoints, which never
// receive it in the clear. The scheme is ECIES over secp256k1 as the web client
// implements it: an ephemeral key pair per call, HKDF-SHA256 over the ephemeral
// public key concatenated with the shared point, then AES-256-GCM with a
// 16-byte nonce. The envelope is
//
//	ephemeral public key (65) || nonce (16) || tag (16) || ciphertext
//
// base64-encoded. Note the tag precedes the ciphertext, which is the opposite
// of Go's own AES-GCM output.
//
// pubKeyHex is the server's public key, from GET /config/security.
func EncryptPassword(pubKeyHex, password string) (string, error) {
	pubBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return "", fmt.Errorf("decoding server public key: %w", err)
	}
	receiver, err := secp256k1.ParsePubKey(pubBytes)
	if err != nil {
		return "", fmt.Errorf("parsing server public key: %w", err)
	}

	ephemeral, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return "", fmt.Errorf("generating ephemeral key: %w", err)
	}

	key, err := deriveSharedKey(ephemeral, receiver)
	if err != nil {
		return "", err
	}

	plaintext, err := json.Marshal(passwordPayload{Pass: password, Time: time.Now().Unix()})
	if err != nil {
		return "", fmt.Errorf("encoding payload: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("initialising cipher: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, nonceSize)
	if err != nil {
		return "", fmt.Errorf("initialising GCM: %w", err)
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, plaintext, nil) // ciphertext || tag
	ciphertext, tag := sealed[:len(sealed)-tagSize], sealed[len(sealed)-tagSize:]

	envelope := make([]byte, 0, ephemeralKeySize+nonceSize+tagSize+len(ciphertext))
	envelope = append(envelope, ephemeral.PubKey().SerializeUncompressed()...)
	envelope = append(envelope, nonce...)
	envelope = append(envelope, tag...)
	envelope = append(envelope, ciphertext...)
	return base64.StdEncoding.EncodeToString(envelope), nil
}

// deriveSharedKey reproduces eciesjs' encapsulate: HKDF-SHA256 over the
// ephemeral public key followed by the shared point, both uncompressed.
func deriveSharedKey(ephemeral *secp256k1.PrivateKey, receiver *secp256k1.PublicKey) ([]byte, error) {
	var receiverPoint, shared secp256k1.JacobianPoint
	receiver.AsJacobian(&receiverPoint)
	secp256k1.ScalarMultNonConst(&ephemeral.Key, &receiverPoint, &shared)
	shared.ToAffine()

	material := make([]byte, 0, 2*ephemeralKeySize)
	material = append(material, ephemeral.PubKey().SerializeUncompressed()...)
	material = append(material, secp256k1.NewPublicKey(&shared.X, &shared.Y).SerializeUncompressed()...)

	key, err := hkdf.Key(sha256.New, material, nil, "", 32)
	if err != nil {
		return nil, fmt.Errorf("deriving key: %w", err)
	}
	return key, nil
}
