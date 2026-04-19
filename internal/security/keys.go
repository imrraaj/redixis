package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"math/big"
	"regexp"
)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var tenantIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{12,32}$`)

func GenerateAlphanumeric(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("length must be positive")
	}

	out := make([]byte, length)
	max := big.NewInt(int64(len(alphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

func GenerateTenantID() (string, error) {
	return GenerateAlphanumeric(16)
}

func GenerateAPIKey() (string, error) {
	return GenerateAlphanumeric(40)
}

func ValidateTenantID(tenantID string) bool {
	return tenantIDPattern.MatchString(tenantID)
}

func HashAPIKey(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return stringFromBytes(sum[:])
}

func CompareAPIKeyHash(expectedHash string, apiKey string) bool {
	actualHash := HashAPIKey(apiKey)
	return subtle.ConstantTimeCompare([]byte(expectedHash), []byte(actualHash)) == 1
}

func stringFromBytes(data []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(data)*2)
	for i, b := range data {
		out[i*2] = hex[b>>4]
		out[i*2+1] = hex[b&0x0f]
	}
	return string(out)
}
