package handlers

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// GenerateOTP returns a random 6-digit numeric code, zero-padded.
func GenerateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
