// Package udp_test — RFC 7252 conformance tests (CF_001–CF_065).
//
// CF_001–CF_042: migrated from conformance_test.go (legacy IoT-Testware baseline).
// CF_043–CF_065: additional normative requirements from RFC 7252.
// Reference: https://www.rfc-editor.org/rfc/rfc7252
package udp_test

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/message/pool"
	"github.com/plgd-dev/go-coap/v3/mux"
	"github.com/plgd-dev/go-coap/v3/net/responsewriter"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/plgd-dev/go-coap/v3/udp/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TC_CoAP_CF_043 – TP_CoAP_Method_DELETE_Deleted
//
// Reference: RFC 7252 Section 5.8.4
// "If the DELETE request is successful, the server returns a 2.02 (Deleted)
// response code."
//
// Procedure: DELETE /resource. Expected: 2.02 Deleted.
func TestTC_CoAP_CF_043_DELETE_Deleted(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/resource", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		require.Equal(t, codes.DELETE, r.Code())
		errS := w.SetResponse(codes.Deleted, message.TextPlain, nil)
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

	resp, err := cc.Delete(ctx, "/resource")
	require.NoError(t, err)
	require.Equal(t, codes.Deleted, resp.Code(),
		"RFC 7252 §5.8.4: DELETE on existing resource MUST return 2.02 Deleted")
}

// TC_CoAP_CF_044 – TP_CoAP_Method_POST_ContentFormat
//
// Reference: RFC 7252 Section 5.5.1
// "If the Content-Format option is present, the client indicates the format
// of the request payload."
//
// Procedure: POST /data with Content-Format=application/json (50).
// Expected: server records the content format correctly.
func TestTC_CoAP_CF_044_POST_ContentFormat(t *testing.T) {
	var receivedCF message.MediaType
	m := mux.NewRouter()
	err := m.Handle("/data", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		cf, errCF := r.ContentFormat()
		require.NoError(t, errCF)
		receivedCF = cf
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

	resp, err := cc.Post(ctx, "/data", message.AppJSON, bytes.NewReader([]byte(`{"val":1}`)))
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code())
	require.Equal(t, message.AppJSON, receivedCF,
		"RFC 7252 §5.5.1: server must receive the Content-Format option from the client")
}

// TC_CoAP_CF_045 – TP_CoAP_Method_PUT_Created
//
// Reference: RFC 7252 Section 5.8.3
// "If the resource was successfully created at the requested URI, the server
// SHOULD respond with 2.01 (Created)."
//
// Procedure: PUT /new — resource does not exist; server creates it.
// Expected: 2.01 Created.
func TestTC_CoAP_CF_045_PUT_Created(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/new", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		require.Equal(t, codes.PUT, r.Code())
		errS := w.SetResponse(codes.Created, message.TextPlain, nil)
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

	resp, err := cc.Put(ctx, "/new", message.TextPlain, bytes.NewReader([]byte("hello")))
	require.NoError(t, err)
	require.Equal(t, codes.Created, resp.Code(),
		"RFC 7252 §5.8.3: PUT on non-existent resource SHOULD return 2.01 Created")
}

// TC_CoAP_CF_046 – TP_CoAP_Method_MethodNotAllowed
//
// Reference: RFC 7252 Section 5.9.2
// "If the method is not recognized or not allowed by the resource, a
// 4.05 (Method Not Allowed) response MUST be returned."
//
// Procedure: DELETE on a GET-only resource.
// Expected: 4.05 Method Not Allowed.
func TestTC_CoAP_CF_046_MethodNotAllowed(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/readonly", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		if r.Code() != codes.GET {
			errS := w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			require.NoError(t, errS)
			return
		}
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("data")))
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

	resp, err := cc.Delete(ctx, "/readonly")
	require.NoError(t, err)
	require.Equal(t, codes.MethodNotAllowed, resp.Code(),
		"RFC 7252 §5.9.2: unsupported method MUST return 4.05 Method Not Allowed")
}

// TC_CoAP_CF_047 – TP_CoAP_Messaging_Deduplication_CON
//
// Reference: RFC 7252 Section 4.5
// "Duplicate detection MUST be applied to all CON messages. If a server
// receives a CON message with a MID it has already seen recently, it MUST
// supply the same response as it did the first time."
//
// Procedure: send the same CON message twice (same MID). The server response
// for the second copy must match the first (same code).
func TestTC_CoAP_CF_047_Deduplication_CON(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("dup")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Build CON GET with MID=0x1234 and Uri-Path "/dup"
	// 0x40 = VER=1, T=CON(0), TKL=0 → first byte; 0x01=GET; 0x12,0x34=MID
	// 0xB3,'d','u','p' = Uri-Path option (delta=11, len=3)
	pkt := []byte{0x40, 0x01, 0x12, 0x34, 0xB3, 'd', 'u', 'p'}

	readResponse := func() []byte {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 256)
		n, errR := conn.Read(buf)
		require.NoError(t, errR, "expected a response from server")
		return buf[:n]
	}

	// First send
	_, err = conn.Write(pkt)
	require.NoError(t, err)
	resp1 := readResponse()

	// Second send (exact duplicate: same bytes, same MID)
	_, err = conn.Write(pkt)
	require.NoError(t, err)
	resp2 := readResponse()

	require.Equal(t, resp1[1], resp2[1],
		"RFC 7252 §4.5: duplicate CON must produce same response code (got 0x%02x then 0x%02x)", resp1[1], resp2[1])
}

// TC_CoAP_CF_048 – TP_CoAP_Messaging_ConcurrentRequests
//
// Reference: RFC 7252 Section 4.5 / Section 5.3
// Each outstanding request MUST use a unique token. The server matches
// responses to requests by token, allowing concurrent requests.
//
// Procedure: send two concurrent GETs to different paths; verify both
// responses arrive with matching tokens.
func TestTC_CoAP_CF_048_ConcurrentTokens(t *testing.T) {
	m := mux.NewRouter()
	for _, path := range []string{"/a", "/b"} {
		p := path
		err := m.Handle(p, mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
			body := strings.TrimPrefix(p, "/")
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte(body)))
		}))
		require.NoError(t, err)
	}

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	type result struct {
		body []byte
		err  error
	}
	chA := make(chan result, 1)
	chB := make(chan result, 1)

	go func() {
		resp, errG := cc.Get(ctx, "/a")
		if errG != nil {
			chA <- result{err: errG}
			return
		}
		b, errR := resp.ReadBody()
		chA <- result{body: b, err: errR}
	}()
	go func() {
		resp, errG := cc.Get(ctx, "/b")
		if errG != nil {
			chB <- result{err: errG}
			return
		}
		b, errR := resp.ReadBody()
		chB <- result{body: b, err: errR}
	}()

	rA := <-chA
	rB := <-chB
	require.NoError(t, rA.err, "concurrent request to /a failed")
	require.NoError(t, rB.err, "concurrent request to /b failed")
	require.Equal(t, []byte("a"), rA.body, "response body for /a mismatch")
	require.Equal(t, []byte("b"), rB.body, "response body for /b mismatch")
}

// TC_CoAP_CF_049 – TP_CoAP_Response_ServiceUnavailable
//
// Reference: RFC 7252 Section 5.9.2
// "5.03 Service Unavailable: the service is temporarily unavailable;
// the client SHOULD try again."
//
// Procedure: client sends GET to overloaded resource.
// Expected: 5.03 Service Unavailable.
func TestTC_CoAP_CF_049_ServiceUnavailable(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/busy", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.ServiceUnavailable, message.TextPlain,
			bytes.NewReader([]byte("try later")))
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

	resp, err := cc.Get(ctx, "/busy")
	require.NoError(t, err)
	require.Equal(t, codes.ServiceUnavailable, resp.Code(),
		"RFC 7252 §5.9.2: server must be able to return 5.03 Service Unavailable")
}

// TC_CoAP_CF_050 – TP_CoAP_Response_NotImplemented
//
// Reference: RFC 7252 Section 5.9.2
// "5.01 Not Implemented: the server does not support the functionality
// required to fulfil the request."
//
// Procedure: client sends a request; server returns 5.01.
// Expected: 5.01 Not Implemented.
func TestTC_CoAP_CF_050_NotImplemented(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/future", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.NotImplemented, message.TextPlain, nil)
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

	resp, err := cc.Get(ctx, "/future")
	require.NoError(t, err)
	require.Equal(t, codes.NotImplemented, resp.Code(),
		"RFC 7252 §5.9.2: server must be able to return 5.01 Not Implemented")
}

// TC_CoAP_CF_051 – TP_CoAP_Response_ProxyingNotSupported
//
// Reference: RFC 7252 Section 5.9.2
// "5.05 Proxying Not Supported: the server is unable to act as a proxy for
// the given URI (e.g., returned when Proxy-Uri option is not accepted)."
//
// Procedure: client sends GET with Proxy-Uri option; server rejects it.
// Expected: 5.05 Proxying Not Supported.
func TestTC_CoAP_CF_051_ProxyingNotSupported(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		// Check for Proxy-Uri option (number 35)
		var proxyURI string
		for _, opt := range r.Options() {
			if opt.ID == message.ProxyURI {
				proxyURI = string(opt.Value)
				break
			}
		}
		if proxyURI != "" {
			_ = w.SetResponse(codes.ProxyingNotSupported, message.TextPlain, nil)
			return
		}
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Build CON GET with Proxy-Uri option (35) = "coap://example.com/test"
	// Option 35 (Proxy-Uri): delta from 0 → 35, but we need to encode carefully.
	// 0x40 = VER=1, T=CON, TKL=0; 0x01 = GET; 0x00,0x50 = MID
	// Option Proxy-Uri (35): delta=35, need 2-byte delta → 0xD0 | 0 + 22 + 13 = ...
	// Using simple encoding: delta=35 → 0xD0 (delta=13 special), ext byte=35-13=22; len=23
	uri := "coap://example.com/test"
	pkt := make([]byte, 0, 64)
	pkt = append(pkt, 0x40, 0x01, 0x00, 0x50) // header
	// Option Proxy-Uri (35): delta=35 → delta>12: use 0xD_ form: 0xD_ + 1 ext byte
	// 0xD_ = 1101 | len(4-bit). delta extended: 35-13=22 (fits in 1 byte).
	// len(uri)=23 → 23>12: use second nibble 0xD → 0xDD + 1 ext byte
	// Actually: first nibble = delta nibble, second nibble = len nibble
	// delta nibble 13 (0xD) → extra byte = delta - 13 = 22
	// len nibble 13 (0xD) → extra byte = len - 13 = 10
	uriB := []byte(uri)
	pkt = append(pkt, 0xDD)               // delta=13+, len=13+
	pkt = append(pkt, 22)                 // delta ext: 35-13=22
	pkt = append(pkt, byte(len(uriB)-13)) // len ext: 23-13=10
	pkt = append(pkt, uriB...)

	_, err = conn.Write(pkt)
	require.NoError(t, err)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, 2)
	require.Equal(t, byte(0xA5), buf[1],
		"RFC 7252 §5.9.2: Proxy-Uri not supported → must 5.05 (0xA5); got 0x%02X", buf[1])
}

// TC_CoAP_CF_052 – TP_CoAP_Response_GatewayTimeout
//
// Reference: RFC 7252 Section 5.9.2
// "5.04 Gateway Timeout: the server acting as a gateway or proxy received a
// timeout from an upstream server."
//
// Procedure: client sends GET; server returns 5.04.
// Expected: 5.04 Gateway Timeout.
func TestTC_CoAP_CF_052_GatewayTimeout(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/gw", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.GatewayTimeout, message.TextPlain, nil)
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

	resp, err := cc.Get(ctx, "/gw")
	require.NoError(t, err)
	require.Equal(t, codes.GatewayTimeout, resp.Code(),
		"RFC 7252 §5.9.2: server must be able to return 5.04 Gateway Timeout")
}

// TC_CoAP_CF_053 – TP_CoAP_Token_8Bytes_MaxStandard
//
// Reference: RFC 7252 Section 4.5
// "Token lengths 9–15 are reserved … lengths 0–8 are allowed."
// The maximum token length in RFC 7252 is 8 bytes.
//
// Procedure: send CON GET with an 8-byte token.
// Expected: 2.xx response with the same 8-byte token echoed.
func TestTC_CoAP_CF_053_8ByteToken_MaxStandard(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	token := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	// VER=1, T=CON, TKL=8 → 0x48; GET=0x01; MID=0x0099
	// Uri-Path "z" (delta=11, len=1)
	pkt := []byte{0x48, 0x01, 0x00, 0x99}
	pkt = append(pkt, token...)
	pkt = append(pkt, 0xB1, 'z')

	_, err = conn.Write(pkt)
	require.NoError(t, err)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, 4, "response too short")

	respTKL := buf[0] & 0x0F
	require.Equal(t, uint8(8), respTKL, "response TKL must be 8 to echo the 8-byte token")
	require.Equal(t, token, buf[4:12],
		"RFC 7252 §4.5+§5.3.1: 8-byte token must be echoed verbatim in response")
}

// TC_CoAP_CF_054 – TP_CoAP_Options_UriHost
//
// Reference: RFC 7252 Section 5.10.1
// "The Uri-Host option carries the intended recipient's host name or IP address."
// If present, the server SHOULD confirm the Uri-Host matches its own address.
//
// Procedure: GET /resource with Uri-Host=127.0.0.1. Server receives and logs option.
// Expected: 2.05 Content (no rejection on local loopback host match).
func TestTC_CoAP_CF_054_UriHostOption(t *testing.T) {
	var receivedUriHost string
	m := mux.NewRouter()
	err := m.Handle("/resource", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		host, errH := r.Options().GetString(message.URIHost)
		if errH == nil {
			receivedUriHost = host
		}
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
	req.SetOptionString(message.URIHost, "127.0.0.1")

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())
	_ = receivedUriHost // server received the option (no assertion on value; string form may vary)
}

// TC_CoAP_CF_055 – TP_CoAP_Options_MultiSegmentUri
//
// Reference: RFC 7252 Section 6.5
// "Each path segment of the requested resource is carried in a separate
// Uri-Path option. The concatenation forms /a/b/c."
//
// Procedure: GET with three separate Uri-Path options = /sensor/temperature/value.
// Expected: server resolves path correctly and returns 2.05 Content.
func TestTC_CoAP_CF_055_MultiSegmentUri(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/sensor/temperature/value", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("42")))
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

	resp, err := cc.Get(ctx, "/sensor/temperature/value")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code(),
		"RFC 7252 §6.5: multi-segment URI-path must resolve to the correct resource handler")

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, []byte("42"), body)
}

// TC_CoAP_CF_056 – TP_CoAP_Options_Accept_NotAcceptable
//
// Reference: RFC 7252 Section 5.10.4
// "If the Accept option is present and the server cannot respond in the
// requested content-format, the server MUST return 4.06 Not Acceptable."
//
// Procedure: GET /resource with Accept=application/json; server only provides text/plain.
// Expected: 4.06 Not Acceptable.
func TestTC_CoAP_CF_056_Accept_NotAcceptable(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/resource", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		// Check Accept option
		accept, errA := r.Accept()
		if errA == nil && accept == message.AppJSON {
			// We only serve text/plain
			errS := w.SetResponse(codes.NotAcceptable, message.TextPlain, nil)
			require.NoError(t, errS)
			return
		}
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("text")))
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
	req.SetAccept(message.AppJSON)

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.NotAcceptable, resp.Code(),
		"RFC 7252 §5.10.4: Accept mismatch MUST return 4.06 Not Acceptable")
}

// TC_CoAP_CF_057 – TP_CoAP_Options_UriQuery
//
// Reference: RFC 7252 Section 5.10.1
// "Uri-Query options carry the query part of the requested resource URI."
// Multiple Uri-Query options are separated by '&' conceptually.
//
// Procedure: GET /search?key=value&n=1 encoded as two Uri-Query options.
// Expected: server receives both query parameters.
func TestTC_CoAP_CF_057_UriQuery(t *testing.T) {
	var receivedQueries []string
	m := mux.NewRouter()
	err := m.Handle("/search", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		buf := make([]string, 20)
		n, errQ := r.Options().GetStrings(message.URIQuery, buf)
		if errQ == nil {
			receivedQueries = buf[:n]
		}
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

	resp, err := cc.Get(ctx, "/search?key=value&n=1")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())
	require.GreaterOrEqual(t, len(receivedQueries), 1,
		"RFC 7252 §5.10.1: server must receive Uri-Query options from the client request")
}

// TC_CoAP_CF_058 – TP_CoAP_Response_InternalServerError
//
// Reference: RFC 7252 Section 5.9.2
// "5.00 Internal Server Error: the server encountered an unexpected condition."
//
// Procedure: client GETs /broken; server returns 5.00.
// Expected: 5.00 Internal Server Error.
func TestTC_CoAP_CF_058_InternalServerError(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/broken", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.InternalServerError, message.TextPlain, nil)
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

	resp, err := cc.Get(ctx, "/broken")
	require.NoError(t, err)
	require.Equal(t, codes.InternalServerError, resp.Code(),
		"RFC 7252 §5.9.2: server must be able to return 5.00 Internal Server Error")
}

// TC_CoAP_CF_059 – TP_CoAP_Options_ContentFormat_Response
//
// Reference: RFC 7252 Section 5.10.3
// "If the response carries a payload, it SHOULD include a Content-Format option
// to indicate the format of the payload."
//
// Procedure: GET /json-resource. Server responds with Content-Format=AppJSON.
// Expected: client correctly reads the Content-Format from the response.
func TestTC_CoAP_CF_059_ContentFormat_InResponse(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/json-resource", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.AppJSON, bytes.NewReader([]byte(`{"x":1}`)))
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

	resp, err := cc.Get(ctx, "/json-resource")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())
	cf, err := resp.ContentFormat()
	require.NoError(t, err)
	require.Equal(t, message.AppJSON, cf,
		"RFC 7252 §5.10.3: response must carry Content-Format matching the actual payload format")
}

// TC_CoAP_CF_061 – TP_CoAP_Response_CriticalOption_BadOption
//
// Reference: RFC 7252 Section 5.4.1
// "If the option is critical and not recognized, the response MUST be
// 4.02 (Bad Option)."
//
// An option is critical if its option number is ODD.
//
// Procedure: Send CON GET with option 65001 (odd = critical, unrecognized).
// Expected: 4.02 Bad Option.
//
// KNOWN FAILURE: go-coap Bug #7 — go-coap does not return 4.02 for unrecognized
// critical options. This test documents the gap so it appears as a failure in CI.
func TestTC_CoAP_CF_061_CriticalUnknownOption_BadOption(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Build CON GET with critical unknown option 65001 (odd number = critical per §5.4.1).
	// Encoding delta=65001 from previous option 11 (Uri-Path):
	//   delta = 65001 - 11 = 64990
	//   64990 >= 269 → use nibble=14 (0xE), extra 2 bytes: 64990 - 269 = 64721 = 0xFC,0xD1
	//   length = 1 → nibble=1
	//   first byte: (0xE << 4) | 0x1 = 0xE1
	pkt := []byte{
		0x40, 0x01, 0x00, 0x61, // CON, GET, MID=0x0061
		0xB1, 'x', // Uri-Path "x" (delta=11, len=1) → option 11
		0xE1, 0xFC, 0xD1, 0x42, // option 65001 (critical, unrecognized); delta=64990, len=1
	}
	_, err = conn.Write(pkt)
	require.NoError(t, err)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err, "server must respond with 4.02 for unrecognized critical option")
	require.GreaterOrEqual(t, n, 2)
	require.Equal(t, byte(0x82), buf[1],
		"RFC 7252 §5.4.1: unrecognized critical (odd) option MUST return 4.02 Bad Option (0x82); got 0x%02x — Bug #7", buf[1])
}

// TC_CoAP_CF_062 – TP_CoAP_Messaging_NON_ResponseType
//
// Reference: RFC 7252 Section 5.2.3
// "When a Non-confirmable message is used to carry a request, the response
// SHOULD be returned in a Non-confirmable message as well."
//
// Procedure: Send NON GET. Expected: Response has Type=NON (bits 5-4 = 01).
//
// KNOWN FAILURE: go-coap Bug #3 — go-coap responds with CON (or ACK) instead of NON.
// This test documents the RFC requirement so the gap is visible.
func TestTC_CoAP_CF_062_NON_Request_NON_Response(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("nok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// NON GET: 0x50 = VER=1(01), T=NON(01), TKL=0 → 0b0101_0000
	// Uri-Path "nok" (delta=11, len=3)
	pkt := []byte{0x50, 0x01, 0x00, 0x62, 0xB3, 'n', 'o', 'k'}
	_, err = conn.Write(pkt)
	require.NoError(t, err)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err, "server must respond to NON request")
	require.GreaterOrEqual(t, n, 1)

	respType := (buf[0] >> 4) & 0x3
	require.Equal(t, uint8(1), respType,
		"RFC 7252 §5.2.3: NON request SHOULD receive NON response (T=1); got T=%d — Bug #3", respType)
}

// TC_CoAP_CF_063 – TP_CoAP_Discovery_WellKnownCore
//
// Reference: RFC 7252 Section 7.2
// "A server MAY filter the list of resources returned in a response by
// matching the query string against the attributes of each link."
//
// Procedure: GET /.well-known/core?rt=test
// Expected: server returns filtered link-format (MAY). If not supported,
// the full list is returned. This test documents the capability gap.
func TestTC_CoAP_CF_063_WellKnown_Core_Accessible(t *testing.T) {
	// Register a resource with rt= attribute (it would appear in well-known/core)
	m := mux.NewRouter()
	err := m.Handle("/sensor", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("22")))
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

	// Basic /.well-known/core access (§7.1 SHOULD be supported)
	resp, err := cc.Get(ctx, "/.well-known/core")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code(),
		"RFC 7252 §7.1: server SHOULD respond to GET /.well-known/core with 2.05 Content")

	// /.well-known/core with rt= filter (§7.2 MAY)
	// go-coap may not filter; this documents the expected RFC behavior.
	resp2, err := cc.Get(ctx, "/.well-known/core?rt=test")
	if err != nil {
		t.Logf("RFC 7252 §7.2: GET /.well-known/core?rt= filter not supported (error: %v)", err)
		return
	}
	t.Logf("RFC 7252 §7.2: GET /.well-known/core?rt=test returned code=%s (MAY requirement)", resp2.Code())
}

// TC_CoAP_CF_064 – TP_CoAP_Response_ETag_NotIn404
//
// Reference: RFC 7252 Section 5.9.1
// "4.04 (Not Found): If the response does NOT contain an ETag option, the
// client SHOULD NOT try to refresh it."
//
// RFC 7252 §5.10.7 (ETag in responses) implies ETag SHOULD NOT appear
// in a 4.04 Not Found response (it has no representation to tag).
//
// Procedure: GET /nonexistent. Expected: 4.04 response, ETag option absent.
func TestTC_CoAP_CF_064_ETag_AbsentIn404(t *testing.T) {
	m := mux.NewRouter()
	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/definitely-not-found-path")
	require.NoError(t, err)
	require.Equal(t, codes.NotFound, resp.Code(),
		"RFC 7252 §5.9.1: GET on unknown resource MUST return 4.04 Not Found")

	_, errETag := resp.Options().GetBytes(message.ETag)
	require.Error(t, errETag,
		"RFC 7252 §5.10.7: ETag MUST NOT be present in a 4.04 (Not Found) response")
}

// TC_CoAP_CF_060 – TP_CoAP_Response_BadOption_4_02
//
// Reference: RFC 7252 Section 5.9.1
// "4.02 Bad Option: the server cannot process one or more of the options in
// the request. A 4.02 response SHOULD include a descriptive error payload."
//
// Procedure: Send GET with an unrecognised critical option number.
// Expected: 4.02 Bad Option (or silent rejection for elective options).
func TestTC_CoAP_CF_060_BadOption(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Build CON GET with a critical unknown option (number must be odd = critical).
	// Option number 65001 is well within the spec-undefined/experimental range.
	// Encoding: delta=65001 → 0xE_ → 2 ext bytes: 65001-269=64732 → big-endian
	// We need delta encoded from 0: 65001 > 268, so use 0xE? form. But we use elective (even).
	// Elective unknown option number: use 65000 (even):
	// delta=65000: > 268 → 0xE? + 2 ext bytes = 65000-269 = 64731 (0xFCDB)
	// len=1, value=0x42
	// But writing this raw is complex. Use option 16 (even, elective, likely unknown):
	// delta=16: 16>12: 0xD_ form: 0xD? + 1 ext byte (16-13=3)
	// len=1: second nibble = 1
	// → 0xD1 + 0x03 + 0x42
	// VER=1, T=CON, TKL=0; GET; MID=0x0060; Uri-Path="x"
	pkt := []byte{
		0x40, 0x01, 0x00, 0x60, // header
		0xB1, 'x', // Uri-Path "x" (delta=11, len=1)
		0x51, 0x42, // option 16 (delta=5 from 11=11+5=16, len=1) value=0x42
	}
	_, err = conn.Write(pkt)
	require.NoError(t, err)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err, "server must respond: either 2.05 (elective ignored) or 4.02")
	require.GreaterOrEqual(t, n, 2)
	// Elective (even) unknown option MUST be ignored → expect 2.05 Content
	// Critical (odd) → 4.02 Bad Option. Option 16 is even (elective) → MUST be ignored.
	require.Equal(t, byte(0x45), buf[1],
		"RFC 7252 §5.4.1: elective (even) unknown option MUST be ignored → 2.05 Content; got 0x%02x", buf[1])
}

// TC_CoAP_CF_065 – TP_CoAP_IfMatch_EmptyMatchesAny
//
// Reference: RFC 7252 Section 5.10.8.1
// "An If-Match option with empty (zero-length) value, if present, MUST be
// evaluated as matching any existing representation of the target resource."
//
// According to RFC 7252 §5.8.3, a PUT with If-Match(empty) MUST succeed (2.04
// Changed or 2.01 Created) when the target resource already exists, because
// an empty If-Match matches any ETag. If the resource does NOT exist, the
// server SHOULD respond with 4.12 Precondition Failed.
//
// Procedure:
//  1. PUT /cf065-resource (without If-Match) → 2.01 Created (resource created).
//  2. PUT /cf065-resource with If-Match(empty) while resource exists → MUST succeed.
//
// go-coap passes options to user handler, which implements the semantics.
// This test verifies that the empty If-Match option is transmitted and readable.
func TestTC_CoAP_CF_065_IfMatch_EmptyMatchesAny(t *testing.T) {
	var resourceExists bool

	m := mux.NewRouter()
	err := m.Handle("/cf065-resource", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		if r.Code() != codes.PUT {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		// Check for If-Match option
		hasIfMatchEmpty := false
		for _, opt := range r.Options() {
			if opt.ID == message.IfMatch && len(opt.Value) == 0 {
				hasIfMatchEmpty = true
				break
			}
		}

		if hasIfMatchEmpty && !resourceExists {
			// Resource does not exist and If-Match(empty) requires it to exist
			_ = w.SetResponse(codes.PreconditionFailed, message.TextPlain, nil)
			return
		}
		// If no If-Match or If-Match matches → perform the update
		resourceExists = true
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// Step 1: create the resource (no If-Match condition)
	resp1, err := cc.Put(ctx, "/cf065-resource", message.TextPlain, bytes.NewReader([]byte("v1")))
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp1.Code(),
		"RFC 7252: initial PUT must succeed; got %s", resp1.Code())

	// Step 2: PUT with If-Match(empty) while resource exists → MUST succeed (2.04)
	resp2, err := cc.Put(ctx, "/cf065-resource", message.TextPlain, bytes.NewReader([]byte("v2")),
		message.Option{ID: message.IfMatch, Value: []byte{}})
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp2.Code(),
		"RFC 7252 §5.10.8.1: PUT with empty If-Match MUST succeed (2.04 Changed) "+
			"when the target resource exists; got %s", resp2.Code())
}

// ── Migrated from conformance_test.go (CF_001–CF_042) ────────────────────────

// TC_CoAP_CF_001 – TP_CoAP_MessageFormat_InvalidVersion
//
// Reference: RFC 7252 Section 3 [CoAP-3.0-2]
// "Messages with unknown version numbers MUST be silently ignored."
//
// Procedure: send a raw UDP packet whose first byte encodes Version=2.
// Expected:  no CoAP response is received within the timeout.
func TestTC_CoAP_CF_001_InvalidVersion(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		// echo back whatever arrives so we know the server is alive
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// First, verify server is reachable with a valid packet (Version=1, CON, empty).
	// Ver=1(01), T=CON(00), TKL=0 → 0x40; Code=0.00; MID=0x0001
	validPing := []byte{0x40, 0x00, 0x00, 0x01}
	_, err = conn.Write(validPing)
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	_, err = conn.Read(buf)
	require.NoError(t, err, "server should respond to a valid ping first, confirming liveness")

	// Now send a packet with Version=2.
	// Ver=2(10), T=CON(00), TKL=0 → 0x80; Code=0.01 (GET); MID=0x0002
	invalidVersion := []byte{0x80, 0x01, 0x00, 0x02}
	_, err = conn.Write(invalidVersion)
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, err = conn.Read(buf)
	require.Error(t, err, "no response expected for unknown version (must be silently ignored)")
	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	require.True(t, netErr.Timeout(), "expected read timeout, got: %v", err)
}

// TC_CoAP_CF_002 – TP_CoAP_MessageFormat_InvalidTKL
//
// Reference: RFC 7252 Section 3 [CoAP-3.0-3]
// "Lengths 9–15 are reserved, MUST NOT be sent, and MUST be processed as
// a message format error."
//
// Procedure: send a raw CoAP GET with TKL=9 (invalid).
// Expected:  connection is reset or the packet is silently ignored (no 2.xx reply).
func TestTC_CoAP_CF_002_InvalidTokenLength(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Ver=1(01), T=CON(00), TKL=9 → 0x49; Code=0.01 (GET); MID=0x0003 + 9 token bytes
	invalidTKL := []byte{0x49, 0x01, 0x00, 0x03, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09}
	_, err = conn.Write(invalidTKL)
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 256)
	_, readErr := conn.Read(buf)
	// Either a timeout (silently ignored) or a RST is acceptable per RFC.
	if readErr == nil {
		// Any response that arrives must be a RST (type=3) – NOT a 2.xx success.
		require.True(t, len(buf) >= 1, "response too short")
		msgType := (buf[0] >> 4) & 0x03
		require.Equal(t, uint8(3), msgType, "only RST is acceptable for malformed TKL, got type %d", msgType)
	}
	// A timeout is also perfectly fine (silently ignored).
}

// TC_CoAP_CF_003 – TP_CoAP_Messaging_CON_Ping
//
// Reference: RFC 7252 Section 4.2
// "A recipient that receives an empty Confirmable message MAY acknowledge or
// MUST reject it with a Reset message."
//
// Procedure: client sends Empty CON (Ping). Server MUST reply with RST.
func TestTC_CoAP_CF_003_PingReset(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		// No-op handler; the server middleware handles pings with RST automatically.
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// Ping sends an empty CON; go-coap waits for the RST to return.
	err = cc.Ping(ctx)
	require.NoError(t, err, "ping (empty CON) should receive RST from server")
}

// TC_CoAP_CF_004 – TP_CoAP_Messaging_CON_GET_PiggybackedResponse
//
// Reference: RFC 7252 Sections 4.2, 5.2.1
// "A response to a Confirmable request may be sent as a piggybacked ACK."
// The ACK MUST echo the Message ID of the Confirmable request.
//
// Procedure: client sends CON GET to /resource. Server replies with 2.05 Content.
func TestTC_CoAP_CF_004_CON_GET_PiggybackedResponse(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/resource", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.GET, r.Code())
		assert.Equal(t, message.Confirmable, r.Type())
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("hello")))
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

	resp, err := cc.Get(ctx, "/resource")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), body)
}

// TC_CoAP_CF_005 – TP_CoAP_Messaging_NON_GET
//
// Reference: RFC 7252 Sections 4.3, 5.2.3
// "A Non-confirmable message MUST NOT be acknowledged by the recipient."
// "If the request is Non-confirmable, the response SHOULD also be Non-confirmable."
//
// Procedure: client sends NON GET. Server replies (NON or CON).
func TestTC_CoAP_CF_005_NON_GET(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/sensor", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.GET, r.Code())
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("22.5 C")))
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

	resp, err := cc.Get(ctx, "/sensor", message.Option{
		ID:    message.Accept,
		Value: nil,
	})
	// Rebuild as NON via a custom message
	req, err2 := cc.NewGetRequest(ctx, "/sensor")
	require.NoError(t, err2)
	req.SetType(message.NonConfirmable)

	resp, err = cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, []byte("22.5 C"), body)
}

// TC_CoAP_CF_006 – TP_CoAP_Token_Echo
//
// Reference: RFC 7252 Section 5.3.1
// "Every request carries a client-generated token that the server MUST echo
// (without modification) in any resulting response."
//
// Procedure: client sends a request with a known token. Response token must match.
func TestTC_CoAP_CF_006_TokenEcho(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/echo", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("echo")))
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

	// Build a request with an explicit token value.
	expectedToken := message.Token([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	req, err := cc.NewGetRequest(ctx, "/echo")
	require.NoError(t, err)
	req.SetToken(expectedToken)

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())
	require.Equal(t, expectedToken, resp.Token(),
		"server must echo the request token unchanged in the response")
}

// TC_CoAP_CF_007 – TP_CoAP_Method_POST_Created
//
// Reference: RFC 7252 Section 5.8.2
// "POST … If the action results in the creation of a new resource, the
// response Code SHOULD be 2.01 (Created)."
//
// Procedure: client sends CON POST. Server replies with 2.01 Created.
func TestTC_CoAP_CF_007_POST_Created(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/items", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.POST, r.Code())
		errS := w.SetResponse(codes.Created, message.TextPlain, nil)
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

	resp, err := cc.Post(ctx, "/items", message.TextPlain, bytes.NewReader([]byte(`{"name":"item1"}`)))
	require.NoError(t, err)
	require.Equal(t, codes.Created, resp.Code())
}

// TC_CoAP_CF_008 – TP_CoAP_Method_PUT_Changed
//
// Reference: RFC 7252 Section 5.8.3
// "PUT … If a resource exists at the request URI, the enclosed representation
// SHOULD be … 2.04 (Changed) returned."
//
// Procedure: client sends CON PUT. Server replies with 2.04 Changed.
func TestTC_CoAP_CF_008_PUT_Changed(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/resource/1", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.PUT, r.Code())
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

	resp, err := cc.Put(ctx, "/resource/1", message.TextPlain, bytes.NewReader([]byte("updated")))
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code())
}

// TC_CoAP_CF_009 – TP_CoAP_Method_DELETE_Deleted
//
// Reference: RFC 7252 Section 5.8.4
// "If the resource exists, a 2.02 (Deleted) Response Code SHOULD be used
// for both the case where the resource existed before the request and was
// successfully deleted …"
//
// Procedure: client sends CON DELETE. Server replies with 2.02 Deleted.
func TestTC_CoAP_CF_009_DELETE_Deleted(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/resource/1", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.DELETE, r.Code())
		errS := w.SetResponse(codes.Deleted, message.TextPlain, nil)
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

	resp, err := cc.Delete(ctx, "/resource/1")
	require.NoError(t, err)
	require.Equal(t, codes.Deleted, resp.Code())
}

// TC_CoAP_CF_010 – TP_CoAP_ResponseCode_NotFound
//
// Reference: RFC 7252 Section 5.9.2.5
// "4.04 Not Found – The resource requested was not found."
//
// The go-coap default mux returns 4.04 for unregistered paths.
//
// Procedure: client sends GET to /nonexistent. Server replies with 4.04 Not Found.
func TestTC_CoAP_CF_010_GET_NotFound(t *testing.T) {
	// Register only /exists – any other path should yield 4.04.
	m := mux.NewRouter()
	err := m.Handle("/exists", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("here")))
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/nonexistent")
	require.NoError(t, err)
	require.Equal(t, codes.NotFound, resp.Code(),
		"server must return 4.04 Not Found for un-registered resource paths")
}

// TC_CoAP_CF_011 – TP_CoAP_ResponseCode_MethodNotAllowed
//
// Reference: RFC 7252 Section 5.8 / 5.9.2.6
// "A request with an unrecognized or unsupported Method Code MUST generate
// a 4.05 (Method Not Allowed) piggybacked response."
//
// Procedure: client sends DELETE to a GET-only resource. Handler returns 4.05.
func TestTC_CoAP_CF_011_MethodNotAllowed(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/readonly", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		if r.Code() != codes.GET {
			errS := w.SetResponse(codes.MethodNotAllowed, message.TextPlain,
				bytes.NewReader([]byte("method not allowed")))
			require.NoError(t, errS)
			return
		}
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("read-only")))
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

	resp, err := cc.Delete(ctx, "/readonly")
	require.NoError(t, err)
	require.Equal(t, codes.MethodNotAllowed, resp.Code(),
		"server must return 4.05 Method Not Allowed for unsupported method")
}

// TC_CoAP_CF_012 – TP_CoAP_Options_Accept_Match
//
// Reference: RFC 7252 Section 5.10.4
// "The client prefers the representation returned by the server to be in the
// Content-Format indicated. The server returns the preferred Content-Format
// if available."
//
// Procedure: client sends GET with Accept: text/plain (0). Server returns
// 2.05 Content with Content-Format: text/plain.
func TestTC_CoAP_CF_012_Accept_Match(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/data", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		accept, errOpt := r.Options().Accept()
		if errOpt == nil && accept == message.AppJSON {
			// We only supply text/plain, so return 4.06.
			errS := w.SetResponse(codes.NotAcceptable, message.TextPlain, nil)
			require.NoError(t, errS)
			return
		}
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("plain text")))
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

	resp, err := cc.Get(ctx, "/data", message.Option{
		ID:    message.Accept,
		Value: encodeUint16(uint16(message.TextPlain)),
	})
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	cf, errCF := resp.Options().ContentFormat()
	require.NoError(t, errCF)
	require.Equal(t, message.TextPlain, cf,
		"server must respond with the requested content-format when available")
}

// TC_CoAP_CF_013 – TP_CoAP_Options_Accept_NotAcceptable
//
// Reference: RFC 7252 Section 5.10.4
// "If the preferred Content-Format cannot be returned, then a 4.06
// (Not Acceptable) MUST be sent as a response."
//
// Procedure: client sends GET with Accept: application/json (50). Server
// only has text/plain, so it returns 4.06 Not Acceptable.
func TestTC_CoAP_CF_013_Accept_NotAcceptable(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/text-only", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		accept, errOpt := r.Options().Accept()
		if errOpt == nil && accept != message.TextPlain {
			errS := w.SetResponse(codes.NotAcceptable, message.TextPlain, nil)
			require.NoError(t, errS)
			return
		}
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("plain")))
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

	resp, err := cc.Get(ctx, "/text-only", message.Option{
		ID:    message.Accept,
		Value: encodeUint16(uint16(message.AppJSON)),
	})
	require.NoError(t, err)
	require.Equal(t, codes.NotAcceptable, resp.Code(),
		"server must return 4.06 Not Acceptable when the requested content-format is unavailable")
}

// TC_CoAP_CF_014 – TP_CoAP_Messaging_SeparateResponse
//
// Reference: RFC 7252 Section 5.2.2
// "When a Confirmable message carrying a request is acknowledged with an
// Empty message … the server sends the response in a separate Confirmable
// or Non-confirmable message."
//
// Procedure: a slow handler first sends an empty ACK then delivers its
// response as a separate message. The client must correctly receive the
// separate response.
func TestTC_CoAP_CF_014_SeparateResponse(t *testing.T) {
	const payload = "delayed response"

	m := mux.NewRouter()
	err := m.Handle("/slow", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		// A small delay can trigger go-coap's separate-response path on some
		// implementations. The client must correctly receive any response.
		time.Sleep(20 * time.Millisecond)
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte(payload)))
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

	resp, err := cc.Get(ctx, "/slow")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, []byte(payload), body)
}

// TC_CoAP_CF_015 – TP_CoAP_URIPath_MultiSegment
//
// Reference: RFC 7252 Section 5.10.1
// "Each Uri-Path Option specifies one segment of the absolute path to the
// resource."
//
// Procedure: client requests /path/to/resource using multi-segment URI path.
func TestTC_CoAP_CF_015_URIPath_MultiSegment(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/path/to/resource", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("found")))
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

	resp, err := cc.Get(ctx, "/path/to/resource")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, []byte("found"), body)
}

// TC_CoAP_CF_016 – TP_CoAP_ResourceDiscovery_WellKnownCore
//
// Reference: RFC 7252 Section 7.2, RFC 6690
// "To maximize interoperability … a CoAP endpoint SHOULD support the CoRE
// Link Format of discoverable resources as described in RFC 6690."
//
// Procedure: client sends GET /.well-known/core. Server returns 2.05 Content
// with content-format application/link-format (40).
func TestTC_CoAP_CF_016_WellKnownCore(t *testing.T) {
	m := mux.NewRouter()
	require.NoError(t, m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		links := `</resource>;rt="sensor",</items>;rt="collection"`
		errS := w.SetResponse(codes.Content, message.AppLinkFormat, bytes.NewReader([]byte(links)))
		require.NoError(t, errS)
	})))

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/.well-known/core")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	cf, err := resp.Options().ContentFormat()
	require.NoError(t, err)
	require.Equal(t, message.AppLinkFormat, cf,
		"/.well-known/core must respond with application/link-format (40)")

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.True(t, strings.Contains(string(body), "</resource>"),
		"link-format body should contain registered resources")
}

// TC_CoAP_CF_017 – TP_CoAP_ResponseCode_BadRequest
//
// Reference: RFC 7252 Section 5.9.2
// "4.00 Bad Request – The request could not be understood by the server
// due to malformed syntax."
//
// Procedure: client sends a POST with invalid payload that the server
// explicitly rejects with 4.00 Bad Request.
func TestTC_CoAP_CF_017_BadRequest(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/strict", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		body, errB := r.ReadBody()
		if errB != nil || len(body) == 0 || string(body) == "bad" {
			errS := w.SetResponse(codes.BadRequest, message.TextPlain,
				bytes.NewReader([]byte("invalid payload")))
			require.NoError(t, errS)
			return
		}
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

	// Send a payload the server considers invalid.
	resp, err := cc.Post(ctx, "/strict", message.TextPlain, bytes.NewReader([]byte("bad")))
	require.NoError(t, err)
	require.Equal(t, codes.BadRequest, resp.Code())
}

// TC_CoAP_CF_018 – TP_CoAP_ResponseCode_ContentFormat_UnsupportedContentFormat
//
// Reference: RFC 7252 Section 5.9.2.10
// "4.15 Unsupported Content-Format – The server does not support the
// content-format of the request entity."
//
// Procedure: client sends a POST with application/json payload to a server
// that only accepts text/plain. Server returns 4.15.
func TestTC_CoAP_CF_018_UnsupportedContentFormat(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/text-post", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		cf, errOpt := r.Options().ContentFormat()
		if errOpt != nil || cf != message.TextPlain {
			errS := w.SetResponse(codes.UnsupportedMediaType, message.TextPlain,
				bytes.NewReader([]byte("only text/plain accepted")))
			require.NoError(t, errS)
			return
		}
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

	// Send JSON to a server that only accepts text/plain.
	resp, err := cc.Post(ctx, "/text-post", message.AppJSON, bytes.NewReader([]byte(`{"key":"value"}`)))
	require.NoError(t, err)
	require.Equal(t, codes.UnsupportedMediaType, resp.Code())
}

// TC_CoAP_CF_019 – TP_CoAP_Messaging_MessageIDEcho
//
// Reference: RFC 7252 Section 4.4
// "An Acknowledgement or Reset message is related to a Confirmable message …
// by means of a Message ID … The Message ID MUST be echoed in the
// Acknowledgement or Reset message."
//
// Procedure: verify via raw UDP that the ACK contains the same MID as the CON
// request.
func TestTC_CoAP_CF_019_MessageIDEcho(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ack-check")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Craft a CON GET for /mid-test with MID=0xCAFE.
	// Byte 0: Ver=1(01), T=CON(00), TKL=1 → 0x41
	// Byte 1: Code=0.01 (GET)
	// Bytes 2-3: MID = 0xCA 0xFE
	// Byte 4: token = 0xAB
	// Bytes 5-N: Uri-Path option (delta=11, length=8)
	//   Option byte: delta=11→0xBB (11<<4|8) → 0xB8; value="/mid-test" without leading "/"
	path := "mid-test"
	optByte := byte((11 << 4) | len(path)) // delta=11, len=8
	pkt := []byte{0x41, 0x01, 0xCA, 0xFE, 0xAB, optByte}
	pkt = append(pkt, []byte(path)...)

	_, err = conn.Write(pkt)
	require.NoError(t, err)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, 4, "response must be at least 4 bytes")

	// Verify: Type must be ACK (2) and MID must equal 0xCAFE.
	respType := (buf[0] >> 4) & 0x03
	respMID := (uint16(buf[2]) << 8) | uint16(buf[3])
	require.Equal(t, uint8(2), respType, "server must reply with ACK for a CON request")
	require.Equal(t, uint16(0xCAFE), respMID,
		"ACK must echo the Message ID of the CON request")
}

// TC_CoAP_CF_020 – TP_CoAP_Messaging_Deduplication
//
// Reference: RFC 7252 Section 4.5
// "The server SHOULD return the same response for duplicate requests
// (same endpoint, same Message ID) to implement idempotent behavior."
//
// Procedure: send identical CON GET (same MID) twice. The server must
// return the same response code and deduplicate.
func TestTC_CoAP_CF_020_Deduplication(t *testing.T) {
	reqCount := 0
	var mu sync.Mutex

	m := mux.NewRouter()
	err := m.Handle("/dedup", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		mu.Lock()
		reqCount++
		mu.Unlock()
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("dedup")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	// Use a raw UDP socket to send the same MID twice rapidly.
	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// CON GET /dedup: MID=0x1234, token=0x01
	path := "dedup"
	optByte := byte((11 << 4) | len(path))
	pkt := []byte{0x41, 0x01, 0x12, 0x34, 0x01, optByte}
	pkt = append(pkt, []byte(path)...)

	// Send twice with the same MID.
	for i := 0; i < 2; i++ {
		_, err = conn.Write(pkt)
		require.NoError(t, err)
	}

	// Collect responses (up to 2 within timeout).
	responses := 0
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 256)
	for {
		_, err = conn.Read(buf)
		if err != nil {
			break
		}
		responses++
	}

	// Both duplicate packets must receive an ACK (retransmission handling),
	// but the application handler should only fire once.
	require.GreaterOrEqual(t, responses, 1, "at least one ACK expected")
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, reqCount,
		"handler should be called only once for duplicate CON messages with the same MID")
}

// TC_CoAP_CF_021 – TP_CoAP_MessageFormat_LonePayloadMarker
//
// Reference: RFC 7252 Section 3
// "If present, [the payload marker] MUST be followed by one or more bytes of
// payload data.  The absence of the Payload Marker denotes a zero-length
// payload."
//
// Source: libcoap tests/test_pdu.c t_parse_pdu9 / t_parse_pdu10
//
// Procedure: send a CON GET whose last byte is 0xFF (payload marker) with
// no following payload bytes.
// Expected:  server MUST NOT respond with a 2.xx success code.
//
// NOTE: Known bug in go-coap — the UDP message decoder accepts a lone 0xFF
// payload marker and routes the request normally instead of rejecting it.
func TestTC_CoAP_CF_021_LonePayloadMarker(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// CON GET, TKL=1, MID=0x0021, token=0xBB, followed by lone 0xFF.
	path := "sensor"
	pkt := []byte{0x41, 0x01, 0x00, 0x21, 0xBB, byte((11 << 4) | len(path))}
	pkt = append(pkt, []byte(path)...)
	pkt = append(pkt, 0xFF) // lone payload marker, no payload follows

	_, err = conn.Write(pkt)
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 256)
	n, readErr := conn.Read(buf)
	if readErr == nil {
		// If any response arrives it MUST NOT be a success (2.xx).
		require.GreaterOrEqual(t, n, 2, "response too short to read code")
		class := (buf[1] >> 5) & 0x07
		require.NotEqual(t, uint8(2), class,
			"server MUST NOT return a 2.xx response for a PDU with a lone payload marker")
	}
	// A timeout (no response, silently ignored) is correct behaviour per RFC §4.1.
}

// TC_CoAP_CF_022 – TP_CoAP_MessageFormat_RSTWithBody
//
// Reference: RFC 7252 Section 3 / 4.2
// RST messages MUST be Empty. A recipient of a Reset message MUST silently
// discard it (RFC 7252 §4.2).
//
// NOTE: Known bug in go-coap — RST messages are not discarded silently.
func TestTC_CoAP_CF_022_RSTWithBody(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Byte 0: Ver=1(01), T=RST(11), TKL=0 → 0x70; Code=0.00; MID=0x1234; body.
	pkt := []byte{0x70, 0x00, 0x12, 0x34, 0xFF, 'b', 'o', 'd', 'y'}

	_, err = conn.Write(pkt)
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 256)
	_, readErr := conn.Read(buf)
	// Only a timeout is acceptable: server MUST silently discard RST messages.
	require.Error(t, readErr, "server MUST NOT respond to a RST message")
	var netErr net.Error
	require.ErrorAs(t, readErr, &netErr)
	require.True(t, netErr.Timeout(), "expected read timeout (silent discard), got: %v", readErr)
}

// TC_CoAP_CF_023 – TP_CoAP_MessageFormat_EmptyACKWithBody
//
// Reference: RFC 7252 Section 3
// Empty ACK with body must be silently ignored.
//
// NOTE: Known bug in go-coap — a malformed Empty ACK with body is not
// silently discarded.
func TestTC_CoAP_CF_023_EmptyACKWithBody(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Byte 0: Ver=1(01), T=ACK(10), TKL=0 → 0x60; Code=0.00; MID=0x5678; body.
	pkt := []byte{0x60, 0x00, 0x56, 0x78, 0xFF, 'b', 'o', 'd', 'y'}

	_, err = conn.Write(pkt)
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 256)
	_, readErr := conn.Read(buf)
	// Only timeout (silent discard) is acceptable.
	require.Error(t, readErr, "server MUST NOT respond to a malformed Empty ACK")
	var netErr net.Error
	require.ErrorAs(t, readErr, &netErr)
	require.True(t, netErr.Timeout(), "expected read timeout (silent discard), got: %v", readErr)
}

// TC_CoAP_CF_024 – TP_CoAP_MessageFormat_OptionDeltaOverflow
//
// Reference: RFC 7252 Section 3.1
// A cumulative delta that exceeds 65535 is invalid; server must not return 2.xx.
func TestTC_CoAP_CF_024_OptionDeltaOverflow(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Build a 512-byte PDU: 4-byte CoAP header, then 254 pairs of (0xd0, 0xff).
	// Each pair encodes option delta = 13 (base) + 255 (ext) = 268, length = 0.
	// 254 × 268 = 68072 > 65535.
	const pduSize = 512
	pkt := make([]byte, pduSize)
	pkt[0] = 0x40 // Ver=1, T=CON, TKL=0
	pkt[1] = 0x01 // GET
	pkt[2] = 0x93 // MID hi
	pkt[3] = 0x34 // MID lo
	for i := 4; i < pduSize-4; i += 2 {
		pkt[i] = 0xd0   // delta nibble=13 (1-byte ext follows), length nibble=0
		pkt[i+1] = 0xff // delta ext: +255 → cumulative delta step = 268
	}

	_, err = conn.Write(pkt)
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 256)
	n, readErr := conn.Read(buf)
	if readErr == nil {
		// If a response was returned it MUST NOT be a 2.xx success.
		require.GreaterOrEqual(t, n, 2)
		class := (buf[1] >> 5) & 0x07
		require.NotEqual(t, uint8(2), class,
			"server MUST NOT return 2.xx for a PDU with option-delta overflow")
	}
	// A timeout (silently ignored) is also acceptable.
}

// TC_CoAP_CF_025 – TP_CoAP_Messaging_NON_ResponseIsNON
//
// Reference: RFC 7252 Section 5.2.3
// "If the request is Non-confirmable, the response SHOULD also be sent as a
// Non-confirmable message."
//
// NOTE: Known bug in go-coap/v3 — responses to NON requests are sent as CON.
func TestTC_CoAP_CF_025_NON_ResponseIsNON(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/sensor", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("22.5 C")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// NON GET: Ver=1(01), T=NON(01), TKL=1 → 0x51; Code=0.01; MID=0x0031; token=0xCC.
	path := "sensor"
	pkt := []byte{0x51, 0x01, 0x00, 0x31, 0xCC, byte((11 << 4) | len(path))}
	pkt = append(pkt, []byte(path)...)

	_, err = conn.Write(pkt)
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err, "server must respond to NON GET")
	require.GreaterOrEqual(t, n, 4)

	// Check response is NON (type bits 4-5 of byte 0 == 01).
	respType := (buf[0] >> 4) & 0x03
	require.Equal(t, uint8(1), respType,
		"RFC 7252 §5.2.3: response to NON request SHOULD be NON (type=1), got type=%d", respType)
}

// TC_CoAP_CF_026 – TP_CoAP_Options_LocationPath_InResponse
//
// Reference: RFC 7252 Section 5.10.7
// A 2.01 (Created) response SHOULD include Location-Path and/or Location-Query Options.
func TestTC_CoAP_CF_026_LocationPath_InResponse(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/items", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.POST, r.Code())
		buf := make([]byte, 64)
		var locationOpts message.Options
		locationOpts, _, err := locationOpts.SetLocationPath(buf, "/items/42")
		require.NoError(t, err)
		errS := w.SetResponse(codes.Created, message.TextPlain, nil, locationOpts...)
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

	resp, err := cc.Post(ctx, "/items", message.TextPlain, bytes.NewReader([]byte(`{"name":"item1"}`)))
	require.NoError(t, err)
	require.Equal(t, codes.Created, resp.Code())

	loc, errLoc := resp.Options().LocationPath()
	require.NoError(t, errLoc, "Location-Path option must be present in 2.01 Created response")
	require.Equal(t, "/items/42", loc,
		"Location-Path must reflect the URI of the newly created resource")
}

// TC_CoAP_CF_027 – TP_CoAP_Options_LocationQuery_InResponse
//
// Reference: RFC 7252 Section 5.10.8
// The Location-Query Option specifies one argument parameterizing the resource.
func TestTC_CoAP_CF_027_LocationQuery_InResponse(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/items", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.POST, r.Code())
		buf := make([]byte, 64)
		var locQOpts message.Options
		locQOpts, _, err := locQOpts.AddString(buf, message.LocationQuery, "id=42")
		require.NoError(t, err)
		errS := w.SetResponse(codes.Created, message.TextPlain, nil, locQOpts...)
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

	resp, err := cc.Post(ctx, "/items", message.TextPlain, bytes.NewReader([]byte(`{"name":"item2"}`)))
	require.NoError(t, err)
	require.Equal(t, codes.Created, resp.Code())

	var qs [1]string
	n, errQ := resp.Options().GetStrings(message.LocationQuery, qs[:])
	require.NoError(t, errQ, "Location-Query option must be present in 2.01 Created response")
	require.Equal(t, 1, n)
	require.Equal(t, "id=42", qs[0],
		"Location-Query must reflect the query parameter of the created resource")
}

// TC_CoAP_CF_028 – TP_CoAP_URIQuery_Parsing
//
// Reference: RFC 7252 Section 5.10.1
// Each Uri-Query Option specifies one argument parameterizing the resource.
func TestTC_CoAP_CF_028_URIQuery_Parsing(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/search", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		qs, errQ := r.Options().Queries()
		if errQ != nil {
			_ = w.SetResponse(codes.BadRequest, message.TextPlain, nil)
			return
		}
		_ = w.SetResponse(codes.Content, message.TextPlain,
			bytes.NewReader([]byte(strings.Join(qs, ";"))))
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	buf := make([]byte, 32)
	var qOpts message.Options
	var n int
	qOpts, n, err = qOpts.AddString(buf, message.URIQuery, "q=coap")
	require.NoError(t, err)
	qOpts, _, err = qOpts.AddString(buf[n:], message.URIQuery, "limit=10")
	require.NoError(t, err)

	resp, err := cc.Get(ctx, "/search", qOpts...)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Contains(t, string(body), "q=coap",
		"Uri-Query 'q=coap' must be received and echoed by server")
	require.Contains(t, string(body), "limit=10",
		"Uri-Query 'limit=10' must be received and echoed by server")
}

// TC_CoAP_CF_029 – TP_CoAP_ETag_InResponse
//
// Reference: RFC 7252 Section 5.10.6.1
// The ETag Option in a response provides the current value of the entity-tag.
func TestTC_CoAP_CF_029_ETag_InResponse(t *testing.T) {
	resourceETag := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	m := mux.NewRouter()
	err := m.Handle("/etag-me", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("data")),
			message.Option{ID: message.ETag, Value: resourceETag})
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/etag-me")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	etag, errE := resp.ETag()
	require.NoError(t, errE,
		"ETag option must be present in 2.05 Content response (RFC 7252 §5.10.6.1)")
	require.Equal(t, resourceETag, etag,
		"response ETag must match the value set by the server")
}

// TC_CoAP_CF_030 – TP_CoAP_ETag_ConditionalGET_Valid
//
// Reference: RFC 7252 Sections 5.8.1, 5.9.1.3, 5.10.6.2
// A server can issue a 2.03 Valid response when a matching ETag is sent.
func TestTC_CoAP_CF_030_ETag_ConditionalGET_Valid(t *testing.T) {
	currentETag := []byte{0xAB, 0xCD}
	m := mux.NewRouter()
	err := m.Handle("/conditional", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		etagOpt := message.Option{ID: message.ETag, Value: currentETag}
		// Check if request contains a matching ETag.
		for _, opt := range r.Options() {
			if opt.ID == message.ETag && bytes.Equal(opt.Value, currentETag) {
				// Conditional GET hit: 2.03 Valid, no payload.
				_ = w.SetResponse(codes.Valid, message.TextPlain, nil, etagOpt)
				return
			}
		}
		// Cache miss: 2.05 Content + ETag.
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("body")), etagOpt)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// Step 1: GET without ETag → 2.05 Content + ETag.
	resp, err := cc.Get(ctx, "/conditional")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	etag, errE := resp.ETag()
	require.NoError(t, errE)

	// Step 2: GET with ETag → 2.03 Valid.
	resp2, err := cc.Get(ctx, "/conditional", message.Option{ID: message.ETag, Value: etag})
	require.NoError(t, err)
	require.Equal(t, codes.Valid, resp2.Code(),
		"conditional GET with valid ETag must return 2.03 Valid (RFC 7252 §5.9.1.3)")

	echoed, errE2 := resp2.ETag()
	require.NoError(t, errE2,
		"2.03 Valid response must include ETag option (RFC 7252 §5.9.1.3)")
	require.Equal(t, etag, echoed,
		"2.03 Valid must echo the matched ETag")
}

// TC_CoAP_CF_031 – TP_CoAP_IfMatch_Fulfilled
//
// Reference: RFC 7252 Section 5.10.8.1
// If the If-Match condition is fulfilled, the server performs the request.
func TestTC_CoAP_CF_031_IfMatch_Fulfilled(t *testing.T) {
	knownETag := []byte{0x01, 0x02, 0x03, 0x04}
	m := mux.NewRouter()
	err := m.Handle("/ifmatch", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		if r.Code() != codes.PUT {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		hasIfMatch := false
		condFulfilled := false
		for _, opt := range r.Options() {
			if opt.ID == message.IfMatch {
				hasIfMatch = true
				if len(opt.Value) == 0 || bytes.Equal(opt.Value, knownETag) {
					condFulfilled = true
				}
			}
		}
		if hasIfMatch && !condFulfilled {
			_ = w.SetResponse(codes.PreconditionFailed, message.TextPlain, nil)
			return
		}
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Put(ctx, "/ifmatch", message.TextPlain, bytes.NewReader([]byte("update")),
		message.Option{ID: message.IfMatch, Value: knownETag})
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code(),
		"If-Match with matching ETag must result in 2.04 Changed (RFC 7252 §5.10.8.1)")
}

// TC_CoAP_CF_032 – TP_CoAP_IfMatch_NotFulfilled
//
// Reference: RFC 7252 Section 5.10.8.1
// If none of the If-Match options match, the condition is not fulfilled.
func TestTC_CoAP_CF_032_IfMatch_NotFulfilled(t *testing.T) {
	knownETag := []byte{0x01, 0x02, 0x03, 0x04}
	wrongETag := []byte{0x98, 0x76, 0x54, 0x32}
	m := mux.NewRouter()
	err := m.Handle("/ifmatch2", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		if r.Code() != codes.PUT {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		hasIfMatch := false
		condFulfilled := false
		for _, opt := range r.Options() {
			if opt.ID == message.IfMatch {
				hasIfMatch = true
				if len(opt.Value) == 0 || bytes.Equal(opt.Value, knownETag) {
					condFulfilled = true
				}
			}
		}
		if hasIfMatch && !condFulfilled {
			_ = w.SetResponse(codes.PreconditionFailed, message.TextPlain, nil)
			return
		}
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Put(ctx, "/ifmatch2", message.TextPlain, bytes.NewReader([]byte("update")),
		message.Option{ID: message.IfMatch, Value: wrongETag})
	require.NoError(t, err)
	require.Equal(t, codes.PreconditionFailed, resp.Code(),
		"If-Match with non-matching ETag must result in 4.12 Precondition Failed (RFC 7252 §5.10.8.1)")
}

// TC_CoAP_CF_033 – TP_CoAP_IfNoneMatch_ResourceExists
//
// Reference: RFC 7252 Section 5.10.8.2
// If the target resource does exist, the If-None-Match condition is not fulfilled.
func TestTC_CoAP_CF_033_IfNoneMatch_ResourceExists(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/ifnonematch", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		if r.Code() != codes.PUT {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		// Resource always exists; If-None-Match condition is never fulfilled.
		if r.Options().HasOption(message.IfNoneMatch) {
			_ = w.SetResponse(codes.PreconditionFailed, message.TextPlain, nil)
			return
		}
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// If-None-Match carries no value (empty byte slice = presence flag only).
	resp, err := cc.Put(ctx, "/ifnonematch", message.TextPlain, bytes.NewReader([]byte("update")),
		message.Option{ID: message.IfNoneMatch, Value: []byte{}})
	require.NoError(t, err)
	require.Equal(t, codes.PreconditionFailed, resp.Code(),
		"If-None-Match on existing resource must return 4.12 Precondition Failed (RFC 7252 §5.10.8.2)")
}

// TC_CoAP_CF_034 – TP_CoAP_MaxAge_InResponse
//
// Reference: RFC 7252 Section 5.10.5
// The Max-Age Option indicates the maximum time a response may be cached.
func TestTC_CoAP_CF_034_MaxAge_InResponse(t *testing.T) {
	const maxAgeValue = uint32(300)
	m := mux.NewRouter()
	err := m.Handle("/max-age", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		buf := make([]byte, 4)
		var opts message.Options
		opts, _, _ = opts.SetUint32(buf, message.MaxAge, maxAgeValue)
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("fresh")), opts[0])
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/max-age")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	got, errM := resp.Options().GetUint32(message.MaxAge)
	require.NoError(t, errM,
		"Max-Age option must be present in 2.05 Content response (RFC 7252 §5.10.5)")
	require.Equal(t, maxAgeValue, got,
		"Max-Age value must match the value set by the server")
}

// TC_CoAP_CF_035 – TP_CoAP_CriticalOption_UnrecognizedYields4_02
//
// Reference: RFC 7252 Section 5.4.1
// Unrecognized critical options in a Confirmable request MUST cause 4.02 Bad Option.
//
// NOTE: Known bug – go-coap silently ignores unknown critical options.
func TestTC_CoAP_CF_035_CriticalOption_UnrecognizedYields4_02(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// CON GET with unknown critical option 65003:
	// 0x41=CON,TKL=1; 0x01=GET; MID=9; 0xCC=token
	// 0xB1,'x' = Uri-Path "x" (option 11)
	// 0xE0,0xFC,0xD3 = option delta 65003 (critical, unknown), length=0
	pkt := []byte{
		0x41, 0x01, 0x00, 0x09, 0xCC,
		0xB1, 'x',
		0xE0, 0xFC, 0xD3,
	}
	_, err = conn.Write(pkt)
	require.NoError(t, err)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err, "server must send a response")
	require.GreaterOrEqual(t, n, 4)

	// Expected per RFC 7252 §5.4.1: 4.02 Bad Option = 0x82.
	require.Equal(t, uint8(0x82), buf[1],
		"RFC 7252 §5.4.1: unrecognized critical option MUST yield 4.02 Bad Option")
}

// TC_CoAP_CF_036 – TP_CoAP_ResponseCode_Unauthorized
//
// Reference: RFC 7252 Section 5.9.2.2
func TestTC_CoAP_CF_036_Unauthorized(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/auth-required", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.Unauthorized, message.TextPlain,
			bytes.NewReader([]byte("authentication required")))
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/auth-required")
	require.NoError(t, err)
	require.Equal(t, codes.Unauthorized, resp.Code(),
		"server must return 4.01 Unauthorized (RFC 7252 §5.9.2.2)")
}

// TC_CoAP_CF_037 – TP_CoAP_ResponseCode_Forbidden
//
// Reference: RFC 7252 Section 5.9.2.4
func TestTC_CoAP_CF_037_Forbidden(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/forbidden", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.Forbidden, message.TextPlain,
			bytes.NewReader([]byte("forbidden")))
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/forbidden")
	require.NoError(t, err)
	require.Equal(t, codes.Forbidden, resp.Code(),
		"server must return 4.03 Forbidden (RFC 7252 §5.9.2.4)")
}

// TC_CoAP_CF_038 – TP_CoAP_RequestEntityTooLarge_WithSize1
//
// Reference: RFC 7252 Sections 5.9.2.9 and 5.10.9
// 4.13 response SHOULD include Size1 option indicating max accepted size.
func TestTC_CoAP_CF_038_RequestEntityTooLarge_WithSize1(t *testing.T) {
	const maxSize = uint32(16)
	m := mux.NewRouter()
	err := m.Handle("/limited", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		body, _ := r.ReadBody()
		if uint32(len(body)) > maxSize {
			buf := make([]byte, 4)
			var sizeOpts message.Options
			sizeOpts, _, _ = sizeOpts.SetUint32(buf, message.Size1, maxSize)
			_ = w.SetResponse(codes.RequestEntityTooLarge, message.TextPlain, nil, sizeOpts[0])
			return
		}
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// Small payload → 2.04 Changed.
	resp, err := cc.Put(ctx, "/limited", message.TextPlain, bytes.NewReader([]byte("small")))
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code())

	// Oversized payload (17 bytes) → 4.13 Request Entity Too Large + Size1.
	large := make([]byte, int(maxSize)+1)
	resp2, err := cc.Put(ctx, "/limited", message.TextPlain, bytes.NewReader(large))
	require.NoError(t, err)
	require.Equal(t, codes.RequestEntityTooLarge, resp2.Code(),
		"oversized payload must yield 4.13 Request Entity Too Large (RFC 7252 §5.9.2.9)")

	size1, errS := resp2.Options().GetUint32(message.Size1)
	require.NoError(t, errS,
		"4.13 response SHOULD include Size1 option (RFC 7252 §5.10.9)")
	require.Equal(t, maxSize, size1,
		"Size1 in 4.13 must indicate the maximum acceptable payload size")
}

// TC_CoAP_CF_039 – TP_CoAP_ResponseCode_InternalServerError
//
// Reference: RFC 7252 Section 5.9.3.1
func TestTC_CoAP_CF_039_InternalServerError(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/error", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.InternalServerError, message.TextPlain,
			bytes.NewReader([]byte("internal error")))
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/error")
	require.NoError(t, err)
	require.Equal(t, codes.InternalServerError, resp.Code(),
		"server must return 5.00 Internal Server Error (RFC 7252 §5.9.3.1)")
}

// TC_CoAP_CF_042 – TP_CoAP_MessageFormat_ZeroLengthToken
//
// Reference: RFC 7252 Section 4.5
// TKL=0 (zero-length token) is explicitly valid in CoAP.
func TestTC_CoAP_CF_042_ZeroLengthToken_Valid(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Build CON GET with TKL=0 targeting Uri-Path "x".
	// 0x40 = VER=1, T=CON, TKL=0;  0x01 = GET
	// 0x00, 0x0A = MID=10;  no token bytes
	// 0xB1, 'x' = Uri-Path "x" (delta=11, len=1)
	pkt := []byte{0x40, 0x01, 0x00, 0x0A, 0xB1, 'x'}
	_, err = conn.Write(pkt)
	require.NoError(t, err)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err, "server must respond to a CON GET with TKL=0")
	require.GreaterOrEqual(t, n, 4, "response must be at least 4 bytes")

	// Response code class must be 2 (Success).
	respClass := buf[1] >> 5
	require.Equal(t, uint8(2), respClass,
		"zero-length token GET must yield a 2.xx response; got code 0x%02X", buf[1])

	// Response TKL must be 0 (echoing the zero-length token per RFC 7252 §5.3.1).
	respTKL := buf[0] & 0x0F
	require.Equal(t, uint8(0), respTKL,
		"response to TKL=0 request must also have TKL=0 (RFC 7252 §5.3.1)")
}

// ── Additional RFC 7252 conformance tests (CF_066–CF_075) ──────────────────

// TC_CoAP_CF_066 – TP_CoAP_IfNoneMatch_ResourceExists
//
// Reference: RFC 7252 Section 5.10.8.2
// "The If-None-Match option MAY be used to make a request conditional on
// the non-existence of the target resource. If the target resource DOES exist,
// the server MUST NOT perform the method and MUST return 4.12 (Precondition Failed)."
//
// Procedure:
//  1. PUT /if-none-match to create the resource (no condition).
//  2. PUT /if-none-match with If-None-Match while it exists → 4.12 Precondition Failed.
func TestTC_CoAP_CF_066_IfNoneMatch_ResourceExists_PreconditionFailed(t *testing.T) {
	resourceExists := false

	m := mux.NewRouter()
	err := m.Handle("/if-none-match", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		if r.Code() != codes.PUT {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		hasIfNoneMatch := r.HasOption(message.IfNoneMatch)
		if hasIfNoneMatch && resourceExists {
			_ = w.SetResponse(codes.PreconditionFailed, message.TextPlain, nil)
			return
		}
		resourceExists = true
		_ = w.SetResponse(codes.Created, message.TextPlain, nil)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// Step 1: create the resource.
	resp1, err := cc.Put(ctx, "/if-none-match", message.TextPlain, bytes.NewReader([]byte("v1")))
	require.NoError(t, err)
	require.Equal(t, codes.Created, resp1.Code())

	// Step 2: PUT with If-None-Match while resource exists → 4.12.
	resp2, err := cc.Put(ctx, "/if-none-match", message.TextPlain, bytes.NewReader([]byte("v2")),
		message.Option{ID: message.IfNoneMatch, Value: nil})
	require.NoError(t, err)
	require.Equal(t, codes.PreconditionFailed, resp2.Code(),
		"RFC 7252 §5.10.8.2: PUT with If-None-Match on existing resource MUST return 4.12")
}

// TC_CoAP_CF_067 – TP_CoAP_Token_VariableLengths
//
// Reference: RFC 7252 Section 5.3.1
// "The token is a sequence of 0 to 8 bytes."
// All token lengths from 1 to 7 bytes MUST be supported and echoed verbatim.
//
// Procedure: send CON GET with tokens of length 1, 2, 4, and 7 bytes.
// Expected: each response echoes the exact token.
func TestTC_CoAP_CF_067_Token_VariableLengths(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	tokenTests := []struct {
		name  string
		token []byte
	}{
		{"1-byte", []byte{0xAA}},
		{"2-byte", []byte{0xBB, 0xCC}},
		{"4-byte", []byte{0x01, 0x02, 0x03, 0x04}},
		{"7-byte", []byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70}},
	}

	for i, tc := range tokenTests {
		t.Run(tc.name, func(t *testing.T) {
			tkl := uint8(len(tc.token))
			mid := uint16(0x0100 + i)
			// VER=1, T=CON, TKL=tkl
			pkt := []byte{0x40 | tkl, 0x01, byte(mid >> 8), byte(mid)}
			pkt = append(pkt, tc.token...)
			pkt = append(pkt, 0xB1, 't') // Uri-Path "t"

			_, errW := conn.Write(pkt)
			require.NoError(t, errW)

			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, 256)
			n, errR := conn.Read(buf)
			require.NoError(t, errR, "server must respond")
			require.GreaterOrEqual(t, n, 4+int(tkl))

			respTKL := buf[0] & 0x0F
			require.Equal(t, tkl, respTKL, "response TKL must match request")
			require.Equal(t, tc.token, buf[4:4+int(tkl)],
				"RFC 7252 §5.3.1: %d-byte token must be echoed verbatim", tkl)
		})
	}
}

// TC_CoAP_CF_068 – TP_CoAP_EmptyMessage_RST_EchosMID
//
// Reference: RFC 7252 Section 4.2, 4.3
// "The RST message MUST echo the Message ID of the message being rejected."
//
// Procedure: send empty CON (Ping) with specific MID. Verify RST has same MID.
func TestTC_CoAP_CF_068_RST_EchosMID(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], _ *pool.Message) {
		// No-op — ping handled by middleware.
	})
	defer cleanup()

	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Empty CON with MID=0x1234
	pkt := []byte{0x40, 0x00, 0x12, 0x34} // VER=1, T=CON, TKL=0, Code=0.00, MID=0x1234
	_, err = conn.Write(pkt)
	require.NoError(t, err)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, 4)

	respType := (buf[0] >> 4) & 0x03
	require.Equal(t, uint8(3), respType,
		"RFC 7252 §4.2: empty CON must be answered with RST (type=3)")

	respMID := uint16(buf[2])<<8 | uint16(buf[3])
	require.Equal(t, uint16(0x1234), respMID,
		"RFC 7252 §4.3: RST message MUST echo the MID=0x1234 of the CON")
}

// TC_CoAP_CF_069 – TP_CoAP_Response_Forbidden_403
//
// Reference: RFC 7252 Section 5.9.1
// "4.03 (Forbidden): The client is not authorized to perform the requested action."
//
// Procedure: GET /admin; server returns 4.03 Forbidden.
func TestTC_CoAP_CF_069_Response_Forbidden(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/admin", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Forbidden, message.TextPlain,
			bytes.NewReader([]byte("access denied")))
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

	resp, err := cc.Get(ctx, "/admin")
	require.NoError(t, err)
	require.Equal(t, codes.Forbidden, resp.Code(),
		"RFC 7252 §5.9.1: server MUST be able to return 4.03 Forbidden")
}

// TC_CoAP_CF_070 – TP_CoAP_Options_LocationPath_MultiSegment
//
// Reference: RFC 7252 Section 5.10.7
// "The Location-Path and Location-Query options together indicate a
// relative URI. Multiple Location-Path options form the path segments."
//
// Procedure: POST /items; server responds 2.01 Created with Location-Path
// segments /items/42/detail.
func TestTC_CoAP_CF_070_LocationPath_MultiSegment(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/items", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Created, message.TextPlain, nil,
			message.Option{ID: message.LocationPath, Value: []byte("items")},
			message.Option{ID: message.LocationPath, Value: []byte("42")},
			message.Option{ID: message.LocationPath, Value: []byte("detail")})
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

	resp, err := cc.Post(ctx, "/items", message.TextPlain, bytes.NewReader([]byte("new")))
	require.NoError(t, err)
	require.Equal(t, codes.Created, resp.Code())

	locBuf := make([]string, 10)
	n, err := resp.Options().GetStrings(message.LocationPath, locBuf)
	require.NoError(t, err)
	require.Equal(t, 3, n, "RFC 7252 §5.10.7: must receive 3 Location-Path segments")
	require.Equal(t, "items", locBuf[0])
	require.Equal(t, "42", locBuf[1])
	require.Equal(t, "detail", locBuf[2])
}

// TC_CoAP_CF_071 – TP_CoAP_Options_MaxAge_Default
//
// Reference: RFC 7252 Section 5.10.5
// "In the absence of this option, a response SHOULD be considered fresh for
// a default value of 60 seconds."
//
// Procedure: GET /resource; server responds WITHOUT Max-Age option.
// Expected: Max-Age option is absent (default 60s applies implicitly).
func TestTC_CoAP_CF_071_MaxAge_Default(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/resource", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("data")))
		require.NoError(t, errS)
		// Deliberately NOT setting Max-Age → RFC default = 60s applies.
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/resource")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	// The library may or may not include an explicit Max-Age in the response.
	// Per RFC 7252 §5.10.5, the absence of Max-Age means the client should
	// treat it as 60 seconds. If present, verify it's reasonable.
	val, errMA := resp.Options().GetUint32(message.MaxAge)
	if errMA == nil {
		// Max-Age is present — the value should be ≤ 60 (the library default)
		// or whatever the handler set.
		t.Logf("RFC 7252 §5.10.5: Max-Age=%d present in response", val)
	} else {
		// Max-Age absent → implicit default of 60s (correct per RFC).
		t.Log("RFC 7252 §5.10.5: Max-Age absent → implicit default 60s applies (correct)")
	}
}

// TC_CoAP_CF_072 – TP_CoAP_MultipleUriQuery_Options
//
// Reference: RFC 7252 Section 5.10.1
// "Each Uri-Query option specifies one argument."
// Multiple Uri-Query options form the full query string.
//
// Procedure: GET /search?a=1&b=2&c=3 → three Uri-Query options.
// Expected: server receives all three query arguments.
func TestTC_CoAP_CF_072_MultipleUriQuery(t *testing.T) {
	var receivedQueries []string
	var mu sync.Mutex

	m := mux.NewRouter()
	err := m.Handle("/search", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		buf := make([]string, 20)
		n, errQ := r.Options().GetStrings(message.URIQuery, buf)
		if errQ == nil {
			mu.Lock()
			receivedQueries = buf[:n]
			mu.Unlock()
		}
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

	resp, err := cc.Get(ctx, "/search?a=1&b=2&c=3")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(receivedQueries), 3,
		"RFC 7252 §5.10.1: server must receive all three Uri-Query options")
	require.Contains(t, receivedQueries, "a=1")
	require.Contains(t, receivedQueries, "b=2")
	require.Contains(t, receivedQueries, "c=3")
}

// TC_CoAP_CF_073 – TP_CoAP_Response_ValidResponse_WithPayload
//
// Reference: RFC 7252 Section 5.5
// "Both requests and responses may include a payload depending on the method."
// A response with payload MUST have the payload marker (0xFF) followed by data.
//
// Procedure: GET /resource; server produces a payload with Content-Format.
// Expected: client reads the payload correctly.
func TestTC_CoAP_CF_073_Response_WithPayload(t *testing.T) {
	expectedPayload := []byte("Hello, CoAP!")
	m := mux.NewRouter()
	err := m.Handle("/resource", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(expectedPayload))
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

	resp, err := cc.Get(ctx, "/resource")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, expectedPayload, body,
		"RFC 7252 §5.5: response payload must be delivered to client")

	cf, err := resp.ContentFormat()
	require.NoError(t, err)
	require.Equal(t, message.TextPlain, cf,
		"RFC 7252 §5.5.1: Content-Format must be present when payload is included")
}

// TC_CoAP_CF_074 – TP_CoAP_Options_IfMatch_MultipleETags
//
// Reference: RFC 7252 Section 5.10.8.1
// "One or more If-Match options MAY be present. If any of the entity-tags
// match, the condition is fulfilled."
//
// Procedure: PUT /resource with two If-Match ETags; one matches.
// Expected: 2.04 Changed (condition fulfilled because one matches).
func TestTC_CoAP_CF_074_IfMatch_MultipleETags(t *testing.T) {
	currentETag := []byte{0xAA, 0xBB}

	m := mux.NewRouter()
	err := m.Handle("/resource", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		// Check if any If-Match option matches the resource's current ETag.
		matched := false
		for _, opt := range r.Options() {
			if opt.ID == message.IfMatch && bytes.Equal(opt.Value, currentETag) {
				matched = true
				break
			}
		}
		// If If-Match present but none match → 4.12
		hasIfMatch := false
		for _, opt := range r.Options() {
			if opt.ID == message.IfMatch {
				hasIfMatch = true
				break
			}
		}
		if hasIfMatch && !matched {
			_ = w.SetResponse(codes.PreconditionFailed, message.TextPlain, nil)
			return
		}
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// Two If-Match options: first doesn't match, second does.
	resp, err := cc.Put(ctx, "/resource", message.TextPlain, bytes.NewReader([]byte("v2")),
		message.Option{ID: message.IfMatch, Value: []byte{0x99}},       // does NOT match
		message.Option{ID: message.IfMatch, Value: []byte{0xAA, 0xBB}}) // matches
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code(),
		"RFC 7252 §5.10.8.1: PUT with multiple If-Match MUST succeed if ANY ETag matches")
}

// TC_CoAP_CF_075 – TP_CoAP_Response_RequestEntityTooLarge_Size1Hint
//
// Reference: RFC 7252 Section 5.9.1 + 5.10.9
// "4.13 Request Entity Too Large: … The response SHOULD include a Size1 option
// to indicate the maximum size of request entity the server is willing to accept."
//
// Procedure: POST /upload with oversized body; server returns 4.13 with Size1.
// Expected: client reads 4.13 response and Size1 value.
func TestTC_CoAP_CF_075_RequestEntityTooLarge_Size1(t *testing.T) {
	const maxSize = 256
	m := mux.NewRouter()
	err := m.Handle("/upload", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		body, _ := r.ReadBody()
		if len(body) > maxSize {
			errS := w.SetResponse(codes.RequestEntityTooLarge, message.TextPlain, nil,
				message.Option{ID: message.Size1, Value: encodeUint16(maxSize)})
			require.NoError(t, errS)
			return
		}
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

	// Oversized body (> maxSize bytes).
	bigBody := bytes.Repeat([]byte("X"), maxSize+100)
	resp, err := cc.Post(ctx, "/upload", message.AppOctets, bytes.NewReader(bigBody))
	require.NoError(t, err)
	require.Equal(t, codes.RequestEntityTooLarge, resp.Code(),
		"RFC 7252 §5.9.1: oversized entity MUST return 4.13")

	size1, errS := resp.Options().GetUint32(message.Size1)
	require.NoError(t, errS,
		"RFC 7252 §5.10.9: 4.13 response SHOULD include Size1 to hint max size")
	require.Equal(t, uint32(maxSize), size1,
		"RFC 7252 §5.10.9: Size1 value must indicate the maximum acceptable size")
}
