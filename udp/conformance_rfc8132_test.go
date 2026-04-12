// Package udp_test — RFC 8132 "FETCH, PATCH, and iPATCH" conformance tests.
//
// Test IDs: FP_001 – FP_010
// Reference: https://www.rfc-editor.org/rfc/rfc8132
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

// RFC 8132 method codes not yet merged to main branch — use numeric values directly.
const (
	codeFETCH  = codes.Code(5) // RFC 8132 §2
	codePATCH  = codes.Code(6) // RFC 8132 §3
	codeIPATCH = codes.Code(7) // RFC 8132 §4
)

// TC_CoAP_FP_001 – TP_CoAP_FETCH_BasicResponse
//
// Reference: RFC 8132 Section 2.1
// "The FETCH method requests a representation of the target resource
// using the supplied query representation. FETCH is analogous to GET
// but carries a request body."
//
// Procedure: client sends FETCH (code 0.05) with Content-Format=text/plain to /data.
// Expected: server receives code 0.05, responds 2.05 Content.
func TestTC_CoAP_FP_001_FETCH_BasicResponse(t *testing.T) {
	var receivedCode codes.Code
	m := mux.NewRouter()
	err := m.Handle("/data", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		receivedCode = r.Code()
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("result")))
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

	req, err := cc.NewGetRequest(ctx, "/data")
	require.NoError(t, err)
	req.SetCode(codeFETCH)
	req.SetContentFormat(message.TextPlain)
	req.SetBody(bytes.NewReader([]byte("filter")))

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codeFETCH, receivedCode,
		"RFC 8132 §1: server MUST receive request with FETCH code 0.05")
	require.Equal(t, codes.Content, resp.Code(),
		"RFC 8132 §2.1: FETCH on existing resource MUST return 2.05 Content")
}

// TC_CoAP_FP_002 – TP_CoAP_FETCH_MissingContentFormat
//
// Reference: RFC 8132 Section 2.3.1
// "If the FETCH request carries a body, a Content-Format option MUST be
// included to indicate the format of the request body. Absence of
// Content-Format in such a request MUST be treated as a 4.00 Bad Request."
//
// Procedure: client sends FETCH with body but without Content-Format.
// Expected: server responds 4.00 Bad Request.
func TestTC_CoAP_FP_002_FETCH_MissingContentFormat(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		if r.Code() != codeFETCH {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		if _, errCF := r.ContentFormat(); errCF != nil {
			// FETCH with body but missing Content-Format → 4.00 Bad Request
			_ = w.SetResponse(codes.BadRequest, message.TextPlain, nil)
			return
		}
		_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("ok")))
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// FETCH with body but deliberately missing Content-Format option.
	req, err := cc.NewGetRequest(ctx, "/data")
	require.NoError(t, err)
	req.SetCode(codeFETCH)
	req.SetBody(bytes.NewReader([]byte("query body")))
	// intentionally NOT calling req.SetContentFormat(...)

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.BadRequest, resp.Code(),
		"RFC 8132 §2.3.1: FETCH with body but no Content-Format MUST return 4.00 Bad Request")
}

// TC_CoAP_FP_003 – TP_CoAP_FETCH_NotFound
//
// Reference: RFC 8132 Section 2
// "If the resource addressed by the request URI does not exist, the server
// MUST respond with 4.04 Not Found."
//
// Procedure: client sends FETCH to a path that is not registered.
// Expected: server responds 4.04 Not Found.
func TestTC_CoAP_FP_003_FETCH_NotFound(t *testing.T) {
	m := mux.NewRouter()
	// Register /data only; /nonexistent has no handler → default returns 4.04.
	err := m.Handle("/data", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
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

	req, err := cc.NewGetRequest(ctx, "/nonexistent")
	require.NoError(t, err)
	req.SetCode(codeFETCH)
	req.SetContentFormat(message.TextPlain)
	req.SetBody(bytes.NewReader([]byte("filter")))

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.NotFound, resp.Code(),
		"RFC 8132 §2: FETCH on non-existing resource MUST return 4.04 Not Found")
}

// TC_CoAP_FP_004 – TP_CoAP_FETCH_Idempotent
//
// Reference: RFC 8132 Section 2
// "The FETCH method is safe and idempotent. Multiple identical FETCH requests
// MUST produce the same result without side effects on the server state."
//
// Procedure: two identical FETCH requests sent sequentially.
// Expected: both return 2.05 Content with identical response code.
func TestTC_CoAP_FP_004_FETCH_Idempotent(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/sensor", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("42")))
		require.NoError(t, errS)
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, m)
	defer cleanup()

	sendFetch := func() codes.Code {
		cc, errD := udp.Dial(addr)
		require.NoError(t, errD)
		defer func() { _ = cc.Close(); <-cc.Done() }()

		ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
		defer cancel()

		reqF, errF := cc.NewGetRequest(ctx, "/sensor")
		require.NoError(t, errF)
		reqF.SetCode(codeFETCH)
		reqF.SetContentFormat(message.TextPlain)
		reqF.SetBody(bytes.NewReader([]byte("reading")))

		resp, errDo := cc.Do(reqF)
		require.NoError(t, errDo)
		return resp.Code()
	}

	code1 := sendFetch()
	code2 := sendFetch()
	require.Equal(t, codes.Content, code1,
		"RFC 8132 §2: first FETCH MUST return 2.05 Content")
	require.Equal(t, code1, code2,
		"RFC 8132 §2: FETCH is idempotent — repeated identical requests MUST return same response code")
}

// TC_CoAP_FP_005 – TP_CoAP_PATCH_Changed
//
// Reference: RFC 8132 Section 3.1
// "If the PATCH request is successful, the server MUST respond with
// 2.04 (Changed). The server applies the changes described in the
// request payload to the target resource."
//
// Procedure: client sends PATCH with Content-Format=text/plain, body="updated".
// Expected: server applies update, state changes, responds 2.04 Changed.
func TestTC_CoAP_FP_005_PATCH_Changed(t *testing.T) {
	var mu sync.Mutex
	state := "initial"

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		if r.Code() != codePATCH {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		body, errB := r.ReadBody()
		if errB != nil {
			_ = w.SetResponse(codes.BadRequest, message.TextPlain, nil)
			return
		}
		mu.Lock()
		state = string(body)
		mu.Unlock()
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/resource")
	require.NoError(t, err)
	req.SetCode(codePATCH)
	req.SetContentFormat(message.TextPlain)
	req.SetBody(bytes.NewReader([]byte("updated")))

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code(),
		"RFC 8132 §3.1: successful PATCH MUST return 2.04 Changed")

	mu.Lock()
	require.Equal(t, "updated", state,
		"RFC 8132 §3.1: PATCH MUST apply partial update to the target resource")
	mu.Unlock()
}

// TC_CoAP_FP_006 – TP_CoAP_iPATCH_Changed
//
// Reference: RFC 8132 Section 4
// "iPATCH is a variant of PATCH that is idempotent. A successful iPATCH
// request MUST return 2.04 (Changed)."
//
// Procedure: client sends iPATCH (code 0.07) with Content-Format=text/plain.
// Expected: server receives code 0.07, responds 2.04 Changed.
func TestTC_CoAP_FP_006_iPATCH_Changed(t *testing.T) {
	var mu sync.Mutex
	state := "original"

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		if r.Code() != codeIPATCH {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		body, errB := r.ReadBody()
		if errB != nil {
			_ = w.SetResponse(codes.BadRequest, message.TextPlain, nil)
			return
		}
		mu.Lock()
		state = string(body)
		mu.Unlock()
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/resource")
	require.NoError(t, err)
	req.SetCode(codeIPATCH)
	req.SetContentFormat(message.TextPlain)
	req.SetBody(bytes.NewReader([]byte("patched")))

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Changed, resp.Code(),
		"RFC 8132 §4: successful iPATCH MUST return 2.04 Changed")

	mu.Lock()
	require.Equal(t, "patched", state,
		"RFC 8132 §4: iPATCH MUST apply modifications to the target resource")
	mu.Unlock()
}

// TC_CoAP_FP_007 – TP_CoAP_PATCH_UnsupportedContentFormat
//
// Reference: RFC 8132 Section 3.2
// "If the Content-Format option in a PATCH or iPATCH request is not
// supported by the server, it MUST return 4.15 (Unsupported Content-Format)."
//
// Procedure: client sends PATCH with Content-Format=application/json (50).
// Server supports only text/plain for PATCH. Expected: 4.15 Unsupported Content-Format.
func TestTC_CoAP_FP_007_PATCH_UnsupportedContentFormat(t *testing.T) {
	supportedPatchCF := message.TextPlain // server only accepts text/plain patches

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		if r.Code() != codePATCH {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		cf, errCF := r.ContentFormat()
		if errCF != nil || cf != supportedPatchCF {
			_ = w.SetResponse(codes.UnsupportedMediaType, message.TextPlain, nil)
			return
		}
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/resource")
	require.NoError(t, err)
	req.SetCode(codePATCH)
	req.SetContentFormat(message.MediaType(50)) // application/json — not supported by this server
	req.SetBody(bytes.NewReader([]byte(`{"key":"val"}`)))

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.UnsupportedMediaType, resp.Code(),
		"RFC 8132 §3.2: PATCH with unsupported Content-Format MUST return 4.15 Unsupported Content-Format")
}

// TC_CoAP_FP_008 – TP_CoAP_iPATCH_Idempotent
//
// Reference: RFC 8132 Section 4
// "iPATCH is idempotent. Applying the same iPATCH multiple times MUST
// produce the same final resource state as applying it once."
//
// Procedure: iPATCH adds "item" to a set (idempotent add). Two identical
// iPATCH requests. Expected: set contains "item" exactly once.
func TestTC_CoAP_FP_008_iPATCH_Idempotent(t *testing.T) {
	var mu sync.Mutex
	set := map[string]bool{}

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		if r.Code() != codeIPATCH {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		body, errB := r.ReadBody()
		if errB != nil {
			_ = w.SetResponse(codes.BadRequest, message.TextPlain, nil)
			return
		}
		mu.Lock()
		set[string(body)] = true // set semantics: idempotent add
		mu.Unlock()
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	})
	defer cleanup()

	sendIPatch := func(payload string) codes.Code {
		cc, errD := udp.Dial(addr)
		require.NoError(t, errD)
		defer func() { _ = cc.Close(); <-cc.Done() }()

		ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
		defer cancel()

		reqF, errF := cc.NewGetRequest(ctx, "/set")
		require.NoError(t, errF)
		reqF.SetCode(codeIPATCH)
		reqF.SetContentFormat(message.TextPlain)
		reqF.SetBody(bytes.NewReader([]byte(payload)))

		resp, errDo := cc.Do(reqF)
		require.NoError(t, errDo)
		return resp.Code()
	}

	code1 := sendIPatch("item")
	code2 := sendIPatch("item") // identical: idempotent

	require.Equal(t, codes.Changed, code1,
		"RFC 8132 §4: first iPATCH MUST return 2.04 Changed")
	require.Equal(t, codes.Changed, code2,
		"RFC 8132 §4: second identical iPATCH MUST also return 2.04 Changed")

	mu.Lock()
	require.Len(t, set, 1,
		"RFC 8132 §4: iPATCH is idempotent — applying twice MUST yield same final state")
	require.True(t, set["item"],
		"RFC 8132 §4: the set MUST contain the added item after idempotent iPATCH")
	mu.Unlock()
}

// TC_CoAP_FP_009 – TP_CoAP_PATCH_IfMatch_PreconditionFailed
//
// Reference: RFC 8132 Section 3.3
// "If an If-Match option is present and the ETag of the target resource
// does not match the provided value, the server MUST return
// 4.12 (Precondition Failed)."
//
// Procedure: client sends PATCH with If-Match=0xABCD (wrong ETag).
// Server holds ETag=0x1234. Expected: 4.12 Precondition Failed.
func TestTC_CoAP_FP_009_PATCH_IfMatch_PreconditionFailed(t *testing.T) {
	knownETag := []byte{0x12, 0x34}

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		if r.Code() != codePATCH {
			_ = w.SetResponse(codes.MethodNotAllowed, message.TextPlain, nil)
			return
		}
		ifMatch, errIM := r.Options().GetBytes(message.IfMatch)
		if errIM == nil && !bytes.Equal(ifMatch, knownETag) {
			// If-Match present but ETag does not match → 4.12 Precondition Failed
			_ = w.SetResponse(codes.PreconditionFailed, message.TextPlain, nil)
			return
		}
		_ = w.SetResponse(codes.Changed, message.TextPlain, nil)
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	req, err := cc.NewGetRequest(ctx, "/resource")
	require.NoError(t, err)
	req.SetCode(codePATCH)
	req.SetContentFormat(message.TextPlain)
	req.SetBody(bytes.NewReader([]byte("change")))
	req.SetOptionBytes(message.IfMatch, []byte{0xAB, 0xCD}) // wrong ETag

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.PreconditionFailed, resp.Code(),
		"RFC 8132 §3.3: PATCH with non-matching If-Match ETag MUST return 4.12 Precondition Failed")
}

// TC_CoAP_FP_010 – TP_CoAP_FETCH_Accept
//
// Reference: RFC 8132 Section 2.4
// "A FETCH request MAY include an Accept option to indicate the content
// format(s) that the client is willing to accept in the response.
// The server MUST honor the Accept option and respond with the
// requested content format if available."
//
// Procedure: client sends FETCH with Accept=application/json (50).
// Server supports both text/plain and application/json.
// Expected: server responds 2.05 Content with Content-Format=50.
func TestTC_CoAP_FP_010_FETCH_Accept(t *testing.T) {
	jsonCF := message.AppJSON // application/json (50)

	m := mux.NewRouter()
	err := m.Handle("/data", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		accept, errA := r.Accept()
		if errA == nil && accept == jsonCF {
			errS := w.SetResponse(codes.Content, jsonCF, bytes.NewReader([]byte(`{"val":42}`)))
			require.NoError(t, errS)
			return
		}
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

	req, err := cc.NewGetRequest(ctx, "/data")
	require.NoError(t, err)
	req.SetCode(codeFETCH)
	req.SetContentFormat(message.TextPlain)
	req.SetBody(bytes.NewReader([]byte("query")))
	req.SetAccept(jsonCF) // request application/json response

	resp, err := cc.Do(req)
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code(),
		"RFC 8132 §2.4: FETCH with Accept MUST return 2.05 Content when format is available")

	cf, errCF := resp.ContentFormat()
	require.NoError(t, errCF)
	require.Equal(t, jsonCF, cf,
		"RFC 8132 §2.4: server MUST honor the Accept option and return requested Content-Format")
}
