package message

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"hash/crc64"
)

type Token []byte

func (t Token) String() string {
	return hex.EncodeToString(t)
}

func (t Token) Hash() uint64 {
	return crc64.Checksum(t, crc64.MakeTable(crc64.ISO))
}

// GetToken generates a random token with the maximum size (8 bytes).
func GetToken() (Token, error) {
	return GetTokenSize(MaxTokenSize)
}

// GetTokenSize generates a random token of the specified length.
// Per RFC 7252, length must be 0-8. A zero length returns a nil token.
func GetTokenSize(length int) (Token, error) {
	if length < 0 || length > MaxTokenSize {
		return nil, fmt.Errorf("invalid token length %d: must be 0-%d", length, MaxTokenSize)
	}
	if length == 0 {
		return nil, nil
	}
	b := make(Token, length)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}
