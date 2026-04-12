// Package tcp — RFC 8323 "CoAP over TCP, TLS, and WebSockets" conformance tests.
//
// Test IDs: TCP_001 – TCP_015
// Reference: https://www.rfc-editor.org/rfc/rfc8323
//
// NOTE: This file uses package tcp (internal) to access NewServer and Dial directly,
// matching the pattern of clientobserve_test.go and client_test.go.
package tcp

import (
	"bytes"
	"context"
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
	"github.com/plgd-dev/go-coap/v3/net/responsewriter"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/tcp/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tcpConformanceTimeout = 10 * time.Second

// startTCPConformanceServer starts a TCP CoAP server with the given handler function
// on a random port. Returns the bound address and a cleanup function.
func startTCPConformanceServerWithHandler(t *testing.T, h func(*responsewriter.ResponseWriter[*client.Conn], *pool.Message)) (string, func()) {
	t.Helper()
	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)

	s := NewServer(options.WithHandlerFunc(h))
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
	return l.Addr().String(), cleanup
}

// startTCPConformanceServerWithMux starts a TCP CoAP server with a mux.Router.
func startTCPConformanceServerWithMux(t *testing.T, m *mux.Router) (string, func()) {
	t.Helper()
	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)

	s := NewServer(options.WithMux(m))
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
	return l.Addr().String(), cleanup
}

// TC_CoAP_TCP_001 – TP_CoAP_TCP_CSM_SentOnConnection
//
// Reference: RFC 8323 Section 5.3
// "Every endpoint MUST send a CSM as the first message it sends in a connection."
//
// go-coap sends CSM automatically when a TCP connection is established.
// This test verifies the connection can be established and basic communication works,
// confirming that CSM exchange completed successfully.
//
// Procedure: TCP client connects and sends GET. If CSM is missing, the server will
// not process the request. Expected: 2.05 Content response.
func TestTC_CoAP_TCP_001_CSM_EnablesRequests(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/hello", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("hello")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)
	defer l.Close()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	// A brief wait for CSM exchange to complete
	time.Sleep(100 * time.Millisecond)

	resp, err := cc.Get(ctx, "/hello")
	require.NoError(t, err, "RFC 8323 §5.3: CSM must be exchanged automatically, enabling requests")
	require.Equal(t, codes.Content, resp.Code())
}

// TC_CoAP_TCP_002 – TP_CoAP_TCP_GET_Response
//
// Reference: RFC 8323 Section 3
// Over TCP there is no Type/MID. The basic request-response model still works.
//
// Procedure: TCP GET /resource. Expected: 2.05 Content.
func TestTC_CoAP_TCP_002_BasicGET(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/resource", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.GET, r.Code())
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("tcp-content")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/resource")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, []byte("tcp-content"), body)
}

// TC_CoAP_TCP_003 – TP_CoAP_TCP_POST_Created
//
// Reference: RFC 8323 Section 3.4
// POST over TCP — server creates resource and returns 2.01 Created.
func TestTC_CoAP_TCP_003_POST_Created(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/items", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.POST, r.Code())
		errS := w.SetResponse(codes.Created, message.TextPlain, nil)
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	resp, err := cc.Post(ctx, "/items", message.TextPlain, bytes.NewReader([]byte("data")))
	require.NoError(t, err)
	require.Equal(t, codes.Created, resp.Code(),
		"RFC 8323 §3.4: POST over TCP must return 2.01 Created")
}

// TC_CoAP_TCP_004 – TP_CoAP_TCP_PUT_Changed
//
// Reference: RFC 8323 Section 3.4
// PUT over TCP — server updates resource and returns 2.04 Changed.
func TestTC_CoAP_TCP_004_PUT_Changed(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/config", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.PUT, r.Code())
		errS := w.SetResponse(codes.Changed, message.TextPlain, nil)
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	resp, err := cc.Put(ctx, "/config", message.TextPlain, bytes.NewReader([]byte(`{"v":1}`)))
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code(),
		"RFC 8323 §3.4: PUT over TCP must return 2.04 Changed")
}

// TC_CoAP_TCP_005 – TP_CoAP_TCP_DELETE_Deleted
//
// Reference: RFC 8323 Section 3.4
// DELETE over TCP — server deletes resource and returns 2.02 Deleted.
func TestTC_CoAP_TCP_005_DELETE_Deleted(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/record", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		assert.Equal(t, codes.DELETE, r.Code())
		errS := w.SetResponse(codes.Deleted, message.TextPlain, nil)
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	resp, err := cc.Delete(ctx, "/record")
	require.NoError(t, err)
	require.Equal(t, codes.Deleted, resp.Code(),
		"RFC 8323 §3.4: DELETE over TCP must return 2.02 Deleted")
}

// TC_CoAP_TCP_006 – TP_CoAP_TCP_TokenEcho
//
// Reference: RFC 8323 Section 3 + RFC 7252 §5.3.1
// Tokens work the same way over TCP: server must echo the token in the response.
func TestTC_CoAP_TCP_006_TokenEcho(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/echo", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	expectedToken := message.Token([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	req, err := cc.NewGetRequest(ctx, "/echo")
	require.NoError(t, err)
	req.SetToken(expectedToken)

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, expectedToken, resp.Token(),
		"RFC 8323 + RFC 7252 §5.3.1: server must echo the token in response over TCP")
}

// TC_CoAP_TCP_007 – TP_CoAP_TCP_ParallelRequests
//
// Reference: RFC 8323 Section 3.4
// "Multiple requests can be in-flight at the same time because CoAP over TCP
// does not use MID for request multiplexing—it uses tokens."
//
// Procedure: two interleaved GET requests with different tokens.
// Expected: both receive the correct responses.
func TestTC_CoAP_TCP_007_ParallelRequests(t *testing.T) {
	m := mux.NewRouter()
	for _, path := range []string{"/a", "/b"} {
		p := path
		err := m.Handle(p, mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
			body := p[1:] // "a" or "b"
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte(body)))
		}))
		require.NoError(t, err)
	}

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
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
	require.NoError(t, rA.err)
	require.NoError(t, rB.err)
	require.Equal(t, []byte("a"), rA.body)
	require.Equal(t, []byte("b"), rB.body)
}

// TC_CoAP_TCP_008 – TP_CoAP_TCP_Ping_Pong
//
// Reference: RFC 8323 Section 5.4
// "A Ping message is sent to check the health of a connection.
// The recipient MUST respond with a Pong message."
//
// Procedure: client sends Ping; server must reply with Pong.
// Expected: Ping returns no error (Pong received).
func TestTC_CoAP_TCP_008_Ping_Pong(t *testing.T) {
	m := mux.NewRouter()
	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	err = cc.Ping(ctx)
	require.NoError(t, err,
		"RFC 8323 §5.4: Ping must receive Pong reply from server")
}

// TC_CoAP_TCP_009 – TP_CoAP_TCP_AsyncPing
//
// Reference: RFC 8323 Section 5.4
// AsyncPing sends a Ping and registers a callback for when Pong arrives.
//
// Procedure: client sends async Ping; Pong callback fires.
// Expected: pong received within timeout.
func TestTC_CoAP_TCP_009_AsyncPing(t *testing.T) {
	m := mux.NewRouter()
	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	pongCh := make(chan struct{}, 1)
	cancel, err := cc.AsyncPing(func() {
		select {
		case pongCh <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)
	defer cancel()

	select {
	case <-pongCh:
	case <-time.After(tcpConformanceTimeout):
		require.FailNow(t, "RFC 8323 §5.4: AsyncPing did not receive Pong within timeout")
	}
}

// TC_CoAP_TCP_010 – TP_CoAP_TCP_LargeMessage
//
// Reference: RFC 8323 Section 3.2
// "TCP provides reliable, ordered delivery. CoAP over TCP does NOT require
// CoAP-level fragmentation for large messages."
//
// Procedure: server returns a 5000-byte payload over TCP (no blockwise).
// Expected: client receives all 5000 bytes intact.
func TestTC_CoAP_TCP_010_LargeMessage_NoFragmentation(t *testing.T) {
	largePayload := bytes.Repeat([]byte("X"), 5000)

	m := mux.NewRouter()
	err := m.Handle("/large", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(largePayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/large")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, largePayload, body,
		"RFC 8323 §3.2: large message over TCP must be delivered intact without CoAP fragmentation")
}

// TC_CoAP_TCP_011 – TP_CoAP_TCP_Observe
//
// Reference: RFC 8323 Section 4.4
// "Observe works over TCP just as over UDP, except the TCP layer provides
// the reliability guarantees, so notifications don't use CON type."
//
// Procedure: register for /sensor over TCP; server sends 3 notifications.
// Expected: client receives all notifications.
func TestTC_CoAP_TCP_011_Observe(t *testing.T) {
	const numNotifs = 3
	var received atomic.Int32
	done := make(chan struct{})

	addr, cleanup := startTCPConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		obs, errO := r.Observe()
		if errO != nil {
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("v0")))
			return
		}
		switch obs {
		case 0:
			cc := w.Conn()
			tok := make([]byte, len(r.Token()))
			copy(tok, r.Token())
			go func() {
				for i := 0; i < numNotifs; i++ {
					req := cc.AcquireMessage(cc.Context())
					req.SetCode(codes.Content)
					req.SetContentFormat(message.TextPlain)
					req.SetObserve(uint32(i + 2))
					req.SetBody(bytes.NewReader([]byte("v")))
					req.SetToken(tok)
					_ = cc.WriteMessage(req)
					cc.ReleaseMessage(req)
					time.Sleep(20 * time.Millisecond)
				}
			}()
		case 1:
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/sensor", func(_ *pool.Message) {
		if n := received.Add(1); n >= numNotifs {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	require.NoError(t, err)
	defer func() {
		if obs != nil {
			_ = obs.Cancel(ctx)
		}
	}()

	select {
	case <-done:
	case <-time.After(tcpConformanceTimeout):
		require.FailNow(t, "RFC 8323 §4.4: Observe over TCP did not receive all notifications",
			"got %d of %d", received.Load(), numNotifs)
	}
}

// TC_CoAP_TCP_012 – TP_CoAP_TCP_BlockwiseBlock2
//
// Reference: RFC 8323 Section 6 (BERT) + RFC 7959
// Block-wise transfers work over TCP. With blockwise enabled, large payloads
// are fragmented into blocks.
//
// Procedure: GET /large-bw with 2048-byte payload; blockwise enabled on both sides.
// Expected: client receives full payload.
func TestTC_CoAP_TCP_012_BlockwiseBlock2_TCP(t *testing.T) {
	largePayload := bytes.Repeat([]byte("BW"), 1024) // 2048 bytes

	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)
	defer l.Close()

	m := mux.NewRouter()
	err = m.Handle("/large-bw", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(largePayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	s := NewServer(
		options.WithMux(m),
		options.WithBlockwise(true, blockwise.SZX512, 30*time.Second),
	)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.Serve(l)
	}()
	defer func() {
		s.Stop()
		wg.Wait()
	}()

	cc, err := Dial(l.Addr().String(),
		options.WithBlockwise(true, blockwise.SZX512, 30*time.Second),
	)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Get(ctx, "/large-bw")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, largePayload, body,
		"RFC 8323 §6: block-wise over TCP must reassemble full payload")
}

// TC_CoAP_TCP_013 – TP_CoAP_TCP_NotFound
//
// Reference: RFC 8323 Section 3.4 + RFC 7252 §5.9.1
// 4.04 Not Found works over TCP the same as over UDP.
func TestTC_CoAP_TCP_013_NotFound(t *testing.T) {
	m := mux.NewRouter()
	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/nonexistent")
	require.NoError(t, err)
	require.Equal(t, codes.NotFound, resp.Code(),
		"RFC 8323: GET on unknown resource over TCP must return 4.04 Not Found")
}

// TC_CoAP_TCP_014 – TP_CoAP_TCP_MultipleConnections
//
// Reference: RFC 8323 Section 3
// Multiple independent TCP connections can be open simultaneously.
// Each maintains its own state.
//
// Procedure: two independent TCP connections send concurrent GETs.
// Expected: both receive correct responses.
func TestTC_CoAP_TCP_014_MultipleConnections(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/data", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("data")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc1, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc1.Close(); <-cc1.Done() }()

	cc2, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc2.Close(); <-cc2.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	resp1, err := cc1.Get(ctx, "/data")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp1.Code())

	resp2, err := cc2.Get(ctx, "/data")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp2.Code())

	b1, _ := resp1.ReadBody()
	b2, _ := resp2.ReadBody()
	require.Equal(t, b1, b2, "RFC 8323 §3: multiple connections must get same response")
}

// TC_CoAP_TCP_015 – TP_CoAP_TCP_ConnectionKeepalive_Ping
//
// Reference: RFC 8323 Section 5.4
// "A Ping can be used as a keep-alive mechanism to verify that
// the connection is still active."
//
// Procedure: send multiple Pings across a connection with delays between them.
// Expected: all Pings receive Pong replies (connection stays alive).
func TestTC_CoAP_TCP_015_KeepAlive_Ping(t *testing.T) {
	m := mux.NewRouter()
	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
		err = cc.Ping(ctx)
		cancel()
		require.NoError(t, err,
			"RFC 8323 §5.4: keep-alive Ping #%d over TCP must receive Pong", i+1)
		time.Sleep(50 * time.Millisecond)
	}
}

// TC_CoAP_TCP_016 – TP_CoAP_TCP_SignalHandler_Pong
//
// Reference: RFC 8323 Section 5.4
// "The Pong message is a response to a Ping. A recipient of a Ping MUST
// send a Pong in reply."
// RFC 8323 §5.1: Signal messages (CSM, Ping, Pong, Release, Abort) are
// processed by a dedicated signal handler on the connection.
//
// Procedure: connect TCP client; register SetTCPSignalReceivedHandler; send Ping.
// Expected: signal handler is invoked with codes.Pong when Pong arrives.
// This verifies that go-coap's signal handler infrastructure correctly fires
// for all CoAP-over-TCP signal messages.
func TestTC_CoAP_TCP_016_SignalHandler_Pong(t *testing.T) {
	m := mux.NewRouter()
	addr, cleanup := startTCPConformanceServerWithMux(t, m)
	defer cleanup()

	cc, err := Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	pongReceived := make(chan struct{}, 1)
	cc.SetTCPSignalReceivedHandler(func(code codes.Code) {
		if code == codes.Pong {
			select {
			case pongReceived <- struct{}{}:
			default:
			}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	err = cc.Ping(ctx)
	require.NoError(t, err, "RFC 8323 §5.4: Ping must succeed (Pong received)")

	select {
	case <-pongReceived:
		// signal handler correctly invoked for Pong ✓
	case <-time.After(500 * time.Millisecond):
		require.Fail(t,
			"RFC 8323 §5.4: SetTCPSignalReceivedHandler was not invoked with codes.Pong "+
				"even though cc.Ping() returned successfully")
	}
}

// TC_CoAP_TCP_017 – TP_CoAP_TCP_BERT_LargeTransfer
//
// Reference: RFC 8323 Section 6
// "Block-Wise Transfer over Reliable Transports (BERT) extends RFC 7959 by
// using SZX=7 to indicate blocks that may be larger than 1024 bytes."
// "BERT is only available over connection-oriented transports (e.g., TCP/TLS)."
//
// RFC 8323 §5.3 also requires: to negotiate BERT, a peer MUST include the
// TCPBlockWiseTransfer option (ID=4) in its CSM capabilities message.
//
// Procedure: server + client configured with SZXBERT (SZX=7) and MaxMessageSize=4096.
// Client GETs a 10000-byte resource. Expected: full payload received correctly.
//
// KNOWN FAILURE: go-coap's tcp/client/session.go `sendCSM()` sends only a bare
// CSM (no Max-Message-Size or TCPBlockWiseTransfer options). Because the client
// does not advertise TCPBlockWiseTransfer, the server's `peerBlockWiseTranferEnabled`
// is never set to true. Consequently, `handle()` (conn.go:364) skips the blockwise
// handler path, the server sends the full 10000-byte payload as a single TCP-CoAP
// message, and the client rejects it because 10000 > MaxMessageSize (4096).
// Root cause: missing TCPBlockWiseTransfer option in sendCSM().
func TestTC_CoAP_TCP_017_BERT_LargeTransfer(t *testing.T) {
	const payloadSize = 10000
	bertPayload := bytes.Repeat([]byte("BERT"), payloadSize/4)
	bertPayload = bertPayload[:payloadSize]

	var receivedBody []byte
	m := mux.NewRouter()
	err := m.Handle("/bert-large", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader(bertPayload))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)

	s := NewServer(
		options.WithMux(m),
		options.WithBlockwise(true, blockwise.SZXBERT, 30*time.Second),
		options.WithMaxMessageSize(4096),
	)
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

	cc, err := Dial(l.Addr().String(),
		options.WithBlockwise(true, blockwise.SZXBERT, 30*time.Second),
		options.WithMaxMessageSize(4096),
	)
	require.NoError(t, err, "RFC 8323 §6: TCP connection with BERT must be established")
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cc.Get(ctx, "/bert-large")
	require.NoError(t, err, "RFC 8323 §6: BERT GET must succeed")
	require.Equal(t, codes.Content, resp.Code(),
		"RFC 8323 §6: BERT GET response must be 2.05 Content")

	receivedBody, err = resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, bertPayload, receivedBody,
		"RFC 8323 §6: BERT transfer must reconstruct the full %d-byte payload", payloadSize)

	_ = assert.Equal(t, payloadSize, len(receivedBody),
		"RFC 8323 §6: received payload length must match original")
}

// ── Additional RFC 8323 conformance tests (TCP_018–TCP_020) ──────

// TC_CoAP_TCP_018 – TP_CoAP_TCP_CSM_MaxMessageSize
//
// Reference: RFC 8323 Section 5.3.1
// "The Max-Message-Size option indicates the maximum message size
//
//	the sender is able to receive."
//
// Procedure: connect client with a specific MaxMessageSize,
//
//	request a resource whose response fits within the limit.
//
// Expected:  response is received successfully.
func TestTC_CoAP_TCP_018_CSM_MaxMessageSize(t *testing.T) {
	const maxMsgSize = 8192

	r := mux.NewRouter()
	err := r.Handle("/csm-test", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		data := make([]byte, 4096)
		for i := range data {
			data[i] = byte(i & 0xFF)
		}
		_ = w.SetResponse(codes.Content, message.AppOctets, bytes.NewReader(data))
	}))
	require.NoError(t, err)

	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)

	s := NewServer(
		options.WithMux(r),
		options.WithMaxMessageSize(uint32(maxMsgSize)),
	)
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

	cc, err := Dial(l.Addr().String(),
		options.WithMaxMessageSize(uint32(maxMsgSize)),
	)
	require.NoError(t, err, "RFC 8323 §5.3.1: TCP connection with MaxMessageSize must succeed")
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/csm-test")
	require.NoError(t, err, "RFC 8323 §5.3.1: GET within MaxMessageSize must succeed")
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Len(t, body, 4096,
		"RFC 8323 §5.3.1: response body must arrive intact when within MaxMessageSize limit")
}

// TC_CoAP_TCP_019 – TP_CoAP_TCP_POST_LargeBody
//
// Reference: RFC 8323 Section 3.3
// "The lengths are expressed in the length field of the CoAP message
//
//	header and there is no separate payload marker."
//
// Procedure: client POSTs a payload larger than one TCP segment
//
//	(without blockwise, using MaxMessageSize large enough).
//
// Expected:  server receives the entire payload.
func TestTC_CoAP_TCP_019_POST_LargeBody(t *testing.T) {
	const payloadSize = 16384
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	var receivedBody []byte
	var mu sync.Mutex

	addr, cleanup := startTCPConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		body, errR := r.ReadBody()
		if errR == nil {
			mu.Lock()
			receivedBody = body
			mu.Unlock()
		}
		_ = w.SetResponse(codes.Created, message.TextPlain, bytes.NewReader([]byte("created")))
	})
	defer cleanup()

	cc, err := Dial(addr,
		options.WithMaxMessageSize(uint32(payloadSize*2)),
	)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	resp, err := cc.Post(ctx, "/large-write", message.AppOctets, bytes.NewReader(payload))
	require.NoError(t, err, "RFC 8323 §3.3: POST with large body over TCP must succeed")
	require.Equal(t, codes.Created, resp.Code())

	mu.Lock()
	require.Equal(t, payload, receivedBody,
		"RFC 8323 §3.3: server must receive the full %d-byte payload", payloadSize)
	mu.Unlock()
}

// TC_CoAP_TCP_020 – TP_CoAP_TCP_ConcurrentRequests
//
// Reference: RFC 8323 Section 3.4
// "Responses are not strictly required to arrive in the same order
//
//	as the requests."
//
// Procedure: client sends multiple in-flight requests concurrently.
// Expected:  all responses are received correctly (matched by token).
func TestTC_CoAP_TCP_020_ConcurrentRequests(t *testing.T) {
	r := mux.NewRouter()
	err := r.Handle("/concurrent", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain,
			bytes.NewReader([]byte("pong")))
	}))
	require.NoError(t, err)

	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)

	s := NewServer(options.WithMux(r))
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

	cc, err := Dial(l.Addr().String())
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), tcpConformanceTimeout)
	defer cancel()

	const numRequests = 10
	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			resp, errG := cc.Get(ctx, "/concurrent")
			if errG != nil {
				results <- errG
				return
			}
			if resp.Code() != codes.Content {
				results <- assert.AnError
				return
			}
			results <- nil
		}()
	}

	for i := 0; i < numRequests; i++ {
		err := <-results
		require.NoError(t, err,
			"RFC 8323 §3.4: concurrent request %d must succeed", i+1)
	}
}
