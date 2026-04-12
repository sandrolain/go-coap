// Package udp_test contains CoAP protocol conformance test helpers.
//
// Test functions are organized in RFC-specific files:
//   - conformance_rfc7252_test.go — RFC 7252 tests (CF_001–CF_065)
//   - conformance_rfc7641_test.go — RFC 7641 tests (OB_001–OB_013, CF_040–CF_041)
//   - conformance_rfc7959_test.go — RFC 7959 tests (BW_001–BW_018)
//   - conformance_rfc6690_test.go — RFC 6690 tests (WK_001–WK_010)
//   - conformance_rfc8132_test.go — RFC 8132 tests (FP_001–FP_010)
//   - conformance_rfc9175_test.go — RFC 9175 + RFC 8768 tests (ET_001–ET_006, HL_001–HL_002)
//   - conformance_rfc9177_test.go — RFC 9177 tests (QB_001–QB_006)
package udp_test

import (
	"sync"
	"testing"
	"time"

	"github.com/plgd-dev/go-coap/v3/message/pool"
	"github.com/plgd-dev/go-coap/v3/mux"
	coapNet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/net/responsewriter"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/plgd-dev/go-coap/v3/udp/client"
	"github.com/plgd-dev/go-coap/v3/udp/server"
	"github.com/stretchr/testify/require"
)

const conformanceTimeout = 5 * time.Second

// startConformanceServer starts a UDP CoAP server with the given mux router
// on a random port. It returns the server, its address, and a cleanup function.
func startConformanceServer(t *testing.T, m *mux.Router) (*server.Server, string, func()) {
	t.Helper()
	l, err := coapNet.NewListenUDP("udp", "127.0.0.1:0")
	require.NoError(t, err)

	var wg sync.WaitGroup
	s := udp.NewServer(options.WithMux(m))
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

// startConformanceServerWithHandler starts a server with a catch-all handler.
func startConformanceServerWithHandler(t *testing.T, h func(*responsewriter.ResponseWriter[*client.Conn], *pool.Message)) (string, func()) {
	t.Helper()
	l, err := coapNet.NewListenUDP("udp", "127.0.0.1:0")
	require.NoError(t, err)

	var wg sync.WaitGroup
	s := udp.NewServer(options.WithHandlerFunc(h))
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

// encodeUint16 encodes a uint16 as the compact CoAP uint option value
// (omitting leading zero bytes as per RFC 7252 Section 3.2).
func encodeUint16(v uint16) []byte {
	if v == 0 {
		return []byte{}
	}
	if v <= 0xFF {
		return []byte{byte(v)}
	}
	return []byte{byte(v >> 8), byte(v)}
}
