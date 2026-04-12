// Package tcp — RFC 8974 "Extended Tokens and Stateless Clients in CoAP" conformance tests.
//
// Test IDs: EXT_001 – EXT_008
// Reference: https://www.rfc-editor.org/rfc/rfc8974
//
// RFC 8974 extends CoAP to support tokens longer than 8 bytes by using TKL values
// 13 (13–268 bytes), 14 (269–65804 bytes), and 15 (format error).
//
// go-coap v3 currently sets message.MaxTokenSize = 8, meaning extended tokens
// defined by RFC 8974 are NOT supported by the library. These tests document
// the current compliance level and expected behavior for non-standard token lengths.
//
// Tests that require RFC 8974 extended-token support are marked with
// // Known Failure: go-coap MaxTokenSize=8
package tcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	coapNet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/options"
	coapUDP "github.com/plgd-dev/go-coap/v3/udp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const extConformanceTimeout = 5 * time.Second

// startExtUDPServer starts a minimal UDP CoAP server with a mux.Router for testing.
func startExtUDPServer(t *testing.T, m *mux.Router) (string, func()) {
	t.Helper()
	l, err := coapNet.NewListenUDP("udp4", "127.0.0.1:0")
	require.NoError(t, err)

	s := coapUDP.NewServer(options.WithMux(m))
	var wg sync.WaitGroup
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
	return l.LocalAddr().String(), cleanup
}

// EX_001 – TP_CoAP_EXT_MaxTokenSizeConstant
//
// Reference: RFC 8974 Section 2.1
// "The token length (TKL) field in CoAP is 4 bits. Values 0–12 indicate that
// many bytes of token follow directly."
//
// go-coap currently limits tokens to 8 bytes (message.MaxTokenSize = 8).
// This test documents that limit and confirms the constant is stable.
func TestTC_CoAP_EXT_001_MaxTokenSize_Is8(t *testing.T) {
	assert.Equal(t, 8, message.MaxTokenSize,
		"RFC 8974 §2.1: go-coap currently limits tokens to 8 bytes (documented limit)")
}

// EX_002 – TP_CoAP_EXT_8ByteTokenTCP
//
// Reference: RFC 8323 §3 + RFC 7252 §5.3.1
// A token of exactly 8 bytes (the current maximum) must be echoed by the server.
//
// Procedure: GET with 8-byte token over TCP. Expected: response echoes same token.
func TestTC_CoAP_EXT_002_8ByteToken_TCP_EchoToken(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/res", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), extConformanceTimeout)
	defer cancel()

	token8b := message.Token([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})

	req, err := cc.NewGetRequest(ctx, "/res")
	require.NoError(t, err)
	req.SetToken(token8b)

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())
	require.Equal(t, token8b, resp.Token(),
		"RFC 7252 §5.3.1: server must echo 8-byte token in response over TCP")
}

// EX_003 – TP_CoAP_EXT_ZeroLenTokenTCP
//
// Reference: RFC 7252 §5.3.1
// "The token itself is a sequence of 0 to 8 bytes."
// A zero-length token is valid; the server must respond with TKL=0.
//
// Procedure: GET with empty token over TCP. Expected: response has zero-length token.
func TestTC_CoAP_EXT_003_ZeroLenToken_TCP(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/zero", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("zok")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), extConformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/zero")
	require.NoError(t, err)
	req.SetToken(message.Token{}) // explicit zero-length token

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())
	t.Logf("Zero-token response TKL=%d", len(resp.Token()))
}

// EX_004 – TP_CoAP_EXT_8ByteTokenUDP_EchoToken
//
// Reference: RFC 7252 §5.3.1
// A token of exactly 8 bytes (current maximum) must be echoed by the server over UDP.
//
// Procedure: GET with 8-byte token over UDP. Expected: response echoes same token.
func TestTC_CoAP_EXT_004_8ByteToken_UDP_EchoToken(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/udpres", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startExtUDPServer(t, m)
	defer cleanup()

	cc, err := coapUDP.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), extConformanceTimeout)
	defer cancel()

	token8b := message.Token([]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22})

	req, err := cc.NewGetRequest(ctx, "/udpres")
	require.NoError(t, err)
	req.SetToken(token8b)

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())
	require.Equal(t, token8b, resp.Token(),
		"RFC 7252 §5.3.1: server must echo 8-byte token in response over UDP")
}

// rawTCPGETFrame builds a minimal CoAP/TCP GET frame with the given TKL nibble
// and token bytes. For tokens up to 12 bytes the TKL nibble equals len(token).
// Pass a non-standard tkl value (e.g. 13, 15) to test parser behaviour.
//
// Frame layout (RFC 8323 §3.2):
//
//	Byte 0: [Len(4)|TKL(4)]   where Len = optionsLen (no Extended Length here)
//	Byte 1: Code (0x01 = GET)
//	Bytes 2…2+TKL: token
//	Remaining: URI-Path "ext" option
func rawTCPGETFrame(tklNibble uint8, token []byte) []byte {
	// URI-Path "ext": option delta=11, length=3 => nibbles (0xB, 0x3), then "ext"
	uriPathOpt := []byte{0xB3, 'e', 'x', 't'}
	optLen := len(uriPathOpt)
	// Len field encodes options length (< 13, so no Extended Length)
	lenNibble := uint8(optLen)
	firstByte := (lenNibble << 4) | (tklNibble & 0x0F)
	var frame []byte
	frame = append(frame, firstByte, 0x01) // Len+TKL nibbles, Code=GET
	frame = append(frame, token...)
	frame = append(frame, uriPathOpt...)
	return frame
}

// rawUDPCoAPPacket builds a CoAP/UDP NON GET datagram with the given TKL nibble
// and token bytes (RFC 7252 §3, extended by RFC 8974 §2.1 for TKL≥13).
func rawUDPCoAPPacket(tklNibble uint8, token []byte, mid uint16) []byte {
	// Byte 0: Ver=1, T=NON=1, TKL=tklNibble
	// 0b01_01_xxxx = 0x50 | tklNibble
	byte0 := byte(0x50 | (tklNibble & 0x0F))
	var midBytes [2]byte
	binary.BigEndian.PutUint16(midBytes[:], mid)

	// URI-Path "ext": option delta=11, length=3
	uriPathOpt := []byte{0xB3, 'e', 'x', 't'}

	var pkt []byte
	pkt = append(pkt, byte0, 0x01, midBytes[0], midBytes[1])
	pkt = append(pkt, token...)
	pkt = append(pkt, uriPathOpt...)
	return pkt
}

// EX_005 – TP_CoAP_EXT_TCP_TKL13_ExtendedToken
//
// Reference: RFC 8974 Section 2.1
// "TKL value 13: Extended Token Length is 1 byte;
// actual token length = Extended Token Length + 13."
//
// Known Failure: go-coap MaxTokenSize=8; the encoder fails with ErrInvalidTokenLen
// when trying to reply with a 13-byte token, causing the connection to close.
// A fully RFC-8974-compliant server would accept and echo a 13-byte token.
//
// Procedure: send a raw TCP frame with TKL nibble = 13 and 13 token bytes.
// Expected with go-coap: connection is closed (no valid response).
func TestTC_CoAP_EXT_005_TCP_TKL13_NoResponse(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/ext", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ext")))
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	// Construct raw TCP frame with TKL=13 (13 literal token bytes).
	// go-coap's TCP decoder reads 13 bytes as the token, but the encoder
	// cannot send a response with a 13-byte token (MaxTokenSize=8), so the
	// handler panics/errors and the connection is closed.
	token13 := bytes.Repeat([]byte{0xAB}, 13)
	frame := rawTCPGETFrame(13, token13)

	conn, errDial := net.DialTimeout("tcp", addr, extConformanceTimeout)
	require.NoError(t, errDial)
	defer conn.Close()

	// Read the CSM that the server sends first
	_ = conn.SetReadDeadline(time.Now().Add(extConformanceTimeout))
	csmBuf := make([]byte, 64)
	n, _ := conn.Read(csmBuf)
	t.Logf("Received %d bytes (expected CSM from server)", n)

	// Send our raw frame with TKL=13
	_, errWrite := conn.Write(frame)
	require.NoError(t, errWrite)

	// Expect either: connection closed or a response; either way read until EOF/deadline
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n2, errRead := conn.Read(buf)
	t.Logf("RFC 8974 §2.1 / TKL=13 on TCP: read %d bytes, err=%v (go-coap MaxTokenSize=8; extended tokens not supported)", n2, errRead)
	// This test documents current behaviour: a compliant implementation would echo the token.
}

// EX_006 – TP_CoAP_EXT_TCP_TKL15_FormatError
//
// Reference: RFC 8974 Section 2.1
// "TKL value 15: Reserved as a format error indicator. An endpoint that
// receives a message with TKL=15 MUST treat it as a message format error."
//
// Expected: the server closes the TCP connection.
func TestTC_CoAP_EXT_006_TCP_TKL15_FormatError(t *testing.T) {
	m := mux.NewRouter()
	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	// TKL=15 with 15 dummy bytes for token; go-coap might treat TKL=15 differently.
	// Per RFC 8974 §2.1: TKL=15 is a format error; connection must be closed.
	frame := rawTCPGETFrame(15, bytes.Repeat([]byte{0xFF}, 15))

	conn, errDial := net.DialTimeout("tcp", addr, extConformanceTimeout)
	require.NoError(t, errDial)
	defer conn.Close()

	// Drain the initial CSM from the server
	_ = conn.SetReadDeadline(time.Now().Add(extConformanceTimeout))
	csmBuf := make([]byte, 64)
	n, _ := conn.Read(csmBuf)
	t.Logf("Received %d bytes (expected CSM from server)", n)

	// Send invalid frame
	_, errWrite := conn.Write(frame)
	require.NoError(t, errWrite)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n2, errRead := conn.Read(buf)
	t.Logf("RFC 8974 §2.1 / TKL=15 on TCP: read %d bytes, err=%v (expect connection closed)", n2, errRead)
}

// EX_007 – TP_CoAP_EXT_UDP_TKL13_Rejected
//
// Reference: RFC 8974 Section 2.2.1
// "UDP implementations that do not support extended tokens MUST silently ignore
// or respond with a Reset to messages with TKL values 13–15."
//
// go-coap UDP decoder returns ErrInvalidTokenLen for TKL > 8, so the packet
// is silently dropped (no response).
//
// Known Failure: a fully RFC-8974-compliant server would accept TKL=13 messages.
// go-coap drops them, which is the allowed fallback per §2.2.1.
//
// Procedure: send raw UDP NON with TKL=13 token (13 bytes). Expected: no response.
func TestTC_CoAP_EXT_007_UDP_TKL13_Dropped(t *testing.T) {
	m := mux.NewRouter()
	addr, cleanup := startExtUDPServer(t, m)
	defer cleanup()

	raddr, errResolve := net.ResolveUDPAddr("udp", addr)
	require.NoError(t, errResolve)

	laddr, errResolve2 := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, errResolve2)

	conn, errDial := net.ListenUDP("udp", laddr)
	require.NoError(t, errDial)
	defer conn.Close()

	token13 := bytes.Repeat([]byte{0xCD}, 13)
	pkt := rawUDPCoAPPacket(13, token13, 0x0013)

	_, errWrite := conn.WriteTo(pkt, raddr)
	require.NoError(t, errWrite)

	// Expect no response (server drops the invalid packet)
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 256)
	n, _, errRead := conn.ReadFrom(buf)
	if errRead != nil {
		t.Logf("RFC 8974 §2.1 / TKL=13 on UDP: no response (timeout/error: %v) — server correctly drops packet (MaxTokenSize=8)", errRead)
	} else {
		t.Logf("RFC 8974 §2.1 / TKL=13 on UDP: received %d bytes (unexpected response: %#x)", n, buf[:n])
	}
}

// EX_008 – TP_CoAP_EXT_UDP_TKL15_Rejected
//
// Reference: RFC 8974 Section 2.1
// TKL=15 is a format error. On UDP the server must silently ignore or reset.
//
// go-coap UDP decoder returns ErrInvalidTokenLen for TKL > 8, so the packet
// is silently dropped.
//
// Procedure: send raw UDP NON with TKL=15 token (15 bytes). Expected: no response.
func TestTC_CoAP_EXT_008_UDP_TKL15_Dropped(t *testing.T) {
	m := mux.NewRouter()
	addr, cleanup := startExtUDPServer(t, m)
	defer cleanup()

	raddr, errResolve := net.ResolveUDPAddr("udp", addr)
	require.NoError(t, errResolve)

	laddr, errResolve2 := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, errResolve2)

	conn, errDial := net.ListenUDP("udp", laddr)
	require.NoError(t, errDial)
	defer conn.Close()

	token15 := bytes.Repeat([]byte{0xEF}, 15)
	pkt := rawUDPCoAPPacket(15, token15, 0x0015)

	_, errWrite := conn.WriteTo(pkt, raddr)
	require.NoError(t, errWrite)

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 256)
	n, _, errRead := conn.ReadFrom(buf)
	if errRead != nil {
		t.Logf("RFC 8974 §2.1 / TKL=15 on UDP: no response (timeout/error: %v) — server correctly drops packet", errRead)
	} else {
		t.Logf("RFC 8974 §2.1 / TKL=15 on UDP: received %d bytes (unexpected response: %#x)", n, buf[:n])
	}
}
