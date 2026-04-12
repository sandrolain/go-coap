// Package main implements a CoAP server acting as the System Under Test (SUT)
// for the Eclipse IoT-Testware CoAP conformance test suite.
//
// Resources exposed (matching CoAP_localhost.cfg PIXITs):
//
//	/Simple_Resource          (PX_DEFAULT_RESOURCE)
//	/Simple_Resource/new      (PX_DEFAULT_RESOURCE/PX_NEW_RESOURCE)
//	/location-query           (PX_METHOD_NOT_ALLOWED_RESOURCE) -> 4.05 always
//	/Storage_Resource         (PX_STORAGE_RESOURCE)
//	/Storage_Resource/New1/New2  (deep path for POST_003)
//	/separate                 (PX_SEPARATE_RESOURCE) -> separate response
//	/any (no handler)         -> 4.04 Not Found
package main

import (
	"bytes"
	"context"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	coapNet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/udp"
)

// simpleResourceETag is a fixed ETag for /Simple_Resource/new.
// A static value is sufficient because the resource content never changes.
var simpleResourceETag = []byte{0x28, 0x6A, 0x5E, 0x10}

// largePayload is a payload large enough (>1024 bytes) to trigger Block2
// block-wise transfer when requested by the client (RFC 7959).
var largePayload = []byte(strings.Repeat("The quick brown fox jumps over the lazy dog. ", 30))

// sut holds the mutable server state shared across handler invocations.
type sut struct {
	mu sync.Mutex

	// Tracks CON POST calls on /Simple_Resource.
	// First call -> 2.01 Created, subsequent -> 2.04 Changed.
	conPostCount int

	// Tracks PUT calls on /Simple_Resource (CON).
	// First PUT -> 2.01 Created; subsequent -> 2.04 Changed.
	putCount int
}

// locationPathOpts returns two Location-Path message.Option values.
func locationPathOpts(seg1, seg2 string) []message.Option {
	return []message.Option{
		{ID: message.LocationPath, Value: []byte(seg1)},
		{ID: message.LocationPath, Value: []byte(seg2)},
	}
}

// handleSimpleResource handles all methods for /Simple_Resource.
//   - GET        -> 2.05 Content (large payload to support Block2, RFC 7959)
//   - PUT        -> 2.01 Created (first) / 2.04 Changed (subsequent)
//   - POST       -> 2.01 Created (first, Location-Path: new) / 2.04 Changed (subsequent)
//   - other      -> 4.05 Method Not Allowed
func (s *sut) handleSimpleResource(w mux.ResponseWriter, r *mux.Message) {
	switch r.Code() {
	case codes.GET:
		// Return a payload large enough to trigger Block2 block-wise transfer
		// when the client requests it (TC_COAP_SERVER_GET_005, RFC 7959 §2.2).
		if err := w.SetResponse(codes.Content, message.TextPlain,
			bytes.NewReader(largePayload)); err != nil {
			log.Printf("handleSimpleResource GET: %v", err)
		}

	case codes.PUT:
		payload, _ := r.ReadBody()

		s.mu.Lock()
		s.putCount++
		count := s.putCount
		s.mu.Unlock()

		respCode := codes.Created
		if count > 1 {
			respCode = codes.Changed
		}
		if err := w.SetResponse(respCode, message.TextPlain,
			bytes.NewReader(payload)); err != nil {
			log.Printf("handleSimpleResource PUT: %v", err)
		}

	case codes.POST:
		// RFC 7252 §5.8.2: POST behaviour depends on payload content.
		//
		// The IoT-Testware POST tests all send to /Simple_Resource with payload "New1/New2":
		//   POST_001 (1st CON call) → 2.01 Created + Location-Path: [New1, New2]
		//   POST_002 (2nd CON call) → 2.04 Changed + Location-Path: [New1, New2]
		//   NON_002  (NON call)     → same logic, but type driven by [LIBRERIA] bug
		//   POST_005 (payload w/o /)→ 4.05 MethodNotAllowed
		//
		// NOTE: POST_001 and POST_002 still FAIL in test runs despite correct SUT responses,
		// because Titan's template v_locationPaths adds a spurious option_length_ext byte
		// even for 4-byte option values (Titan [BUG TITAN] §3.1 encoder, same root cause
		// as GET_002/GET_005/SEPARATE tests). The mismatch is in the RESPONSE TEMPLATE.
		payload, _ := r.ReadBody()
		payloadStr := string(payload)

		if !strings.Contains(payloadStr, "/") {
			// Payload has no slash → POST not supported for non-hierarchical payloads.
			if err := w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil); err != nil {
				log.Printf("handleSimpleResource POST not-allowed: %v", err)
			}
			return
		}

		s.mu.Lock()
		s.conPostCount++
		count := s.conPostCount
		s.mu.Unlock()

		// Both POST_001 and POST_002 send the same payload to /Simple_Resource and
		// both expect Location-Path: [New1, New2] in the response. Only the code differs:
		//   POST_001 (1st call) → 2.01 Created + Location-Path  (resource newly created)
		//   POST_002 (2nd call) → 2.04 Changed + Location-Path  (resource updated)
		parts := strings.Split(payloadStr, "/")
		opts := make([]message.Option, len(parts))
		for i, p := range parts {
			opts[i] = message.Option{ID: message.LocationPath, Value: []byte(p)}
		}
		respCode := codes.Created
		if count > 1 {
			respCode = codes.Changed
		}
		if err := w.SetResponse(respCode, message.TextPlain, nil, opts...); err != nil {
			log.Printf("handleSimpleResource POST: %v", err)
		}

	default:
		if err := w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil); err != nil {
			log.Printf("handleSimpleResource default: %v", err)
		}
	}
}

// handleSimpleResourceNew handles GET /Simple_Resource/new.
// Implements conditional GET via ETag (RFC 7252 §5.10.6, TC_COAP_SERVER_GET_002):
//   - If the request carries a matching ETag -> 2.03 Valid (no payload)
//   - Otherwise                              -> 2.05 Content + ETag option
func (s *sut) handleSimpleResourceNew(w mux.ResponseWriter, r *mux.Message) {
	const body = "sub-resource"
	etagOpt := message.Option{ID: message.ETag, Value: simpleResourceETag}

	// Check all ETag options in the incoming request.
	for _, opt := range r.Options() {
		if opt.ID == message.ETag && bytes.Equal(opt.Value, simpleResourceETag) {
			// Cache hit: respond 2.03 Valid with ETag, no payload.
			if err := w.SetResponse(codes.Valid, message.TextPlain, nil, etagOpt); err != nil {
				log.Printf("handleSimpleResourceNew Valid: %v", err)
			}
			return
		}
	}

	// Cache miss or no ETag: respond 2.05 Content + ETag.
	if err := w.SetResponse(codes.Content, message.TextPlain,
		bytes.NewReader([]byte(body)), etagOpt); err != nil {
		log.Printf("handleSimpleResourceNew Content: %v", err)
	}
}

// handleLocationQuery always returns 4.05 Method Not Allowed.
func (s *sut) handleLocationQuery(w mux.ResponseWriter, _ *mux.Message) {
	if err := w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil); err != nil {
		log.Printf("handleLocationQuery: %v", err)
	}
}

// handleStorageResource handles GET/POST/PUT/DELETE for /Storage_Resource.
func (s *sut) handleStorageResource(w mux.ResponseWriter, r *mux.Message) {
	switch r.Code() {
	case codes.GET:
		if err := w.SetResponse(codes.Content, message.TextPlain,
			bytes.NewReader([]byte("storage content"))); err != nil {
			log.Printf("handleStorageResource GET: %v", err)
		}
	case codes.POST:
		// TC_COAP_SERVER_POST_001: POST on storage resource -> 2.04 Changed (RFC 7252 §5.8.2).
		payload, _ := r.ReadBody()
		if err := w.SetResponse(codes.Changed, message.TextPlain,
			bytes.NewReader(payload)); err != nil {
			log.Printf("handleStorageResource POST: %v", err)
		}
	case codes.PUT:
		payload, _ := r.ReadBody()
		if err := w.SetResponse(codes.Changed, message.TextPlain,
			bytes.NewReader(payload)); err != nil {
			log.Printf("handleStorageResource PUT: %v", err)
		}
	case codes.DELETE:
		if err := w.SetResponse(codes.Deleted, message.TextPlain, nil); err != nil {
			log.Printf("handleStorageResource DELETE: %v", err)
		}
	default:
		if err := w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil); err != nil {
			log.Printf("handleStorageResource default: %v", err)
		}
	}
}

// handleStorageResourceDeep handles POST /Storage_Resource/New1/New2.
// Response: 2.04 Changed + Location-Path: New1, new
func (s *sut) handleStorageResourceDeep(w mux.ResponseWriter, r *mux.Message) {
	if r.Code() != codes.POST {
		if err := w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil); err != nil {
			log.Printf("handleStorageResourceDeep non-POST: %v", err)
		}
		return
	}
	opts := locationPathOpts("New1", "new")
	if err := w.SetResponse(codes.Changed, message.TextPlain, nil, opts...); err != nil {
		log.Printf("handleStorageResourceDeep POST: %v", err)
	}
}

// handleSeparate implements the separate-response flow for /separate.
//
// RFC 7252 §5.2.2: server acknowledges with an empty ACK, then sends a
// separate CON with the actual response code.
// In go-coap: do NOT call SetResponse(); the framework auto-sends empty ACK;
// then we send the actual response from a goroutine.
func (s *sut) handleSeparate(w mux.ResponseWriter, r *mux.Message) {
	reqCode := r.Code()
	token := make([]byte, len(r.Token()))
	copy(token, r.Token())
	cc := w.Conn()

	// Intentionally not calling SetResponse -> framework sends empty ACK.
	go func() {
		time.Sleep(50 * time.Millisecond)
		sendSeparateResponse(cc, reqCode, token)
	}()
}

// sendSeparateResponse sends a new CON message to the client.
// RFC 7252 §5.2.2: the separate CON carries the actual response code and
// must use a fresh message ID (different from the original request).
func sendSeparateResponse(cc mux.Conn, reqCode codes.Code, token []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp := cc.AcquireMessage(ctx)
	defer cc.ReleaseMessage(resp)

	resp.SetToken(token)
	resp.SetType(message.Confirmable)
	// Assign a fresh message ID so the CON can be independently ACK-ed.
	resp.SetMessageID(message.GetMID())

	switch reqCode {
	case codes.GET:
		// TC_COAP_SEPERATE_RESPONSE_001: GET -> 2.05 Content
		resp.SetCode(codes.Content)
		resp.SetContentFormat(message.TextPlain)
		resp.SetBody(bytes.NewReader([]byte("separate response")))

	case codes.POST:
		// TC_COAP_SEPERATE_RESPONSE_002: POST -> 2.04 Changed (RFC 7252 §5.8.2)
		resp.SetCode(codes.Changed)

	case codes.PUT:
		// TC_COAP_SEPERATE_RESPONSE_003: PUT -> 2.04 Changed (RFC 7252 §5.8.3)
		resp.SetCode(codes.Changed)
		resp.SetContentFormat(message.TextPlain)
		resp.SetBody(bytes.NewReader([]byte("stored")))

	case codes.DELETE:
		// TC_COAP_SEPERATE_RESPONSE_004: DELETE -> 2.02 Deleted (RFC 7252 §5.8.4)
		resp.SetCode(codes.Deleted)

	default:
		resp.SetCode(codes.Content)
	}

	if err := cc.WriteMessage(resp); err != nil {
		log.Printf("sendSeparateResponse code=%v: %v", reqCode, err)
	} else {
		log.Printf("sendSeparateResponse code=%v: sent OK", reqCode)
	}
}

func loggingMiddleware(next mux.Handler) mux.Handler {
	return mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		log.Printf("[SUT] %v  %v  path=%v", w.Conn().RemoteAddr(), r.Code(), func() string {
			p, _ := r.Options().Path()
			return p
		}())
		next.ServeCOAP(w, r)
	})
}

func main() {
	s := &sut{}

	r := mux.NewRouter()
	r.Use(loggingMiddleware)

	// More specific paths first.
	r.Handle("/Simple_Resource/new", mux.HandlerFunc(s.handleSimpleResourceNew))
	r.Handle("/Simple_Resource", mux.HandlerFunc(s.handleSimpleResource))
	r.Handle("/location-query", mux.HandlerFunc(s.handleLocationQuery))
	r.Handle("/Storage_Resource/New1/New2", mux.HandlerFunc(s.handleStorageResourceDeep))
	r.Handle("/Storage_Resource", mux.HandlerFunc(s.handleStorageResource))
	r.Handle("/separate", mux.HandlerFunc(s.handleSeparate))
	// /any has no handler -> mux returns 4.04 Not Found automatically.

	srv := udp.NewServer(options.WithMux(r))

	listenAddr := ":5683"
	l, err := coapNet.NewListenUDP("udp", listenAddr)
	if err != nil {
		log.Fatalf("listen %v: %v", listenAddr, err)
	}

	log.Printf("[SUT] CoAP SUT listening on %v (UDP)", net.JoinHostPort("0.0.0.0", "5683"))
	if err := srv.Serve(l); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
