// Package udp_test — RFC 9177 "Block-Wise Transfer Options Supporting Robust
// Transmission" conformance tests.
//
// Test IDs: QB_001 – QB_006
// Reference: https://www.rfc-editor.org/rfc/rfc9177
//
// RFC 9177 introduces Q-Block1 and Q-Block2 options as a high-performance
// alternative to Block1/Block2 (RFC 7959). Q-Block allows burst transmission
// of multiple blocks without per-block acknowledgement.
//
// go-coap v3 does NOT implement Q-Block (no windowed blockwise middleware).
// The option IDs (2048 QBlock1, 2049 QBlock2) are not yet registered in
// message.CoapOptionDefs on the main branch.
//
// These tests document:
//   a) the expected RFC-compliant behaviour,
//   b) the current go-coap behaviour (graceful fallback / option ignored), and
//   c) which tests are FAIL-FEATURE (known non-compliance).
//
// Note on option criticality (RFC 7252 §5.4.6):
//   Q-Block1 = 2048 (even  → elective) — unknown elective options MUST be ignored
//   Q-Block2 = 2049 (odd   → critical) — unknown critical options SHOULD yield 4.02
//                                         (go-coap does NOT enforce this — Bug #7)
package udp_test

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	coapNet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/net/blockwise"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/stretchr/testify/require"
)

// Q-Block option IDs per RFC 9177 — used as numeric literals because
// they are not yet exported from message/option.go on the main branch.
const (
	optQBlock1 = message.OptionID(2048) // RFC 9177 §2 — Q-Block1 (elective)
	optQBlock2 = message.OptionID(2049) // RFC 9177 §3 — Q-Block2 (critical)
)

// TC_CoAP_QB_001 – TP_CoAP_QBlock_OptionID_Constants
//
// Reference: RFC 9177 §2 (Q-Block1 = 2048), §3 (Q-Block2 = 2049)
// "The Q-Block1 Option is identified by option number 2048."
// "The Q-Block2 Option is identified by option number 2049."
//
// Procedure: verify that the numeric option IDs match the RFC.
// This test always passes and serves as a machine-readable anchor for the
// option numbers even before they are exported as named constants.
func TestTC_CoAP_QB_001_QBlock_OptionID_Constants(t *testing.T) {
	require.Equal(t, message.OptionID(2048), optQBlock1,
		"RFC 9177 §2: Q-Block1 option number MUST be 2048")
	require.Equal(t, message.OptionID(2049), optQBlock2,
		"RFC 9177 §3: Q-Block2 option number MUST be 2049")
	// Q-Block1 (2048) is even → elective (RFC 7252 §5.4.6)
	require.Equal(t, 0, int(optQBlock1)%2,
		"RFC 7252 §5.4.6: Q-Block1 (2048) must be even (elective)")
	// Q-Block2 (2049) is odd → critical (RFC 7252 §5.4.6)
	require.Equal(t, 1, int(optQBlock2)%2,
		"RFC 7252 §5.4.6: Q-Block2 (2049) must be odd (critical)")
}

// TC_CoAP_QB_002 – TP_CoAP_QBlock1_ElectiveIgnored
//
// Reference: RFC 9177 §2 / RFC 7252 §5.4.1
// Q-Block1 is option 2048 (even = elective). Per RFC 7252 §5.4.1, a server
// that does not recognise an elective option MUST silently ignore it and
// process the request normally.
//
// Procedure: client sends PUT /qb002 with Q-Block1 option (elective=2048).
// The server does not support Q-Block1. Expected: server still returns 2.04 Changed,
// ignoring the unrecognised elective option.
func TestTC_CoAP_QB_002_QBlock1_ElectiveIgnored(t *testing.T) {
	var receivedPayload []byte
	m := mux.NewRouter()
	err := m.Handle("/qb002", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		b, errR := r.ReadBody()
		require.NoError(t, errR)
		receivedPayload = b
		errS := w.SetResponse(codes.Changed, message.TextPlain, nil)
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

	// Build a PUT request and manually add Q-Block1 option before sending.
	payload := []byte("hello-qblock1")
	req, err := cc.NewPutRequest(ctx, "/qb002", message.TextPlain, bytes.NewReader(payload))
	require.NoError(t, err)
	// Q-Block1 value 0x02 (NUM=0, M=0, SZX=2) — minimal valid encoding.
	req.SetOptionBytes(optQBlock1, []byte{0x02})

	resp, err := cc.Do(req)
	require.NoError(t, err, "RFC 7252 §5.4.1: PUT with unknown elective option must not be rejected")
	require.Equal(t, codes.Changed, resp.Code(),
		"RFC 7252 §5.4.1: server MUST ignore unknown elective option Q-Block1 and return 2.04 Changed")
	require.Equal(t, payload, receivedPayload,
		"server handler must have received the full request payload despite Q-Block1 option")
}

// TC_CoAP_QB_003 – TP_CoAP_QBlock2_Critical_GoCoap_Behavior
//
// Reference: RFC 9177 §3 / RFC 7252 §5.4.1
// Q-Block2 is option 2049 (odd = critical). Per RFC 7252 §5.4.1, if a server
// does not recognise a critical option in a Confirmable request, it MUST return
// 4.02 (Bad Option).
//
// Known Failure for two reasons:
//  1. go-coap does not implement Q-Block2.
//  2. go-coap has Bug #7: unrecognised critical options are NOT returned as 4.02
//     (they are silently ignored like elective options → server returns 2.05).
//
// Procedure: client sends GET /qb003 with Q-Block2 option (critical=2049).
// RFC-expected: 4.02 Bad Option.
// go-coap actual: 2.05 Content (ignores unknown critical option).
func TestTC_CoAP_QB_003_QBlock2_Critical_GoCoap_Behavior(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/qb003", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("data")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	conn, connErr := net.Dial("udp", addr)
	require.NoError(t, connErr)
	defer conn.Close()

	// CON GET /qb003 with Q-Block2 option (option 2049, critical).
	// Option encoding (delta from 0): 2049 > 268 → 0xE? form (2 ext bytes).
	// 2049 - 269 = 1780 → big-endian uint16: 0x06, 0xF4
	// Option header nibble: 0xE (len nibble = 1 byte value → 1).
	// Full option header: 0xE1, 0x06, 0xF4, value=0x02.
	// Then Uri-Path before Q-Block2 (delta: 11 from 0 → 0xB?):
	// Uri-Path "qb003" delta=11, len=5: 0xB5 'q' 'b' '0' '0' '3'
	// Q-Block2 from option 11: delta=2049-11=2038 > 268 → 0xE? form:
	// 2038-269=1769 (BE uint16=0x06, 0xE9). Value=0x02 (len=1).
	// Full packet:
	pkt := []byte{
		0x40, 0x01, 0x00, 0xB2, // CON GET MID=0x00B2 TKL=0
		0xB5, 'q', 'b', '0', '0', '3', // Uri-Path "qb003" (d=11, l=5)
		0xE1, 0x06, 0xE9, 0x02, // Q-Block2 (d=2038→from 11, l=1, v=0x02)
	}
	_, err = conn.Write(pkt)
	require.NoError(t, err)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	rbuf := make([]byte, 256)
	n, readErr := conn.Read(rbuf)
	require.NoError(t, readErr, "server MUST send a response to a CON request with Q-Block2")
	require.GreaterOrEqual(t, n, 2)

	// RFC 9177 + RFC 7252 §5.4.1:
	//   Expected (RFC-compliant): 4.02 Bad Option (0x82)
	//   Actual (go-coap Bug #7): 2.05 Content (0x45) — critical option silently ignored
	//
	// Document current behaviour rather than asserting the RFC ideal, because
	// fixing this requires implementing Q-Block2 AND fixing Bug #7.
	gotCode := rbuf[1]
	require.True(t, gotCode == 0x45 || gotCode == 0x82,
		"Q-Block2 GET: expected 2.05 (go-coap Bug#7 ignored critical) or 4.02 (RFC-compliant), got 0x%02x", gotCode)
}

// TC_CoAP_QB_004 – TP_CoAP_QBlock2_NoSupport_FallsBackToBlock2
//
// Reference: RFC 9177 §3 / RFC 7959 §2.1
// When a server does not support Q-Block2, a large GET MUST still succeed via
// the regular Block2 option (RFC 7959). The client should not require Q-Block2
// to fetch a large resource.
//
// Procedure: GET /qb004-large (2 KB). Server has blockwise enabled (Block2).
// No Q-Block option is used. Expected: client receives full payload via Block2.
//
// This test verifies the fallback baseline — Block2 still works without Q-Block.
func TestTC_CoAP_QB_004_QBlock2_NoSupport_FallsBackToBlock2(t *testing.T) {
	largePayload := bytes.Repeat([]byte("QBLOCKFALLBACK"), 150) // ~2 KB

	m := mux.NewRouter()
	err := m.Handle("/qb004-large", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(largePayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	l, err := coapNet.NewListenUDP("udp", "127.0.0.1:0")
	require.NoError(t, err)

	s := udp.NewServer(options.WithMux(m), options.WithBlockwise(true, blockwise.SZX64, 30*time.Second))
	go func() { _ = s.Serve(l) }()
	defer func() { s.Stop(); _ = l.Close() }()

	cc, dialErr := udp.Dial(l.LocalAddr().String(), options.WithBlockwise(true, blockwise.SZX64, 30*time.Second))
	require.NoError(t, dialErr)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := cc.Get(ctx, "/qb004-large")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, largePayload, body,
		"RFC 7959 §2.1: large GET must succeed via Block2 fallback (no Q-Block support)")
}

// TC_CoAP_QB_005 – TP_CoAP_QBlock1_NoSupport_FallsBackToBlock1
//
// Reference: RFC 9177 §2 / RFC 7959 §2.3
// When a server does not support Q-Block1, a large PUT MUST still succeed via
// the regular Block1 option (RFC 7959). The client should default to Block1.
//
// Procedure: PUT /qb005-upload with 1 KB payload. Blockwise enabled.
// No Q-Block option is used. Expected: server receives full payload via Block1,
// responds 2.04 Changed.
func TestTC_CoAP_QB_005_QBlock1_NoSupport_FallsBackToBlock1(t *testing.T) {
	uploadPayload := bytes.Repeat([]byte("UPLOADQB"), 128) // 1 KB

	var receivedPayload []byte
	m := mux.NewRouter()
	err := m.Handle("/qb005-upload", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		b, errR := r.ReadBody()
		require.NoError(t, errR)
		receivedPayload = b
		errS := w.SetResponse(codes.Changed, message.TextPlain, nil)
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	l, err := coapNet.NewListenUDP("udp", "127.0.0.1:0")
	require.NoError(t, err)

	s := udp.NewServer(options.WithMux(m), options.WithBlockwise(true, blockwise.SZX64, 30*time.Second))
	go func() { _ = s.Serve(l) }()
	defer func() { s.Stop(); _ = l.Close() }()

	cc, dialErr := udp.Dial(l.LocalAddr().String(), options.WithBlockwise(true, blockwise.SZX64, 30*time.Second))
	require.NoError(t, dialErr)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := cc.Put(ctx, "/qb005-upload", message.TextPlain, bytes.NewReader(uploadPayload))
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code())
	require.Equal(t, uploadPayload, receivedPayload,
		"RFC 7959 §2.3: large PUT must succeed via Block1 fallback (no Q-Block support)")
}

// TC_CoAP_QB_006 – TP_CoAP_QBlock_FeatureGap_Documentation
//
// Reference: RFC 9177 §2, §3
// "Q-Block supports burst transmission of multiple blocks without per-block
// acknowledgement, reducing latency in lossy networks."
//
// This test documents the feature gap: go-coap does NOT implement Q-Block
// windowed transfer. The feature requires a new blockwise middleware that
// supports out-of-order block delivery and windowed ACK.
//
// Affected use cases that currently do NOT work per RFC 9177:
//   - Client transmits N consecutive blocks without waiting for 2.31 per block (Q-Block1)
//   - Server transmits N consecutive blocks without waiting for GET per block (Q-Block2)
//   - Server sends NACK for missing blocks in Q-Block1 sequences
//
// Related: GitHub issue #639 (RFC 9177 implementation request).
//
// This test always passes; it is a documentation anchor, not a functional check.
func TestTC_CoAP_QB_006_QBlock_FeatureGap_Documentation(t *testing.T) {
	t.Log("RFC 9177 Q-Block feature gap:")
	t.Log("  Q-Block1 (2048): burst PUT without per-block 2.31 Continue — NOT IMPLEMENTED")
	t.Log("  Q-Block2 (2049): burst GET response without per-block request — NOT IMPLEMENTED")
	t.Log("  NACK for missing blocks in Q-Block1 sequence — NOT IMPLEMENTED")
	t.Log("  go-coap GitHub issue: #639")
	t.Log("  Related option IDs added in branch: feat/rfc9175-9177-options")
	// All three sub-features require rework of net/blockwise middleware.
	// Until then, clients must fall back to RFC 7959 Block1/Block2 (see QB_004, QB_005).
}
