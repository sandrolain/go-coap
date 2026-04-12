// Package udp_test — RFC 9175 / RFC 8768 conformance tests.
//
// RFC 9175: Echo, Request-Tag, and Token Processing for CoAP.
// RFC 8768: Hop-Limit Option for CoAP.
//
// Test IDs: ET_001 – ET_006 (Echo & Request-Tag), HL_001 – HL_002 (HopLimit).
// References:
//
//	https://www.rfc-editor.org/rfc/rfc9175
//	https://www.rfc-editor.org/rfc/rfc8768
package udp_test

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/message/pool"
	"github.com/plgd-dev/go-coap/v3/mux"
	"github.com/plgd-dev/go-coap/v3/net/responsewriter"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/plgd-dev/go-coap/v3/udp/client"
	"github.com/stretchr/testify/require"
)

// RFC 9175 / RFC 8768 option IDs not yet merged to main branch — use numeric values.
const (
	optEcho       = message.OptionID(252) // RFC 9175 §2 — Echo
	optRequestTag = message.OptionID(292) // RFC 9175 §3 — Request-Tag
	optHopLimit   = message.OptionID(16)  // RFC 8768 §3 — Hop-Limit
)

// TC_CoAP_ET_001 – TP_CoAP_Echo_ServerIncludes
//
// Reference: RFC 9175 Section 2.2
// "The server MAY include an Echo option in a response to enable the
// client to demonstrate freshness in the next request."
//
// Procedure: client sends GET /time; server includes Echo in the response.
// Expected: client reads Echo option of 4 bytes from the response.
func TestTC_CoAP_ET_001_Echo_ServerIncludes(t *testing.T) {
	echoChallenge := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	m := mux.NewRouter()
	err := m.Handle("/time", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
		require.NoError(t, errS)
		// Add Echo option to response after SetResponse.
		w.Message().SetOptionBytes(optEcho, echoChallenge)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/time")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	echoVal, errE := resp.GetOptionBytes(optEcho)
	require.NoError(t, errE,
		"RFC 9175 §2.2: server MUST be able to include Echo option in response")
	require.Equal(t, echoChallenge, echoVal,
		"RFC 9175 §2.2: Echo option value MUST be preserved intact in the response")
}

// TC_CoAP_ET_002 – TP_CoAP_Echo_ClientSendsInRequest
//
// Reference: RFC 9175 Section 2.3
// "A client MUST include the Echo option in a request if the server
// previously sent an Echo option with the same token."
//
// Procedure: client sends GET with Echo option; server verifies it is received.
// Expected: server receives Echo option with correct value.
func TestTC_CoAP_ET_002_Echo_ClientSendsInRequest(t *testing.T) {
	sentEcho := []byte{0x01, 0x02, 0x03, 0x04}
	var receivedEcho []byte
	var mu sync.Mutex

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		echo, errE := r.GetOptionBytes(optEcho)
		if errE == nil {
			mu.Lock()
			receivedEcho = echo
			mu.Unlock()
		}
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/resource")
	require.NoError(t, err)
	req.SetOptionBytes(optEcho, sentEcho)

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	mu.Lock()
	require.Equal(t, sentEcho, receivedEcho,
		"RFC 9175 §2.3: Echo option in request MUST be delivered intact to the server handler")
	mu.Unlock()
}

// TC_CoAP_ET_003 – TP_CoAP_Echo_FreshnessChallenge
//
// Reference: RFC 9175 Section 2.4
// "If the server requires a fresh request, it MUST respond with
// 4.01 (Unauthorized) and include an Echo option to challenge the client.
// The client repeats the request with the Echo option set to the value
// from the challenge response."
//
// Procedure:
//  1. Client sends GET /protected (no Echo) → server challenges with 4.01 + Echo.
//  2. Client extracts Echo, retries GET /protected with Echo → server accepts, 2.05 Content.
func TestTC_CoAP_ET_003_Echo_FreshnessChallenge(t *testing.T) {
	challengeEcho := []byte{0xCA, 0xFE, 0xBA, 0xBE}
	var mu sync.Mutex
	challenged := false

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		echo, errE := r.GetOptionBytes(optEcho)
		mu.Lock()
		alreadyChallenged := challenged
		mu.Unlock()

		if errE != nil || !bytes.Equal(echo, challengeEcho) {
			// No Echo present (or wrong value): issue freshness challenge.
			mu.Lock()
			challenged = true
			mu.Unlock()
			_ = w.SetResponse(codes.Unauthorized, message.TextPlain, nil,
				message.Option{ID: optEcho, Value: challengeEcho})
			return
		}
		// Echo matches the challenge: request is fresh.
		_ = alreadyChallenged // consumed above
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("fresh")))
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// Step 1: request without Echo → challenge.
	resp1, err := cc.Get(ctx, "/protected")
	require.NoError(t, err)
	require.Equal(t, codes.Unauthorized, resp1.Code(),
		"RFC 9175 §2.4: server MUST challenge with 4.01 Unauthorized when freshness is required")

	echoVal, errE := resp1.GetOptionBytes(optEcho)
	require.NoError(t, errE,
		"RFC 9175 §2.4: 4.01 challenge MUST carry an Echo option")
	require.Equal(t, challengeEcho, echoVal)

	// Step 2: retry with echoed value → accepted.
	req, err := cc.NewGetRequest(ctx, "/protected")
	require.NoError(t, err)
	req.SetOptionBytes(optEcho, echoVal)

	resp2, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp2.Code(),
		"RFC 9175 §2.4: request with correct Echo value MUST be accepted (2.05 Content)")
}

// TC_CoAP_ET_004 – TP_CoAP_Echo_ByteIntegrity
//
// Reference: RFC 9175 Section 2.2
// "The Echo option value is opaque and MUST be echoed back without modification.
// The server MUST verify the exact byte sequence."
//
// Procedure: server issues Echo=0xAABBCCDD. Client echoes a different value.
// Expected: server rejects with 4.01 Unauthorized (wrong Echo value).
func TestTC_CoAP_ET_004_Echo_ByteIntegrity(t *testing.T) {
	correctEcho := []byte{0xAA, 0xBB, 0xCC, 0xDD}

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		echo, errE := r.GetOptionBytes(optEcho)
		if errE != nil || !bytes.Equal(echo, correctEcho) {
			// Echo absent or does not match exactly → reject.
			_ = w.SetResponse(codes.Unauthorized, message.TextPlain, nil,
				message.Option{ID: optEcho, Value: correctEcho})
			return
		}
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("verified")))
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// Send wrong Echo value.
	req, err := cc.NewGetRequest(ctx, "/secure")
	require.NoError(t, err)
	req.SetOptionBytes(optEcho, []byte{0x00, 0x11, 0x22, 0x33}) // wrong

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Unauthorized, resp.Code(),
		"RFC 9175 §2.2: server MUST reject request with incorrect Echo value")

	// Send correct Echo value.
	req2, err := cc.NewGetRequest(ctx, "/secure")
	require.NoError(t, err)
	req2.SetOptionBytes(optEcho, correctEcho) // correct

	resp2, err := cc.Do(req2)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp2.Code(),
		"RFC 9175 §2.2: request with exact Echo bytes MUST be accepted")
}

// TC_CoAP_ET_005 – TP_CoAP_RequestTag_ReceivedByServer
//
// Reference: RFC 9175 Section 3.3
// "The Request-Tag option is an opaque byte sequence used by the client
// to identify request bodies across multiple block-wise messages. The
// server MUST be able to receive and process Request-Tag options."
//
// Procedure: client sends GET with Request-Tag option containing {0x01, 0x02}.
// Expected: server receives Request-Tag with exact byte value.
func TestTC_CoAP_ET_005_RequestTag_ReceivedByServer(t *testing.T) {
	sentTag := []byte{0x01, 0x02}
	var receivedTag []byte
	var mu sync.Mutex

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		tag, errT := r.GetOptionBytes(optRequestTag)
		if errT == nil {
			mu.Lock()
			receivedTag = tag
			mu.Unlock()
		}
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/resource")
	require.NoError(t, err)
	req.SetOptionBytes(optRequestTag, sentTag)

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	mu.Lock()
	require.Equal(t, sentTag, receivedTag,
		"RFC 9175 §3.3: Request-Tag option MUST be received intact by the server")
	mu.Unlock()
}

// TC_CoAP_ET_006 – TP_CoAP_RequestTag_Differentiation
//
// Reference: RFC 9175 Section 3.3
// "Two requests with different Request-Tag values are treated as distinct
// by the server. The server observes each tag independently."
//
// Procedure: two requests with different Request-Tag values.
// Expected: server sees two distinct non-equal tags.
func TestTC_CoAP_ET_006_RequestTag_Differentiation(t *testing.T) {
	tagA := []byte{0xAA}
	tagB := []byte{0xBB}

	var mu sync.Mutex
	receivedTags := [][]byte{}

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		tag, errT := r.GetOptionBytes(optRequestTag)
		if errT == nil {
			// Clone the tag: the underlying buffer is reused by the message pool.
			cloned := make([]byte, len(tag))
			copy(cloned, tag)
			mu.Lock()
			receivedTags = append(receivedTags, cloned)
			mu.Unlock()
		}
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	sendWithTag := func(tag []byte) {
		cc, errD := udp.Dial(addr)
		require.NoError(t, errD)
		defer func() { _ = cc.Close(); <-cc.Done() }()

		ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
		defer cancel()

		req, errF := cc.NewGetRequest(ctx, "/data")
		require.NoError(t, errF)
		req.SetOptionBytes(optRequestTag, tag)

		_, errDo := cc.Do(req)
		require.NoError(t, errDo)
	}

	sendWithTag(tagA)
	sendWithTag(tagB)

	mu.Lock()
	require.Len(t, receivedTags, 2,
		"RFC 9175 §3.3: server MUST receive Request-Tag from each request")
	require.NotEqual(t, receivedTags[0], receivedTags[1],
		"RFC 9175 §3.3: different Request-Tag values MUST be distinguishable at the server")
	mu.Unlock()
}

// TC_CoAP_HL_001 – TP_CoAP_HopLimit_ReceivedByEndpoint
//
// Reference: RFC 8768 Section 3
// "A CoAP endpoint (non-proxy) that receives a request with a Hop-Limit
// option MUST NOT reject the message solely because the option is present.
// It processes the request normally."
//
// Procedure: client sends GET /data with HopLimit=64. Server is a non-proxy endpoint.
// Expected: server responds 2.05 Content without error.
func TestTC_CoAP_HL_001_HopLimit_ReceivedByEndpoint(t *testing.T) {
	var receivedHopLimit uint32
	var mu sync.Mutex

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		hl, errHL := r.Options().GetUint32(optHopLimit)
		if errHL == nil {
			mu.Lock()
			receivedHopLimit = hl
			mu.Unlock()
		}
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("data")))
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/data")
	require.NoError(t, err)
	req.SetOptionUint32(optHopLimit, 64)

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code(),
		"RFC 8768 §3: non-proxy endpoint MUST process request with HopLimit normally")

	mu.Lock()
	require.EqualValues(t, 64, receivedHopLimit,
		"RFC 8768 §3: Hop-Limit option value MUST be received intact by the endpoint")
	mu.Unlock()
}

// TC_CoAP_HL_002 – TP_CoAP_HopLimit_ValueOne_AtEndpoint
//
// Reference: RFC 8768 Section 3
// "Only a CoAP proxy decrements Hop-Limit. A non-proxy endpoint that
// receives Hop-Limit=1 MUST still process the message normally — it is
// the final destination and does not forward."
//
// Procedure: client sends GET /resource with HopLimit=1.
// Expected: non-proxy server accepts and responds 2.05 Content.
func TestTC_CoAP_HL_002_HopLimit_ValueOne_AtEndpoint(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/resource", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/resource")
	require.NoError(t, err)
	req.SetOptionUint32(optHopLimit, 1) // minimum valid HopLimit at a non-proxy

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code(),
		"RFC 8768 §3: HopLimit=1 at a non-proxy endpoint MUST NOT cause an error response")
}

// ── Additional RFC 9175 / RFC 8768 conformance tests (ET_007–ET_009) ──────

// TC_CoAP_ET_007 – TP_CoAP_Echo_MaxSize_40Bytes
//
// Reference: RFC 9175 Section 2.2
// "The Echo option is opaque and between 1 and 40 bytes in length."
// A 40-byte Echo value is the maximum allowed size.
//
// Procedure: server sends Echo with exactly 40 bytes. Client echoes it.
// Expected: 40-byte Echo option is transmitted and received correctly.
func TestTC_CoAP_ET_007_Echo_MaxSize_40Bytes(t *testing.T) {
	echoMax := make([]byte, 40)
	for i := range echoMax {
		echoMax[i] = byte(i + 1)
	}

	var receivedEcho []byte
	var mu sync.Mutex

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		echo, errE := r.GetOptionBytes(optEcho)
		if errE == nil && bytes.Equal(echo, echoMax) {
			// Correct 40-byte Echo echoed back.
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("fresh")))
			mu.Lock()
			receivedEcho = echo
			mu.Unlock()
			return
		}
		// Challenge with 40-byte Echo.
		_ = w.SetResponse(codes.Unauthorized, message.TextPlain, nil,
			message.Option{ID: optEcho, Value: echoMax})
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// Step 1: get challenge.
	resp1, err := cc.Get(ctx, "/protected")
	require.NoError(t, err)
	require.Equal(t, codes.Unauthorized, resp1.Code())

	echoVal, err := resp1.GetOptionBytes(optEcho)
	require.NoError(t, err)
	require.Len(t, echoVal, 40,
		"RFC 9175 §2.2: Echo option size MUST be between 1 and 40 bytes")

	// Step 2: echo back.
	req, err := cc.NewGetRequest(ctx, "/protected")
	require.NoError(t, err)
	req.SetOptionBytes(optEcho, echoVal)

	resp2, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp2.Code(),
		"RFC 9175 §2.2: 40-byte Echo must be accepted when echoed correctly")

	mu.Lock()
	require.Equal(t, echoMax, receivedEcho,
		"RFC 9175 §2.2: 40-byte Echo value must be preserved intact")
	mu.Unlock()
}

// TC_CoAP_ET_008 – TP_CoAP_RequestTag_MaxSize_8Bytes
//
// Reference: RFC 9175 Section 3.2
// "The Request-Tag is an opaque byte string between 0 and 8 bytes."
// An 8-byte Request-Tag is the maximum allowed size.
//
// Procedure: client sends GET with 8-byte Request-Tag.
// Expected: server receives the full 8-byte Request-Tag.
func TestTC_CoAP_ET_008_RequestTag_MaxSize_8Bytes(t *testing.T) {
	maxTag := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	var receivedTag []byte
	var mu sync.Mutex

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		tag, errT := r.GetOptionBytes(optRequestTag)
		if errT == nil {
			mu.Lock()
			receivedTag = tag
			mu.Unlock()
		}
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/resource")
	require.NoError(t, err)
	req.SetOptionBytes(optRequestTag, maxTag)

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	mu.Lock()
	require.Equal(t, maxTag, receivedTag,
		"RFC 9175 §3.2: 8-byte Request-Tag (maximum) must be received intact")
	mu.Unlock()
}

// TC_CoAP_ET_009 – TP_CoAP_RequestTag_Empty
//
// Reference: RFC 9175 Section 3.2
// "A zero-length Request-Tag is valid and means that no tag is associated."
//
// Procedure: client sends GET with 0-byte Request-Tag.
// Expected: server receives the empty Request-Tag option.
func TestTC_CoAP_ET_009_RequestTag_Empty(t *testing.T) {
	gotEmptyTag := false
	var mu sync.Mutex

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		tag, errT := r.GetOptionBytes(optRequestTag)
		if errT == nil && len(tag) == 0 {
			mu.Lock()
			gotEmptyTag = true
			mu.Unlock()
		}
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/resource")
	require.NoError(t, err)
	req.SetOptionBytes(optRequestTag, []byte{}) // 0-byte empty tag

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	mu.Lock()
	require.True(t, gotEmptyTag,
		"RFC 9175 §3.2: 0-byte Request-Tag must be received by server")
	mu.Unlock()
}
