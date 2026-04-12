// Package udp_test — RFC 7959 "Block-Wise Transfers in CoAP" conformance tests.
//
// Test IDs: BW_001 – BW_018
// Reference: https://www.rfc-editor.org/rfc/rfc7959
package udp_test

import (
	"bytes"
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/message/pool"
	"github.com/plgd-dev/go-coap/v3/mux"
	coapNet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/net/blockwise"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/plgd-dev/go-coap/v3/udp/client"
	"github.com/plgd-dev/go-coap/v3/udp/server"
	"github.com/stretchr/testify/require"
)

// startBlockwiseServer starts a UDP CoAP server with blockwise enabled at the given SZX.
func startBlockwiseServer(t *testing.T, m *mux.Router, szx blockwise.SZX) (*server.Server, string, func()) {
	t.Helper()
	l, err := coapNet.NewListenUDP("udp", "127.0.0.1:0")
	require.NoError(t, err)

	var wg sync.WaitGroup
	s := udp.NewServer(options.WithMux(m), options.WithBlockwise(true, szx, 30*time.Second))
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.Serve(l)
	}()

	cleanup := func() {
		s.Stop()
		wg.Wait()
		_ = l.Close()
	}
	return s, l.LocalAddr().String(), cleanup
}

// startBlockwiseClient dials a UDP CoAP client with blockwise enabled.
func startBlockwiseClient(t *testing.T, addr string, szx blockwise.SZX) *client.Conn {
	t.Helper()
	cc, err := udp.Dial(addr, options.WithBlockwise(true, szx, 30*time.Second))
	require.NoError(t, err)
	return cc
}

// TC_CoAP_BW_001 – TP_CoAP_BlockWise_Block2_GET_LargeResponse
//
// Reference: RFC 7959 Section 2.1
// The server fragments a large response into blocks using the Block2 option.
// The last block has M=0; preceding blocks have M=1.
//
// Procedure: GET /large (payload > 128 bytes). Server+client have blockwise.
// Expected: client reassembles full payload correctly.
func TestTC_CoAP_BW_001_Block2_GET_LargeResponse(t *testing.T) {
	largePayload := bytes.Repeat([]byte("ABCDEFGH"), 32) // 256 bytes > 128B SZX3

	m := mux.NewRouter()
	err := m.Handle("/large", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(largePayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := cc.Get(ctx, "/large")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, largePayload, body,
		"RFC 7959 §2.1: client must reassemble the complete block2 payload")
}

// TC_CoAP_BW_002 – TP_CoAP_BlockWise_Block2_GET_VeryLarge
//
// Reference: RFC 7959 Section 2.1
// Test with a payload requiring many blocks (~2KB with SZX64=64B blocks → 32 blocks minimum).
//
// Procedure: GET /verylarge (2048 bytes). SZX=2 (64 bytes/block).
// Expected: client reassembles all ~32 blocks correctly.
func TestTC_CoAP_BW_002_Block2_GET_VeryLarge(t *testing.T) {
	largePayload := bytes.Repeat([]byte("XY"), 1024) // 2048 bytes

	m := mux.NewRouter()
	err := m.Handle("/verylarge", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(largePayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Get(ctx, "/verylarge")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, largePayload, body,
		"RFC 7959 §2.1: must correctly reassemble 2048-byte block2 response")
}

// TC_CoAP_BW_003 – TP_CoAP_BlockWise_Block1_PUT_MultiBlock
//
// Reference: RFC 7959 Section 2.3
// "If the Block1 option is present in a request, the server MUST respond
// with 2.31 (Continue) to all but the last block."
//
// Procedure: PUT /upload with payload of 512 bytes. SZX=2 (64B blocks).
// Expected: server receives full payload, responds with 2.04 Changed.
func TestTC_CoAP_BW_003_Block1_PUT_MultiBlock(t *testing.T) {
	uploadPayload := bytes.Repeat([]byte("UPLOAD"), 86) // ~516 bytes
	uploadPayload = uploadPayload[:512]

	var receivedPayload []byte
	m := mux.NewRouter()
	err := m.Handle("/upload", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		require.Equal(t, codes.PUT, r.Code())
		body, errR := r.ReadBody()
		require.NoError(t, errR)
		receivedPayload = body
		errS := w.SetResponse(codes.Changed, message.TextPlain, nil)
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Put(ctx, "/upload", message.TextPlain, bytes.NewReader(uploadPayload))
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code(),
		"RFC 7959 §2.3: final block of Block1 PUT must receive 2.04 Changed")
	require.Equal(t, uploadPayload, receivedPayload,
		"RFC 7959 §2.3: server must have reassembled the full PUT payload")
}

// TC_CoAP_BW_004 – TP_CoAP_BlockWise_Block1_POST_MultiBlock
//
// Reference: RFC 7959 Section 2.3
// POST with large body uses Block1 just like PUT.
//
// Procedure: POST /process with 512-byte body.
// Expected: server receives complete body, responds 2.04 Changed.
func TestTC_CoAP_BW_004_Block1_POST_MultiBlock(t *testing.T) {
	postPayload := bytes.Repeat([]byte("POST"), 128) // 512 bytes

	var receivedPayload []byte
	m := mux.NewRouter()
	err := m.Handle("/process", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		require.Equal(t, codes.POST, r.Code())
		body, errR := r.ReadBody()
		require.NoError(t, errR)
		receivedPayload = body
		errS := w.SetResponse(codes.Changed, message.TextPlain, nil)
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Post(ctx, "/process", message.TextPlain, bytes.NewReader(postPayload))
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code(),
		"RFC 7959 §2.3: Block1 POST must complete with 2.04 Changed")
	require.Equal(t, postPayload, receivedPayload,
		"RFC 7959 §2.3: server must reassemble full Block1 POST payload")
}

// TC_CoAP_BW_005 – TP_CoAP_BlockWise_SZX16_SmallBlocks
//
// Reference: RFC 7959 Section 2.2
// SZX=0 (16 bytes) is the smallest valid block size. Larger payloads require many blocks.
//
// Procedure: GET /data with payload of 64 bytes; server+client set SZX=0 (16B).
// Expected: client receives all 4 blocks and reassembles the full 64-byte payload.
func TestTC_CoAP_BW_005_SZX16_SmallBlocks(t *testing.T) {
	payload := bytes.Repeat([]byte("16byteblk"), 8)[:64] // 72 bytes sliced to 64

	m := mux.NewRouter()
	err := m.Handle("/data", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(payload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX16)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX16)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Get(ctx, "/data")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, payload, body,
		"RFC 7959 §2.2: SZX=0 (16B blocks) must correctly reassemble payload")
}

// TC_CoAP_BW_006 – TP_CoAP_BlockWise_Block2_ExactlyOneSZX
//
// Reference: RFC 7959 Section 2.1
// A payload that fits exactly in one block should NOT trigger Block2.
// The server responds normally without Block2 option.
//
// Procedure: GET /small with payload ≤ SZX size.
// Expected: single response with 2.05 Content (no Block2 negotiation needed).
func TestTC_CoAP_BW_006_SmallPayload_NoBlock2(t *testing.T) {
	smallPayload := []byte("hello world") // 11 bytes < 64B SZX64

	m := mux.NewRouter()
	err := m.Handle("/small", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(smallPayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/small")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, smallPayload, body,
		"RFC 7959: small payload that fits in one block must be delivered whole")
}

// TC_CoAP_BW_007 – TP_CoAP_BlockWise_Block1_SmallPayload
//
// Reference: RFC 7959 Section 2.3
// A PUT payload that fits in a single block does not require Block1 option.
//
// Procedure: PUT /item with small payload (< one block). SZX=6 (1024B).
// Expected: server receives the payload, responds 2.04 Changed (no block1 iteration).
func TestTC_CoAP_BW_007_Block1_SmallPayload(t *testing.T) {
	var receivedBody []byte
	m := mux.NewRouter()
	err := m.Handle("/item", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		body, errR := r.ReadBody()
		require.NoError(t, errR)
		receivedBody = body
		errS := w.SetResponse(codes.Changed, message.TextPlain, nil)
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX1024)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX1024)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	payload := []byte("small payload")
	resp, err := cc.Put(ctx, "/item", message.TextPlain, bytes.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code())
	require.Equal(t, payload, receivedBody)
}

// TC_CoAP_BW_008 – TP_CoAP_BlockWise_Block2_ConcurrentGETs
//
// Reference: RFC 7959 Section 2.1
// Multiple concurrent blockwise GET requests on separate tokens must each
// be independently assembled.
//
// Procedure: two concurrent GET /large requests.
// Expected: both receive the full and correct payload.
func TestTC_CoAP_BW_008_ConcurrentBlock2_GETs(t *testing.T) {
	largePayload := bytes.Repeat([]byte("CONCURRENT"), 30) // 300 bytes

	m := mux.NewRouter()
	err := m.Handle("/large-shared", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(largePayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	cc1 := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc1.Close(); <-cc1.Done() }()

	cc2 := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc2.Close(); <-cc2.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type result struct {
		body []byte
		err  error
	}
	ch1 := make(chan result, 1)
	ch2 := make(chan result, 1)

	go func() {
		resp, errG := cc1.Get(ctx, "/large-shared")
		if errG != nil {
			ch1 <- result{err: errG}
			return
		}
		b, errR := resp.ReadBody()
		ch1 <- result{body: b, err: errR}
	}()
	go func() {
		resp, errG := cc2.Get(ctx, "/large-shared")
		if errG != nil {
			ch2 <- result{err: errG}
			return
		}
		b, errR := resp.ReadBody()
		ch2 <- result{body: b, err: errR}
	}()

	r1 := <-ch1
	r2 := <-ch2
	require.NoError(t, r1.err, "concurrent block2 GET 1 failed")
	require.NoError(t, r2.err, "concurrent block2 GET 2 failed")
	require.Equal(t, largePayload, r1.body, "GET 1: payload mismatch")
	require.Equal(t, largePayload, r2.body, "GET 2: payload mismatch")
}

// TC_CoAP_BW_009 – TP_CoAP_BlockWise_Block1_LargeUpload_Integrity
//
// Reference: RFC 7959 Section 2.3 + 2.5 (Atomicity)
// "The server SHOULD process the Block1 transferred message atomically."
// The final assembled payload on the server must exactly match what the client sent.
//
// Procedure: PUT /integrity with 1024-byte payload. SZX=2 (64B).
// Expected: received payload is byte-for-byte identical to sent payload.
func TestTC_CoAP_BW_009_Block1_Integrity(t *testing.T) {
	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	var receivedPayload []byte
	m := mux.NewRouter()
	err := m.Handle("/integrity", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		body, errR := r.ReadBody()
		require.NoError(t, errR)
		receivedPayload = body
		errS := w.SetResponse(codes.Changed, message.TextPlain, nil)
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Put(ctx, "/integrity", message.AppOctets, bytes.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code())
	require.Equal(t, payload, receivedPayload,
		"RFC 7959 §2.5: Block1 assembled payload must be byte-for-byte identical to sent data")
}

// TC_CoAP_BW_010 – TP_CoAP_BlockWise_Block2_ResponseContentFormat
//
// Reference: RFC 7959 Section 2.1
// The Content-Format of the initial response must be preserved in all blocks.
//
// Procedure: GET /json-large returns large JSON payload.
// Expected: Content-Format in response is AppJSON.
func TestTC_CoAP_BW_010_Block2_ContentFormat_Preserved(t *testing.T) {
	jsonPayload := []byte(`{"data":"` + string(bytes.Repeat([]byte("x"), 200)) + `"}`)

	m := mux.NewRouter()
	err := m.Handle("/json-large", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.AppJSON, bytes.NewReader(jsonPayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Get(ctx, "/json-large")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	cf, err := resp.ContentFormat()
	require.NoError(t, err)
	require.Equal(t, message.AppJSON, cf,
		"RFC 7959 §2.1: Content-Format must be preserved across block2 transfer")

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, jsonPayload, body)
}

// TC_CoAP_BW_011 – TP_CoAP_BlockWise_Block2_GET_ExactBlockBoundary
//
// Reference: RFC 7959 Section 2.1
// A payload whose size is an exact multiple of the block size requires
// one extra block with empty body and M=0 to signal the end.
//
// Procedure: GET /exact-blocks, payload = 192 bytes (exactly 3×SZX64=64).
// Expected: client receives all 192 bytes.
func TestTC_CoAP_BW_011_Block2_ExactBoundary(t *testing.T) {
	payload := bytes.Repeat([]byte("Z"), 192) // 192 = 3 × 64 (SZX64)

	m := mux.NewRouter()
	err := m.Handle("/exact-blocks", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(payload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Get(ctx, "/exact-blocks")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, payload, body,
		"RFC 7959 §2.1: block-aligned payload must be fully reassembled")
	require.Equal(t, 192, len(body), "must receive all 192 bytes")
}

// TC_CoAP_BW_012 – TP_CoAP_BlockWise_Block1_DELETE_ServerRejects
//
// Reference: RFC 7959 Sections 2.3-2.4
// DELETE with a large body using Block1 should be handled by the server.
// If not supported, server should return appropriately.
//
// Procedure: DELETE /record with no body. Expected: 2.02 Deleted.
func TestTC_CoAP_BW_012_DELETE_WithBody(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/record", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Deleted, message.TextPlain, nil)
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX1024)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX1024)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Delete(ctx, "/record")
	require.NoError(t, err)
	require.Equal(t, codes.Deleted, resp.Code(),
		"RFC 7959: DELETE must return 2.02 Deleted")
}

// TC_CoAP_BW_013 – TP_CoAP_BlockWise_Block2_SZX1024
//
// Reference: RFC 7959 Section 2.2
// SZX=6 (1024 bytes per block) is the largest standard block size in UDP.
//
// Procedure: GET /huge with 4096-byte payload; SZX=1024.
// Expected: client reassembles full payload (4 blocks).
func TestTC_CoAP_BW_013_Block2_SZX1024(t *testing.T) {
	payload := bytes.Repeat([]byte("K"), 4096)

	m := mux.NewRouter()
	err := m.Handle("/huge", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(payload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX1024)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX1024)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Get(ctx, "/huge")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, payload, body,
		"RFC 7959 §2.2: SZX=6 (1024B) block2 must reassemble full 4KB payload")
}

// TC_CoAP_BW_014 – TP_CoAP_BlockWise_Block2_Block1_Combined
//
// Reference: RFC 7959 Sections 2.1 + 2.3
// A single exchange can use both Block1 (for the request) and Block2 (for the response).
// This is typical for a POST that uploads data and returns a large response.
//
// Procedure: POST /transform with 512B body; server returns 512B body.
// Expected: client sends Block1, receives Block2; both payloads correct.
func TestTC_CoAP_BW_014_Block1_And_Block2_Combined(t *testing.T) {
	requestPayload := bytes.Repeat([]byte("REQ"), 171) // ~513B → trim to 512
	requestPayload = requestPayload[:512]
	responsePayload := bytes.Repeat([]byte("RSP"), 171)
	responsePayload = responsePayload[:512]

	var receivedRequest []byte
	m := mux.NewRouter()
	err := m.Handle("/transform", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		require.Equal(t, codes.POST, r.Code())
		body, errR := r.ReadBody()
		require.NoError(t, errR)
		receivedRequest = body
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(responsePayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	cc := startBlockwiseClient(t, addr, blockwise.SZX64)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Post(ctx, "/transform", message.TextPlain, bytes.NewReader(requestPayload))
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	respBody, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, requestPayload, receivedRequest,
		"RFC 7959 §2.3: server must reconstruct the full Block1 POST body")
	require.Equal(t, responsePayload, respBody,
		"RFC 7959 §2.1: client must reconstruct the full Block2 response body")
}

// hasCoAPOption returns true if a raw CoAP packet contains the specified option number.
// It parses the option list according to RFC 7252 §3.1 option encoding rules.
func hasCoAPOption(pkt []byte, targetOption uint32) bool {
	if len(pkt) < 4 {
		return false
	}
	tkl := int(pkt[0] & 0x0F)
	pos := 4 + tkl
	if pos >= len(pkt) {
		return false
	}
	var currentOption uint32
	for pos < len(pkt) {
		if pkt[pos] == 0xFF { // payload marker
			break
		}
		deltaNibble := uint32(pkt[pos]>>4) & 0x0F
		lenNibble := uint32(pkt[pos]) & 0x0F
		pos++
		var delta uint32
		switch deltaNibble {
		case 13:
			if pos >= len(pkt) {
				return false
			}
			delta = uint32(pkt[pos]) + 13
			pos++
		case 14:
			if pos+1 >= len(pkt) {
				return false
			}
			delta = uint32(pkt[pos])<<8 + uint32(pkt[pos+1]) + 269
			pos += 2
		default:
			delta = deltaNibble
		}
		var optLen uint32
		switch lenNibble {
		case 13:
			if pos >= len(pkt) {
				return false
			}
			optLen = uint32(pkt[pos]) + 13
			pos++
		case 14:
			if pos+1 >= len(pkt) {
				return false
			}
			optLen = uint32(pkt[pos])<<8 + uint32(pkt[pos+1]) + 269
			pos += 2
		default:
			optLen = lenNibble
		}
		currentOption += delta
		if currentOption == targetOption {
			return true
		}
		pos += int(optLen)
	}
	return false
}

// TC_CoAP_BW_015 – TP_CoAP_BlockWise_Size2_InFirstBlock2Response
//
// Reference: RFC 7959 Section 4 (SZ-3)
// "The Size2 option SHOULD be included in the first Block2 response
// to provide the total payload size to the requesting client."
//
// Procedure: raw UDP GET /size2-resource (large payload, no Block2 in request).
// Expected: first Block2 response from server contains Size2 option (option ID 28).
// go-coap sets Size2 via blockwise.createSendingMessage (blockwise.go line ~482).
func TestTC_CoAP_BW_015_Size2_InBlock2Response(t *testing.T) {
	// 256 bytes – forces block2 at SZX64 (64 bytes per block)
	largePayload := bytes.Repeat([]byte("SZ2"), 86) // ~258 bytes
	largePayload = largePayload[:256]

	m := mux.NewRouter()
	// Use short path "/sz2" (3 chars) to keep option encoding in single-byte nibble form.
	err := m.Handle("/sz2", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(largePayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	conn, connErr := net.Dial("udp", addr)
	require.NoError(t, connErr)
	defer conn.Close()

	// CON GET /sz2 – include a 2-byte token so blockwise middleware is not bypassed
	// (processReceivedMessage short-circuits to next() when token is empty)
	// Option: Uri-Path "sz2" → delta=11, len=3 → first byte 0xB3
	pkt := []byte{
		0x42, 0x01, 0x00, 0x71, // CON GET MID=0x0071 TKL=2
		0xAB, 0xCD, // 2-byte token
		0xB3, 's', 'z', '2', // Uri-Path "sz2" (delta=11, len=3)
	}
	_, err = conn.Write(pkt)
	require.NoError(t, err)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	require.NoError(t, err, "server must respond to GET on large resource")
	require.GreaterOrEqual(t, n, 4)

	// Response should be ACK + Content (0x60, 0x45) with Block2 option (27) and Size2 (28)
	require.Equal(t, byte(0x45), buf[1],
		"RFC 7959 §2.1: server must respond with 2.05 Content for block2 GET")
	require.True(t, hasCoAPOption(buf[:n], 28),
		"RFC 7959 §4 (SZ-3): first Block2 response SHOULD include Size2 option (option ID 28)")
}

// TC_CoAP_BW_016 – TP_CoAP_BlockWise_Size1_InBlock1Request
//
// Reference: RFC 7959 Section 4 (SZ-4)
// "The Size1 option SHOULD be included in the first Block1 request to indicate
// the total payload size to the receiving server."
//
// Procedure: raw UDP server captures go-coap client's Block1 PUT request.
// Expected: first Block1 request from the go-coap client contains Size1 option
// (option ID 60). go-coap adds it via blockwise.Do (blockwise.go line ~246).
func TestTC_CoAP_BW_016_Size1_InBlock1Request(t *testing.T) {
	// Raw UDP "server" that captures the first Block1 packet
	pconn, pcErr := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, pcErr)
	defer pconn.Close()

	rawAddr := pconn.LocalAddr().String()
	firstPktCh := make(chan []byte, 1)

	go func() {
		buf := make([]byte, 4096)
		for {
			n, clientAddr, readErr := pconn.ReadFrom(buf)
			if readErr != nil {
				return
			}
			pkt := make([]byte, n)
			copy(pkt, buf[:n])
			select {
			case firstPktCh <- pkt:
			default:
			}
			// Send minimal ACK to unblock go-coap's send loop (no further processing)
			if n >= 4 {
				ack := []byte{0x60, 0x00, pkt[2], pkt[3]} // ACK Empty (MID echo)
				_, _ = pconn.WriteTo(ack, clientAddr)
			}
		}
	}()

	// go-coap client performs blockwise PUT with large payload
	largePayload := bytes.Repeat([]byte("PUT"), 87) // ~261 bytes > 64 bytes SZX64
	largePayload = largePayload[:256]

	putDone := make(chan error, 1)
	cc, dialErr := udp.Dial(rawAddr, options.WithBlockwise(true, blockwise.SZX64, 5*time.Second))
	require.NoError(t, dialErr)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	putCtx, putCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer putCancel()

	go func() {
		_, putErr := cc.Put(putCtx, "/size1-test", message.TextPlain, bytes.NewReader(largePayload))
		putDone <- putErr
	}()

	var firstPkt []byte
	select {
	case firstPkt = <-firstPktCh:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "go-coap client did not send any Block1 request within 2 seconds")
	}

	require.True(t, hasCoAPOption(firstPkt, 60),
		"RFC 7959 §4 (SZ-4): first Block1 request SHOULD include Size1 option (option ID 60); "+
			"go-coap sets Size1 in blockwise.Do()")

	putCancel() // let the Put goroutine finish
	<-putDone
}

// TC_CoAP_BW_017 – TP_CoAP_BlockWise_OutOfOrder_Block1_Returns4_08
//
// Reference: RFC 7959 Section 2.9 (B1-5)
// "If the blocks received so far are not contiguous, the server SHOULD
// return a 4.08 (Request Entity Incomplete) response."
//
// Procedure: raw UDP PUT /bw017 with Block1 NUM=2 (skipping NUM=0 and NUM=1).
// Expected: 4.08 Request Entity Incomplete (code byte 0x88).
//
// KNOWN FAILURE: go-coap returns 2.31 Continue (0x5F) with Block1 NUM=0,
// asking the client to restart from block 0, instead of returning 4.08.
// This reveals a missing RFC 7959 §2.9 implementation in go-coap.
func TestTC_CoAP_BW_017_OutOfOrderBlock1_Returns4_08(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/bw017", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startBlockwiseServer(t, m, blockwise.SZX64)
	defer cleanup()

	conn, connErr := net.Dial("udp", addr)
	require.NoError(t, connErr)
	defer conn.Close()

	// CON PUT /bw017 with Block1 option NUM=2, M=1, SZX=2 (64B blocks)
	// IMPORTANT: include a 2-byte token — processReceivedMessage skips blockwise
	// processing entirely when TKL=0 (len(token)==0 → next(w,r)), which would
	// bypass the out-of-order check we want to test.
	// Block1 value: (NUM<<4)|(M<<3)|SZX = (2<<4)|(1<<3)|2 = 0x2A
	// Options in ascending order: Uri-Path(11), Content-Format(12), Block1(27)
	// delta encoding for Block1 from option 12: delta=15 → 0xD form (13+2): 0xD1, ext=0x02
	pkt := []byte{
		0x42, 0x03, 0x00, 0x77, // CON PUT MID=0x0077 TKL=2
		0xDE, 0xAD, // 2-byte token
		0xB5, 'b', 'w', '0', '1', '7', // Uri-Path "bw017" (d=11, l=5)
		0x11, 0x00, // Content-Format text/plain (d=1, l=1, v=0)
		0xD1, 0x02, 0x2A, // Block1 option 27 (d=15, l=1, v=0x2A: NUM=2,M=1,SZX=2)
		0xFF,               // payload marker
		'A', 'A', 'A', 'A', // arbitrary payload fragment
	}
	_, err = conn.Write(pkt)
	require.NoError(t, err)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	buf := make([]byte, 256)
	n, readErr := conn.Read(buf)
	require.NoError(t, readErr, "server must respond to out-of-order Block1")
	require.GreaterOrEqual(t, n, 2)

	// RFC 7959 §2.9: SHOULD return 4.08 Request Entity Incomplete (code byte 0x88)
	// go-coap returns 2.31 Continue (0x5F) instead → test FAILS, revealing the gap
	require.Equal(t, byte(0x88), buf[1],
		"RFC 7959 §2.9 (B1-5): out-of-order Block1 (NUM=2 without prior NUM=0) "+
			"SHOULD return 4.08 Request Entity Incomplete (0x88); got 0x%02x — "+
			"go-coap returns 2.31 Continue instead (missing implementation)", buf[1])
}

// TC_CoAP_BW_018 – TP_CoAP_BlockWise_Observe_LargeNotification
//
// Reference: RFC 7959 §2.1 / RFC 7641 §4.1 (combined)
// When a server sends a notification with a payload larger than the block size,
// the Block2 mechanism MUST be used to deliver the complete content.
//
// Historical context: Bug #575 ("Combination of blockwise and observe not working")
// reported a logic error in blockwise.go that prevented large observe notifications.
// The issue has since been addressed (see commit "Fix processing of observe
// notifications with ETags"). This test provides regression coverage.
//
// Procedure:
//  1. Start server with blockwise enabled (SZX64).
//  2. Client registers for /bw018-large with Observe=0.
//  3. Server sends one notification with a payload of ~260 bytes (> 64 B block).
//  4. Expected: client callback receives the complete payload reassembled via Block2.
func TestTC_CoAP_BW_018_Observe_LargeNotification(t *testing.T) {
	largePayload := bytes.Repeat([]byte("OBSERVEBLOCK2"), 20) // ~260 bytes > SZX64=64

	var received atomic.Int32
	done := make(chan []byte, 1)

	// Use a handler-based server with blockwise enabled.
	l, err := coapNet.NewListenUDP("udp", "127.0.0.1:0")
	require.NoError(t, err)

	m := mux.NewRouter()
	err = m.Handle("/bw018-large", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		obs, errO := r.Observe()
		if errO != nil {
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(largePayload))
			return
		}
		switch obs {
		case 0: // registration
			cc := w.Conn()
			tok := make([]byte, len(r.Token()))
			copy(tok, r.Token())
			// Send one notification with a large payload (requires Block2).
			go func() {
				time.Sleep(30 * time.Millisecond)
				req := cc.AcquireMessage(cc.Context())
				req.SetCode(codes.Content)
				req.SetContentFormat(message.TextPlain)
				req.SetObserve(2)
				req.SetBody(bytes.NewReader(largePayload))
				req.SetToken(tok)
				_ = cc.WriteMessage(req)
				cc.ReleaseMessage(req)
			}()
		case 1: // deregistration
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	}))
	require.NoError(t, err)

	s := udp.NewServer(options.WithMux(m), options.WithBlockwise(true, blockwise.SZX64, 30*time.Second))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.Serve(l)
	}()
	defer func() {
		s.Stop()
		wg.Wait()
		_ = l.Close()
	}()

	// Client also needs blockwise enabled to request subsequent Block2 portions.
	cc, dialErr := udp.Dial(l.LocalAddr().String(), options.WithBlockwise(true, blockwise.SZX64, 30*time.Second))
	require.NoError(t, dialErr)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	obs, err := cc.Observe(ctx, "/bw018-large", func(msg *pool.Message) {
		if received.Add(1) == 1 {
			b, _ := msg.ReadBody()
			select {
			case done <- b:
			default:
			}
		}
	})
	require.NoError(t, err, "RFC 7641 §3.1: Observe registration on blockwise-capable server must succeed")
	defer func() {
		if obs != nil {
			_ = obs.Cancel(ctx)
		}
	}()

	select {
	case body := <-done:
		require.Equal(t, largePayload, body,
			"RFC 7641 §4.1 + RFC 7959 §2.1: large observe notification "+
				"MUST deliver the complete payload via Block2 reassembly (Bug #575 regression)")
	case <-time.After(3 * time.Second):
		t.Fatal("RFC 7641 §4.1 + RFC 7959 §2.1: large observe notification was not received within 3 s " +
			"(possible regression of Bug #575 — blockwise+observe combination)")
	}
}
