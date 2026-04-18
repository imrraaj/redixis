package main

import (
	"crypto/rand"
	"encoding/hex"
)

func validateTenantID(tenantID string) bool {
	return len(tenantID) == 8
}

func generateAPIKey() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
