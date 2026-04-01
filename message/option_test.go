package message

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMediaTypeString(t *testing.T) {
	for i := 0; i < 12000; i++ {
		func(mt int, s string) {
			if v, err := ToMediaType(s); err == nil {
				require.Equal(t, MediaType(mt), v)
			}
		}(i, MediaType(i).String())
	}
}

func TestOptionIDString(t *testing.T) {
	for i := 0; i < 12000; i++ {
		func(oid int, s string) {
			if v, err := ToOptionID(s); err == nil {
				require.Equal(t, OptionID(oid), v)
			}
		}(i, OptionID(i).String())
	}
}

func TestRFC9175_9177_8768_OptionIDs(t *testing.T) {
	// RFC 8768 - Hop Limit
	require.Equal(t, OptionID(16), HopLimit)
	require.Equal(t, "HopLimit", HopLimit.String())

	// RFC 9175 - Echo and Request-Tag
	require.Equal(t, OptionID(252), Echo)
	require.Equal(t, "Echo", Echo.String())
	require.Equal(t, OptionID(292), RequestTag)
	require.Equal(t, "RequestTag", RequestTag.String())

	// RFC 9177 - Q-Block1 and Q-Block2
	require.Equal(t, OptionID(2048), QBlock1)
	require.Equal(t, "QBlock1", QBlock1.String())
	require.Equal(t, OptionID(2049), QBlock2)
	require.Equal(t, "QBlock2", QBlock2.String())

	// Verify option definitions exist with correct formats
	def, ok := CoapOptionDefs[Echo]
	require.True(t, ok)
	require.Equal(t, ValueOpaque, def.ValueFormat)
	require.Equal(t, uint32(1), def.MinLen)
	require.Equal(t, uint32(40), def.MaxLen)

	def, ok = CoapOptionDefs[RequestTag]
	require.True(t, ok)
	require.Equal(t, ValueOpaque, def.ValueFormat)
	require.Equal(t, uint32(0), def.MinLen)
	require.Equal(t, uint32(8), def.MaxLen)

	def, ok = CoapOptionDefs[QBlock1]
	require.True(t, ok)
	require.Equal(t, ValueUint, def.ValueFormat)
	require.Equal(t, uint32(0), def.MinLen)
	require.Equal(t, uint32(3), def.MaxLen)

	def, ok = CoapOptionDefs[QBlock2]
	require.True(t, ok)
	require.Equal(t, ValueUint, def.ValueFormat)

	def, ok = CoapOptionDefs[HopLimit]
	require.True(t, ok)
	require.Equal(t, ValueUint, def.ValueFormat)
	require.Equal(t, uint32(1), def.MinLen)
	require.Equal(t, uint32(1), def.MaxLen)

	// Verify ToOptionID works for new options
	for _, tc := range []struct {
		str string
		id  OptionID
	}{
		{"Echo", Echo},
		{"RequestTag", RequestTag},
		{"QBlock1", QBlock1},
		{"QBlock2", QBlock2},
		{"HopLimit", HopLimit},
	} {
		id, err := ToOptionID(tc.str)
		require.NoError(t, err)
		require.Equal(t, tc.id, id)
	}
}
