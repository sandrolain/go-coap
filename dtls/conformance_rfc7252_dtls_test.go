// Package dtls_test — RFC 7252 §9 "DTLS-Secured CoAP" conformance tests.
//
// Test IDs: DTLS_001 – DTLS_008
// Reference: https://www.rfc-editor.org/rfc/rfc7252#section-9
//
// These tests verify that CoAP over DTLS (RFC 7252 §9) works correctly
// using the pion/dtls third-party library.
package dtls_test

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	piondtls "github.com/pion/dtls/v3"
	"github.com/plgd-dev/go-coap/v3/dtls"
	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/message/pool"
	"github.com/plgd-dev/go-coap/v3/mux"
	coapNet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/net/responsewriter"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/udp/client"
	"github.com/stretchr/testify/require"
)

const dtlsConformanceTimeout = 8 * time.Second

// dtlsPSKConfig returns a pion/dtls PSK config for testing.
func dtlsPSKConfig() *piondtls.Config {
	return &piondtls.Config{
		PSK: func(hint []byte) ([]byte, error) {
			return []byte{0xAB, 0xC1, 0x23}, nil
		},
		PSKIdentityHint: []byte("CoAP-Conformance"),
		CipherSuites:    []piondtls.CipherSuiteID{piondtls.TLS_PSK_WITH_AES_128_CCM_8},
	}
}

// startDTLSServer starts a DTLS CoAP server with a mux router.
func startDTLSServer(t *testing.T, m *mux.Router) (string, func()) {
	t.Helper()
	cfg := dtlsPSKConfig()
	l, err := coapNet.NewDTLSListener("udp", "", cfg)
	require.NoError(t, err)

	s := dtls.NewServer(options.WithMux(m))
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

// startDTLSServerWithHandler starts a DTLS CoAP server with a catch-all handler.
func startDTLSServerWithHandler(t *testing.T, h func(*responsewriter.ResponseWriter[*client.Conn], *pool.Message)) (string, func()) {
	t.Helper()
	cfg := dtlsPSKConfig()
	l, err := coapNet.NewDTLSListener("udp", "", cfg)
	require.NoError(t, err)

	s := dtls.NewServer(options.WithHandlerFunc(h))
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

// TC_CoAP_DTLS_001 – TP_CoAP_DTLS_PSK_GET
//
// Reference: RFC 7252 Section 9.1
// "CoAP can be secured using DTLS. A CoAP endpoint MUST support the
//
//	mandatory-to-implement cipher suite TLS_PSK_WITH_AES_128_CCM_8."
//
// Procedure: establish DTLS connection with PSK, perform GET.
// Expected: response 2.05 Content received over secure channel.
func TestTC_CoAP_DTLS_001_PSK_GET(t *testing.T) {
	r := mux.NewRouter()
	err := r.Handle("/secure", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("secret-data")))
	}))
	require.NoError(t, err)

	addr, cleanup := startDTLSServer(t, r)
	defer cleanup()

	cc, err := dtls.Dial(addr, dtlsPSKConfig())
	require.NoError(t, err, "RFC 7252 §9.1: DTLS PSK handshake must succeed")
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), dtlsConformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/secure")
	require.NoError(t, err, "RFC 7252 §9: GET over DTLS must succeed")
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, []byte("secret-data"), body,
		"RFC 7252 §9: payload must be delivered intact over DTLS")
}

// TC_CoAP_DTLS_002 – TP_CoAP_DTLS_PSK_POST
//
// Reference: RFC 7252 Section 9
// POST with payload over DTLS.
//
// Procedure: POST data to /resource over DTLS.
// Expected: server receives payload, responds 2.01 Created.
func TestTC_CoAP_DTLS_002_PSK_POST(t *testing.T) {
	var receivedBody []byte
	var mu sync.Mutex

	addr, cleanup := startDTLSServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		body, errR := r.ReadBody()
		if errR == nil {
			mu.Lock()
			receivedBody = body
			mu.Unlock()
		}
		_ = w.SetResponse(codes.Created, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	cc, err := dtls.Dial(addr, dtlsPSKConfig())
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), dtlsConformanceTimeout)
	defer cancel()

	payload := []byte("sensor-reading=42")
	resp, err := cc.Post(ctx, "/resource", message.TextPlain, bytes.NewReader(payload))
	require.NoError(t, err, "RFC 7252 §9: POST over DTLS must succeed")
	require.Equal(t, codes.Created, resp.Code())

	mu.Lock()
	require.Equal(t, payload, receivedBody,
		"RFC 7252 §9: POST payload must be received intact over DTLS")
	mu.Unlock()
}

// TC_CoAP_DTLS_003 – TP_CoAP_DTLS_LargePayload_Blockwise
//
// Reference: RFC 7252 §9 + RFC 7959
// Large payloads over DTLS should use blockwise transfer.
//
// Procedure: GET a resource with 4KB payload over DTLS (blockwise enabled).
// Expected: full payload received via automatic blockwise transfer.
func TestTC_CoAP_DTLS_003_LargePayload_Blockwise(t *testing.T) {
	const payloadSize = 4096
	largePayload := make([]byte, payloadSize)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	r := mux.NewRouter()
	err := r.Handle("/large", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.Content, message.AppOctets, bytes.NewReader(largePayload))
	}))
	require.NoError(t, err)

	addr, cleanup := startDTLSServer(t, r)
	defer cleanup()

	cc, err := dtls.Dial(addr, dtlsPSKConfig())
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), dtlsConformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/large")
	require.NoError(t, err, "RFC 7252 §9 + RFC 7959: blockwise GET over DTLS must succeed")
	require.Equal(t, codes.Content, resp.Code())

	body, err := resp.ReadBody()
	require.NoError(t, err)
	require.Equal(t, largePayload, body,
		"RFC 7252 §9: large payload (%d bytes) must be received intact over DTLS+blockwise", payloadSize)
}

// TC_CoAP_DTLS_004 – TP_CoAP_DTLS_NotFound
//
// Reference: RFC 7252 §9 + §5.9.1.4
// Error codes work over DTLS.
//
// Procedure: GET /nonexistent over DTLS.
// Expected: 4.04 Not Found.
func TestTC_CoAP_DTLS_004_NotFound(t *testing.T) {
	r := mux.NewRouter()
	err := r.Handle("/exists", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	}))
	require.NoError(t, err)

	addr, cleanup := startDTLSServer(t, r)
	defer cleanup()

	cc, err := dtls.Dial(addr, dtlsPSKConfig())
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), dtlsConformanceTimeout)
	defer cancel()

	resp, err := cc.Get(ctx, "/nonexistent")
	require.NoError(t, err)
	require.Equal(t, codes.NotFound, resp.Code(),
		"RFC 7252 §9 + §5.9.1.4: 4.04 Not Found must be returned over DTLS")
}

// TC_CoAP_DTLS_005 – TP_CoAP_DTLS_Observe
//
// Reference: RFC 7252 §9 + RFC 7641
// Observe works over DTLS.
//
// Procedure: register observe over DTLS, receive notifications.
// Expected: at least 1 notification received.
func TestTC_CoAP_DTLS_005_Observe(t *testing.T) {
	notifReceived := make(chan struct{}, 2)

	addr, cleanup := startDTLSServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		obs, errO := r.Observe()
		if errO != nil {
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("v0")))
			return
		}
		switch obs {
		case 0: // registration
			cc := w.Conn()
			tok := make([]byte, len(r.Token()))
			copy(tok, r.Token())
			req := cc.AcquireMessage(cc.Context())
			req.SetCode(codes.Content)
			req.SetContentFormat(message.TextPlain)
			req.SetObserve(2)
			req.SetBody(bytes.NewReader([]byte("v1")))
			req.SetToken(tok)
			_ = cc.WriteMessage(req)
			cc.ReleaseMessage(req)
		case 1: // deregistration
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := dtls.Dial(addr, dtlsPSKConfig())
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), dtlsConformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/observable", func(_ *pool.Message) {
		select {
		case notifReceived <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err, "RFC 7641 over DTLS: observe registration must succeed")
	defer func() {
		if obs != nil {
			_ = obs.Cancel(ctx)
		}
	}()

	select {
	case <-notifReceived:
		// OK, got notification
	case <-time.After(5 * time.Second):
		require.FailNow(t, "RFC 7641 over DTLS: timed out waiting for notification")
	}
}

// TC_CoAP_DTLS_006 – TP_CoAP_DTLS_PUT_DELETE
//
// Reference: RFC 7252 §9 + §5.8.3 + §5.8.4
// PUT and DELETE methods work over DTLS.
//
// Procedure: PUT then DELETE a resource.
// Expected: 2.04 Changed and 2.02 Deleted.
func TestTC_CoAP_DTLS_006_PUT_DELETE(t *testing.T) {
	r := mux.NewRouter()
	err := r.Handle("/resource", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		switch r.Code() {
		case codes.PUT:
			_ = w.SetResponse(codes.Changed, message.TextPlain, bytes.NewReader([]byte("updated")))
		case codes.DELETE:
			_ = w.SetResponse(codes.Deleted, message.TextPlain, bytes.NewReader([]byte("deleted")))
		default:
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
		}
	}))
	require.NoError(t, err)

	addr, cleanup := startDTLSServer(t, r)
	defer cleanup()

	cc, err := dtls.Dial(addr, dtlsPSKConfig())
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), dtlsConformanceTimeout)
	defer cancel()

	// PUT
	resp, err := cc.Put(ctx, "/resource", message.TextPlain, bytes.NewReader([]byte("new-value")))
	require.NoError(t, err, "RFC 7252 §9 + §5.8.3: PUT over DTLS must succeed")
	require.Equal(t, codes.Changed, resp.Code())

	// DELETE
	resp, err = cc.Delete(ctx, "/resource")
	require.NoError(t, err, "RFC 7252 §9 + §5.8.4: DELETE over DTLS must succeed")
	require.Equal(t, codes.Deleted, resp.Code())
}

// TC_CoAP_DTLS_007 – TP_CoAP_DTLS_MultipleRequests_SameConnection
//
// Reference: RFC 7252 Section 9.1.2
// "After the DTLS handshake, the endpoint can send multiple CoAP messages
//
//	over the same DTLS connection."
//
// Procedure: send 5 sequential GET requests on the same DTLS connection.
// Expected: all 5 succeed with correct responses.
func TestTC_CoAP_DTLS_007_MultipleRequests_SameConnection(t *testing.T) {
	r := mux.NewRouter()
	for i := 0; i < 5; i++ {
		path := fmt.Sprintf("/r%d", i)
		val := fmt.Sprintf("val%d", i)
		err := r.Handle(path, mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte(val)))
		}))
		require.NoError(t, err)
	}

	addr, cleanup := startDTLSServer(t, r)
	defer cleanup()

	cc, err := dtls.Dial(addr, dtlsPSKConfig())
	require.NoError(t, err, "RFC 7252 §9.1.2: DTLS handshake must succeed")
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), dtlsConformanceTimeout)
	defer cancel()

	for i := 0; i < 5; i++ {
		path := fmt.Sprintf("/r%d", i)
		expected := fmt.Sprintf("val%d", i)

		resp, err := cc.Get(ctx, path)
		require.NoError(t, err, "RFC 7252 §9.1.2: request %d on same DTLS connection must succeed", i+1)
		require.Equal(t, codes.Content, resp.Code())

		body, err := resp.ReadBody()
		require.NoError(t, err)
		require.Equal(t, []byte(expected), body,
			"RFC 7252 §9.1.2: response %d must match expected value", i+1)
	}
}

// TC_CoAP_DTLS_008 – TP_CoAP_DTLS_WrongPSK_Rejected
//
// Reference: RFC 7252 Section 9.1.3
// "If the DTLS handshake fails, the CoAP endpoint MUST NOT send any
//
//	application data."
//
// Procedure: attempt DTLS connection with wrong PSK.
// Expected: handshake fails, no data exchanged.
func TestTC_CoAP_DTLS_008_WrongPSK_Rejected(t *testing.T) {
	r := mux.NewRouter()
	err := r.Handle("/secret", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("secret")))
	}))
	require.NoError(t, err)

	addr, cleanup := startDTLSServer(t, r)
	defer cleanup()

	// Wrong PSK
	wrongCfg := &piondtls.Config{
		PSK: func(hint []byte) ([]byte, error) {
			return []byte{0xFF, 0xFF, 0xFF}, nil // wrong key
		},
		PSKIdentityHint: []byte("CoAP-Conformance"),
		CipherSuites:    []piondtls.CipherSuiteID{piondtls.TLS_PSK_WITH_AES_128_CCM_8},
	}

	cc, err := dtls.Dial(addr, wrongCfg)
	if err != nil {
		// Expected: handshake failure
		return
	}
	// If connection somehow succeeded, try to GET — should fail
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = cc.Get(ctx, "/secret")
	require.Error(t, err,
		"RFC 7252 §9.1.3: request with wrong PSK must fail")
}
