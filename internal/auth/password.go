package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = 64 * 1024
	argonIterations  = 3
	argonParallelism = 1
	argonKeyLength   = 32
	argonSaltLength  = 16
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	memory, iterations, parallelism, ok := parseArgonParameters(parts[3])
	if !ok || memory < uint32(parallelism)*8 || memory > 256*1024 || iterations > 10 || parallelism > 16 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func parseArgonParameters(value string) (uint32, uint32, uint8, bool) {
	var memory, iterations uint64
	var parallelism uint64
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	for _, part := range parts {
		keyValue := strings.SplitN(part, "=", 2)
		if len(keyValue) != 2 {
			return 0, 0, 0, false
		}
		number, err := strconv.ParseUint(keyValue[1], 10, 32)
		if err != nil || number == 0 {
			return 0, 0, 0, false
		}
		switch keyValue[0] {
		case "m":
			memory = number
		case "t":
			iterations = number
		case "p":
			parallelism = number
		default:
			return 0, 0, 0, false
		}
	}
	if memory == 0 || iterations == 0 || parallelism == 0 || parallelism > 255 {
		return 0, 0, 0, false
	}
	return uint32(memory), uint32(iterations), uint8(parallelism), true
}

func validPassword(password string) bool {
	return len(password) <= 1024 && utf8.ValidString(password) && utf8.RuneCountInString(password) >= 12
}
