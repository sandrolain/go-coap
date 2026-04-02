package message

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetToken(t *testing.T) {
	token, err := GetToken()
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Len(t, token, MaxTokenSize)
	require.NotEqual(t, 0, token.Hash())
}

func TestGetTokenSize(t *testing.T) {
	// Valid lengths 1-8
	for l := 1; l <= MaxTokenSize; l++ {
		tok, err := GetTokenSize(l)
		require.NoError(t, err)
		require.Len(t, tok, l)
	}

	// Zero length returns nil token (RFC 7252 §5.3.1: "A token of zero length is acceptable")
	tok, err := GetTokenSize(0)
	require.NoError(t, err)
	require.Nil(t, tok)

	// Invalid lengths return error
	_, err = GetTokenSize(-1)
	require.Error(t, err)
	_, err = GetTokenSize(MaxTokenSize + 1)
	require.Error(t, err)
}
