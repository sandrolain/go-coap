package coder

import "errors"

var (
	ErrMessageTruncated      = errors.New("message is truncated")
	ErrMessageInvalidVersion = errors.New("message has invalid version")
	ErrLonePayloadMarker     = errors.New("lone payload marker (0xFF) with no payload")
)
