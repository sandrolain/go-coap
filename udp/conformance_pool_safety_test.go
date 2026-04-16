// Package udp_test — Pool buffer safety tests.
//
// These tests document and verify the behavior of GetOptionBytes() when
// pool.Message objects are reused — the returned slice MUST be cloned
// if it is stored beyond the handler's lifetime.
//
// Reference: message/pool/message.go GetOptionBytes
package udp_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/message/pool"
	"github.com/plgd-dev/go-coap/v3/net/responsewriter"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/plgd-dev/go-coap/v3/udp/client"
	"github.com/stretchr/testify/require"
)

// optETagForPool is the ETag option ID (message.ETag).
const optETagForPool = message.ETag

// TC_CoAP_POOL_001 – TP_Pool_GetOptionBytes_CloneRequired
//
// Demonstrates that GetOptionBytes returns a slice into the internal buffer.
// If two requests reuse the same message pool slot, a previously-stored
// (un-cloned) slice may be overwritten.
//
// Procedure: two sequential requests from different clients set different
// ETag values. Server stores cloned copies. Both must be distinct.
// Expected: cloned values remain intact after pool message reuse.
func TestTC_CoAP_POOL_001_GetOptionBytes_CloneRequired(t *testing.T) {
	etagA := []byte{0x11, 0x22}
	etagB := []byte{0x33, 0x44}

	var mu sync.Mutex
	var storedETags [][]byte

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		etag, err := r.GetOptionBytes(optETagForPool)
		if err == nil {
			// CORRECT: clone before storing
			cloned := make([]byte, len(etag))
			copy(cloned, etag)
			mu.Lock()
			storedETags = append(storedETags, cloned)
			mu.Unlock()
		}
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	sendWithETag := func(etag []byte) {
		cc, err := udp.Dial(addr)
		require.NoError(t, err)
		defer func() { _ = cc.Close(); <-cc.Done() }()

		ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
		defer cancel()

		req, err := cc.NewGetRequest(ctx, "/resource")
		require.NoError(t, err)
		req.SetOptionBytes(optETagForPool, etag)

		_, err = cc.Do(req)
		require.NoError(t, err)
	}

	sendWithETag(etagA)
	sendWithETag(etagB)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, storedETags, 2,
		"Pool safety: both ETags must be received by server")
	require.NotEqual(t, storedETags[0], storedETags[1],
		"Pool safety: cloned ETags must remain distinct after pool reuse")
	require.Equal(t, etagA, storedETags[0],
		"Pool safety: first ETag must be 0x1122")
	require.Equal(t, etagB, storedETags[1],
		"Pool safety: second ETag must be 0x3344")
}

// TC_CoAP_POOL_002 – TP_Pool_ReadBody_SafeAfterHandler
//
// Demonstrates that ReadBody returns a copy of the payload that is safe to use
// after the handler returns (payload is fully read into a new slice).
//
// Procedure: client sends POST with payload; server reads body in handler
// and stores it; second request with different payload; both stored.
// Expected: both stored bodies are intact and distinct.
func TestTC_CoAP_POOL_002_ReadBody_SafeAfterHandler(t *testing.T) {
	var mu sync.Mutex
	var storedBodies [][]byte

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		body, err := r.ReadBody()
		if err == nil && len(body) > 0 {
			mu.Lock()
			storedBodies = append(storedBodies, body)
			mu.Unlock()
		}
		_ = w.SetResponse(codes.Created, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	for _, payload := range [][]byte{[]byte("payload-alpha"), []byte("payload-beta")} {
		cc, err := udp.Dial(addr)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
		resp, err := cc.Post(ctx, "/store", message.TextPlain, bytes.NewReader(payload))
		require.NoError(t, err)
		require.Equal(t, codes.Created, resp.Code())
		cancel()
		_ = cc.Close()
		<-cc.Done()
	}

	// Brief wait for server handlers to complete
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, storedBodies, 2,
		"Pool safety: both request bodies must be stored")
	require.Equal(t, []byte("payload-alpha"), storedBodies[0])
	require.Equal(t, []byte("payload-beta"), storedBodies[1])
}
