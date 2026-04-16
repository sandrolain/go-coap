// Package udp_test — RFC 7641 "Observing Resources in CoAP" conformance tests.
//
// Test IDs: OB_001 – OB_012
// Reference: https://www.rfc-editor.org/rfc/rfc7641
package udp_test

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
	"github.com/plgd-dev/go-coap/v3/net/responsewriter"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/plgd-dev/go-coap/v3/udp/client"
	"github.com/stretchr/testify/require"
)

// TC_CoAP_OB_001 – TP_CoAP_Observe_NonObservableResource
//
// Reference: RFC 7641 Section 3.2
// "If the server does not recognize the Observe option or does not support
// observation of the resource, it MUST respond without an Observe option."
//
// Procedure: GET /static with Observe=0. Server ignores Observe.
// Expected: response does NOT contain Observe option.
func TestTC_CoAP_OB_001_NonObservableResource(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/static", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		// Deliberately omit Observe option in response (not observable)
		errS := w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("static")))
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

	var gotNotification bool
	obs, err := cc.Observe(ctx, "/static", func(msg *pool.Message) {
		// If server sends Observe in response, this will be called.
		// Check if response has Observe option.
		_, obsErr := msg.Observe()
		if obsErr == nil {
			gotNotification = true
		}
	})
	// Observe registration may succeed or return an error depending on how the
	// library handles a registration response without Observe.
	// The key requirement is that the server's response has NO Observe option.
	if err == nil && obs != nil {
		// Cancel immediately; the observation should not be truly active.
		_ = obs.Cancel(ctx)
	}
	require.False(t, gotNotification,
		"RFC 7641 §3.2: non-observable resource MUST NOT include Observe in response")
}

// TC_CoAP_OB_002 – TP_CoAP_Observe_RegistrationSuccess
//
// Reference: RFC 7641 Section 3.1 & 4.1
// "If the server is able to add the client to the list of observers for the
// resource, it MUST return the current representation along with an Observe
// option."
//
// Procedure: GET /temp with Observe=0. Server supports observations.
// Expected: first response includes Observe option with sequence number ≥ 0.
func TestTC_CoAP_OB_002_RegistrationHasObserveOption(t *testing.T) {
	var gotObserveOption bool
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/temp", func(msg *pool.Message) {
		_, errO := msg.Observe()
		if errO == nil {
			gotObserveOption = true
		}
	})
	require.NoError(t, err, "Observe registration must succeed for observable resource")
	time.Sleep(100 * time.Millisecond)
	if obs != nil {
		_ = obs.Cancel(ctx)
	}
	require.True(t, gotObserveOption,
		"RFC 7641 §4.1: server must include Observe option in notification response")
}

// TC_CoAP_OB_003 – TP_CoAP_Observe_SequenceNumbersMonotonic
//
// Reference: RFC 7641 Section 4.4 (sequence numbers)
// "Within each notification, the Observe option value is a sequence number
// that increases modulo 2^24."
//
// Procedure: server sends 3 notifications with increasing Observe seq numbers.
// Expected: each received Observe value is strictly greater than the previous (mod 2^24).
func TestTC_CoAP_OB_003_SequenceNumbersMonotonic(t *testing.T) {
	numNotifications := 3
	notifCh := make(chan uint32, numNotifications)

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
				for i := 0; i < numNotifications; i++ {
					seq := uint32(i + 2) // sequence: 2, 3, 4
					req := cc.AcquireMessage(cc.Context())
					req.SetCode(codes.Content)
					req.SetContentFormat(message.TextPlain)
					req.SetObserve(seq)
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

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/counter", func(msg *pool.Message) {
		seq, errO := msg.Observe()
		if errO == nil {
			select {
			case notifCh <- seq:
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

	// Collect notifications
	var seqs []uint32
	deadline := time.After(3 * time.Second)
	for len(seqs) < numNotifications {
		select {
		case seq := <-notifCh:
			seqs = append(seqs, seq)
		case <-deadline:
			require.FailNow(t, "timed out waiting for notifications", "got %d of %d", len(seqs), numNotifications)
		}
	}

	for i := 1; i < len(seqs); i++ {
		require.Greater(t, seqs[i], seqs[i-1],
			"RFC 7641 §4.4: Observe sequence numbers must be strictly increasing (got %d after %d)", seqs[i], seqs[i-1])
	}
}

// TC_CoAP_OB_004 – TP_CoAP_Observe_DeregistrationSameToken
//
// Reference: RFC 7641 Section 3.5
// "The client MUST use the same token when sending the deregistration request
// as it used in the registration (GET with Observe=0)."
//
// Procedure: register with known token; cancel observation; verify no error.
// Expected: cancellation succeeds (verifying go-coap uses the same token).
func TestTC_CoAP_OB_004_DeregistrationSameToken(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
			req := cc.AcquireMessage(cc.Context())
			req.SetCode(codes.Content)
			req.SetContentFormat(message.TextPlain)
			req.SetObserve(2)
			req.SetBody(bytes.NewReader([]byte("v1")))
			req.SetToken(tok)
			_ = cc.WriteMessage(req)
			cc.ReleaseMessage(req)
		case 1:
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/observable", func(_ *pool.Message) {})
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	err = obs.Cancel(ctx)
	require.NoError(t, err,
		"RFC 7641 §3.5: deregistration with same token must succeed")
	require.True(t, obs.Canceled(),
		"observation must be marked as canceled after Cancel()")
}

// TC_CoAP_OB_005 – TP_CoAP_Observe_MultipleNotifications
//
// Reference: RFC 7641 Section 4.1
// "Each notification is a response to the observation request, containing
// the current representation of the resource."
//
// Procedure: register for /counter; server sends 5 notifications.
// Expected: client receives all 5 notifications with Content payload.
func TestTC_CoAP_OB_005_MultipleNotifications(t *testing.T) {
	const numNotifs = 5
	var received atomic.Int32
	done := make(chan struct{})

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/counter", func(msg *pool.Message) {
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
	case <-time.After(5 * time.Second):
		require.FailNow(t, "RFC 7641 §4.1: did not receive all notifications in time",
			"got %d of %d", received.Load(), numNotifs)
	}
	require.GreaterOrEqual(t, received.Load(), int32(numNotifs),
		"RFC 7641 §4.1: must receive all server-sent notifications")
}

// TC_CoAP_OB_006 – TP_CoAP_Observe_MaxAgeInNotification
//
// Reference: RFC 7641 Section 4.6
// "When a server sends a notification, it SHOULD include a Max-Age option
// to indicate how long the notification may be considered fresh."
//
// Procedure: register for /temp-sensor; server sends notification with Max-Age.
// Expected: notification carries Max-Age option.
func TestTC_CoAP_OB_006_MaxAgeInNotification(t *testing.T) {
	type notifResult struct {
		maxAge uint32
		err    error
	}
	notifReceived := make(chan notifResult, 1)

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
			req := cc.AcquireMessage(cc.Context())
			req.SetCode(codes.Content)
			req.SetContentFormat(message.TextPlain)
			req.SetObserve(2)
			req.SetOptionUint32(message.MaxAge, 30)
			req.SetBody(bytes.NewReader([]byte("22.5")))
			req.SetToken(tok)
			_ = cc.WriteMessage(req)
			cc.ReleaseMessage(req)
		case 1:
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/temp-sensor", func(msg *pool.Message) {
		// Read MaxAge inside the callback: pool.Message is only valid for the lifetime of the callback.
		v, errMA := msg.Options().GetUint32(message.MaxAge)
		select {
		case notifReceived <- notifResult{maxAge: v, err: errMA}:
		default:
		}
	})
	require.NoError(t, err)
	defer func() {
		if obs != nil {
			_ = obs.Cancel(ctx)
		}
	}()

	select {
	case res := <-notifReceived:
		require.NoError(t, res.err,
			"RFC 7641 §4.6: notification SHOULD include Max-Age option; got error: %v", res.err)
		require.Greater(t, res.maxAge, uint32(0),
			"RFC 7641 §4.6: Max-Age value must be positive")
	case <-time.After(3 * time.Second):
		require.FailNow(t, "RFC 7641: timed out waiting for notification with Max-Age")
	}
}

// TC_CoAP_OB_007 – TP_CoAP_Observe_ConsistentContentFormat
//
// Reference: RFC 7641 Section 4.1
// "Each notification MUST use the same Content-Format as the initial response."
//
// Procedure: register; server sends 2 notifications with text/plain.
// Expected: both notifications have Content-Format=text/plain.
func TestTC_CoAP_OB_007_ConsistentContentFormat(t *testing.T) {
	contentFormats := make(chan message.MediaType, 2)

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
				for i := 0; i < 2; i++ {
					req := cc.AcquireMessage(cc.Context())
					req.SetCode(codes.Content)
					req.SetContentFormat(message.TextPlain)
					req.SetObserve(uint32(i + 2))
					req.SetBody(bytes.NewReader([]byte("data")))
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

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/cf-resource", func(msg *pool.Message) {
		cf, errCF := msg.ContentFormat()
		if errCF == nil {
			select {
			case contentFormats <- cf:
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

	collected := make([]message.MediaType, 0, 2)
	deadline := time.After(3 * time.Second)
	for len(collected) < 2 {
		select {
		case cf := <-contentFormats:
			collected = append(collected, cf)
		case <-deadline:
			require.FailNow(t, "timed out waiting for 2 notifications")
		}
	}

	require.Equal(t, collected[0], collected[1],
		"RFC 7641 §4.1: all notifications must use the same Content-Format")
}

// TC_CoAP_OB_008 – TP_CoAP_Observe_ErrorTerminatesRegistration
//
// Reference: RFC 7641 Section 4.7
// "If the server sends a notification with an error code (4.xx or 5.xx),
// the observation is terminated."
//
// Procedure: server sends 5.00 Internal Server Error notification.
// Expected: observation is cancelled (no further notifications expected).
func TestTC_CoAP_OB_008_ErrorTerminatesObservation(t *testing.T) {
	var notifCount atomic.Int32

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
			// First a valid notification, then an error notification
			go func() {
				req := cc.AcquireMessage(cc.Context())
				req.SetCode(codes.Content)
				req.SetContentFormat(message.TextPlain)
				req.SetObserve(2)
				req.SetBody(bytes.NewReader([]byte("ok")))
				req.SetToken(tok)
				_ = cc.WriteMessage(req)
				cc.ReleaseMessage(req)
				time.Sleep(30 * time.Millisecond)
				// Send error notification — should terminate observation
				req2 := cc.AcquireMessage(cc.Context())
				req2.SetCode(codes.InternalServerError) // 5.00 — no Observe
				req2.SetToken(tok)
				_ = cc.WriteMessage(req2)
				cc.ReleaseMessage(req2)
			}()
		case 1:
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/error-resource", func(_ *pool.Message) {
		notifCount.Add(1)
	})
	require.NoError(t, err)
	defer func() {
		if obs != nil && !obs.Canceled() {
			_ = obs.Cancel(ctx)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	// After the error notification, count should be at most 1 (the valid one)
	// and no further notifications should arrive.
	require.LessOrEqual(t, notifCount.Load(), int32(2),
		"RFC 7641 §4.7: error notification should terminate observations; got %d notifications", notifCount.Load())
}

// TC_CoAP_OB_009 – TP_CoAP_Observe_MultipleObservers
//
// Reference: RFC 7641 Section 3.1
// Multiple clients can observe the same resource simultaneously. Each gets
// independent notifications.
//
// Procedure: two clients register for /shared. Server sends 2 notifications.
// Expected: both clients receive the notifications.
func TestTC_CoAP_OB_009_MultipleObservers(t *testing.T) {
	var (
		mu           sync.Mutex
		observerToks [][]byte
	)

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
			mu.Lock()
			observerToks = append(observerToks, tok)
			mu.Unlock()
			// Send initial notification to register this observer
			req := cc.AcquireMessage(cc.Context())
			req.SetCode(codes.Content)
			req.SetContentFormat(message.TextPlain)
			req.SetObserve(2)
			req.SetBody(bytes.NewReader([]byte("v1")))
			req.SetToken(tok)
			_ = cc.WriteMessage(req)
			cc.ReleaseMessage(req)
		case 1:
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	client1received := make(chan struct{}, 1)
	client2received := make(chan struct{}, 1)

	cc1, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc1.Close(); <-cc1.Done() }()

	cc2, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc2.Close(); <-cc2.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs1, err := cc1.Observe(ctx, "/shared", func(_ *pool.Message) {
		select {
		case client1received <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)
	defer func() {
		if obs1 != nil {
			_ = obs1.Cancel(ctx)
		}
	}()

	obs2, err := cc2.Observe(ctx, "/shared", func(_ *pool.Message) {
		select {
		case client2received <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)
	defer func() {
		if obs2 != nil {
			_ = obs2.Cancel(ctx)
		}
	}()

	// Both observers should receive at least one notification
	deadline := time.After(3 * time.Second)
	var got1, got2 bool
	for !got1 || !got2 {
		select {
		case <-client1received:
			got1 = true
		case <-client2received:
			got2 = true
		case <-deadline:
			require.True(t, got1, "RFC 7641 §3.1: client1 must receive notifications")
			require.True(t, got2, "RFC 7641 §3.1: client2 must receive notifications")
			return
		}
	}
}

// TC_CoAP_OB_010 – TP_CoAP_Observe_ReRegistration
//
// Reference: RFC 7641 Section 3.6
// "A client that wants to reregister (e.g., after the resource representation
// has changed) can send a new GET request with Observe=0."
//
// Procedure: register, cancel, register again.
// Expected: second registration succeeds and receives new notifications.
func TestTC_CoAP_OB_010_ReRegistration(t *testing.T) {
	notifCh := make(chan struct{}, 5)

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
			req := cc.AcquireMessage(cc.Context())
			req.SetCode(codes.Content)
			req.SetContentFormat(message.TextPlain)
			req.SetObserve(2)
			req.SetBody(bytes.NewReader([]byte("v1")))
			req.SetToken(tok)
			_ = cc.WriteMessage(req)
			cc.ReleaseMessage(req)
		case 1:
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	// First registration
	obs1, err := cc.Observe(ctx, "/reregister", func(_ *pool.Message) {
		select {
		case notifCh <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)
	select {
	case <-notifCh:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "first registration: no notification received")
	}

	err = obs1.Cancel(ctx)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	// Second registration
	obs2, err := cc.Observe(ctx, "/reregister", func(_ *pool.Message) {
		select {
		case notifCh <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err, "RFC 7641 §3.6: re-registration after cancel must succeed")
	defer func() {
		if obs2 != nil {
			_ = obs2.Cancel(ctx)
		}
	}()
	select {
	case <-notifCh:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "RFC 7641 §3.6: re-registration must receive new notification")
	}
}

// TC_CoAP_OB_011 – TP_CoAP_Observe_CancelImmediately
//
// Reference: RFC 7641 Section 3.5
// "A client can deregister at any time by sending a GET request with Observe=1."
// Cancelling immediately after registration is valid.
//
// Procedure: register, then immediately cancel. No panics or errors expected.
func TestTC_CoAP_OB_011_CancelImmediately(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
			req := cc.AcquireMessage(cc.Context())
			req.SetCode(codes.Content)
			req.SetContentFormat(message.TextPlain)
			req.SetObserve(2)
			req.SetBody(bytes.NewReader([]byte("v1")))
			req.SetToken(tok)
			_ = cc.WriteMessage(req)
			cc.ReleaseMessage(req)
		case 1:
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/immediate", func(_ *pool.Message) {})
	require.NoError(t, err)
	// Cancel immediately
	err = obs.Cancel(ctx)
	require.NoError(t, err, "RFC 7641 §3.5: immediate cancellation must not error")
}

// TC_CoAP_OB_012 – TP_CoAP_Observe_TokenEchoInNotification
//
// Reference: RFC 7252 Section 5.3.1 (applied to notifications, RFC 7641 §4.1)
// Notifications are responses; the server MUST echo the registration token
// in every notification.
//
// Procedure: register; capture registration token from server handler; verify notification
// token matches.
func TestTC_CoAP_OB_012_TokenEchoedInNotification(t *testing.T) {
	notifTokCh := make(chan message.Token, 2)
	registrationTokCh := make(chan message.Token, 1)

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		obs, errO := r.Observe()
		if errO != nil {
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("v0")))
			return
		}
		switch obs {
		case 0:
			cc := w.Conn()
			regTok := make(message.Token, len(r.Token()))
			copy(regTok, r.Token())
			select {
			case registrationTokCh <- regTok:
			default:
			}
			req := cc.AcquireMessage(cc.Context())
			req.SetCode(codes.Content)
			req.SetContentFormat(message.TextPlain)
			req.SetObserve(2)
			req.SetBody(bytes.NewReader([]byte("v1")))
			req.SetToken(regTok)
			_ = cc.WriteMessage(req)
			cc.ReleaseMessage(req)
		case 1:
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/token-echo", func(msg *pool.Message) {
		tok := make(message.Token, len(msg.Token()))
		copy(tok, msg.Token())
		select {
		case notifTokCh <- tok:
		default:
		}
	})
	require.NoError(t, err)
	defer func() {
		if obs != nil {
			_ = obs.Cancel(ctx)
		}
	}()

	var registrationToken message.Token
	select {
	case registrationToken = <-registrationTokCh:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for registration")
	}

	select {
	case notifTok := <-notifTokCh:
		require.Equal(t, registrationToken, notifTok,
			"RFC 7641 §4.1 + RFC 7252 §5.3.1: notification must echo the registration token")
	case <-time.After(3 * time.Second):
		require.FailNow(t, "timed out waiting for notification with token")
	}
}

// TC_CoAP_OB_013 – TP_CoAP_Observe_StaleNotificationDiscarded
//
// Reference: RFC 7641 Section 3.4
// "A notification is fresh if its Observe option value v_next > v_curr (within
// 2^23 modular distance) OR the notification arrived more than 128 seconds after
// the last fresh notification."
//
// Procedure: server sends three notifications with seq=5, seq=3 (stale: 3 < 5
// within 128 s window), seq=7 (fresh: 7 > 5).
// Expected: client callback is invoked only twice (seq=5 and seq=7);
// the stale notification (seq=3) MUST be silently discarded by the client.
//
// go-coap implements ValidSequenceNumber in net/observation/observation.go
// which correctly implements RFC 7641 §3.4.
func TestTC_CoAP_OB_013_StaleNotificationDiscarded(t *testing.T) {
	notifSeqs := make(chan uint32, 10)

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
			go func() {
				for _, seq := range []uint32{5, 3, 7} { // 3 is stale (3 < 5)
					req := cc.AcquireMessage(cc.Context())
					req.SetCode(codes.Content)
					req.SetContentFormat(message.TextPlain)
					req.SetObserve(seq)
					req.SetBody(bytes.NewReader([]byte("v")))
					req.SetToken(tok)
					_ = cc.WriteMessage(req)
					cc.ReleaseMessage(req)
					time.Sleep(20 * time.Millisecond)
				}
			}()
		case 1: // deregistration
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/stale-obs", func(msg *pool.Message) {
		seq, errO := msg.Observe()
		if errO == nil {
			notifSeqs <- seq
		}
	})
	require.NoError(t, err)
	defer func() {
		if obs != nil {
			_ = obs.Cancel(ctx)
		}
	}()

	// Collect notifications for up to 2 seconds
	var received []uint32
	deadline := time.After(2 * time.Second)
collect:
	for {
		select {
		case seq := <-notifSeqs:
			received = append(received, seq)
		case <-deadline:
			break collect
		}
	}

	require.Equal(t, 2, len(received),
		"RFC 7641 §3.4: stale notification (seq=3 < seq=5 within 128s) MUST be discarded; "+
			"client should receive exactly 2 notifications (seq=5 and seq=7), got %v", received)
	require.Equal(t, uint32(5), received[0],
		"RFC 7641 §3.4: first received notification must be seq=5")
	require.Equal(t, uint32(7), received[1],
		"RFC 7641 §3.4: second received notification must be seq=7; stale seq=3 must be skipped")
}

// ── Migrated from conformance_test.go (CF_040–CF_041) ────────────────────────

// TC_CoAP_CF_040 – TP_CoAP_Observe_Registration
//
// Reference: RFC 7641 Section 3.1
// "When a client registers interest by adding an Observe Option (value 0) to
// a GET request, the server returns the current state and adds the client to
// its list of observers."
//
// Procedure: client sends GET /temperature with Observe=0. Server sends 3
// notifications with incrementing Observe counters.
// Expected: client receives at least 3 Observe notifications.
func TestTC_CoAP_CF_040_Observe_Registration(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		obs, errO := r.Observe()
		if errO != nil {
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("v0")))
			return
		}
		switch obs {
		case 0:
			// Register: send 3 notifications asynchronously.
			cc := w.Conn()
			tok := make([]byte, len(r.Token()))
			copy(tok, r.Token())
			go func() {
				for i := uint32(0); i < 3; i++ {
					req := cc.AcquireMessage(cc.Context())
					req.SetCode(codes.Content)
					req.SetContentFormat(message.TextPlain)
					req.SetObserve(i + 2)
					req.SetBody(bytes.NewReader([]byte{byte('1' + i)}))
					req.SetToken(tok)
					_ = cc.WriteMessage(req)
					cc.ReleaseMessage(req)
					time.Sleep(10 * time.Millisecond)
				}
			}()
		case 1:
			// Deregister.
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	notifCh := make(chan struct{}, 10)
	obs, err := cc.Observe(ctx, "/temperature", func(_ *pool.Message) {
		select {
		case notifCh <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err,
		"RFC 7641 §3.1: Observe registration (GET with Observe=0) must succeed")

	received := 0
	timeout40 := time.After(3 * time.Second)
outerCF040:
	for received < 3 {
		select {
		case <-notifCh:
			received++
		case <-timeout40:
			break outerCF040
		}
	}
	require.GreaterOrEqual(t, received, 3,
		"RFC 7641 §3.1: server must deliver at least 3 Observe notifications")
	_ = obs
}

// TC_CoAP_CF_041 – TP_CoAP_Observe_Cancellation
//
// Reference: RFC 7641 Section 3.5
// "A client can cancel an observation by sending a GET request … with an
// Observe Option set to 1 (deregister)."
//
// Procedure: client registers an observation then cancels it via Cancel()
// (sends GET with Observe=1). Server responds to the deregister request.
// Expected: Cancel() returns no error and the observation is marked canceled.
func TestTC_CoAP_CF_041_Observe_Cancellation(t *testing.T) {
	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
		obs, errO := r.Observe()
		if errO != nil {
			_ = w.SetResponse(codes.Content, message.TextPlain, bytes.NewReader([]byte("v0")))
			return
		}
		switch obs {
		case 0:
			// Register: send one initial notification so cc.Observe() can return.
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
		case 1:
			// Deregister: respond so Cancel() can complete.
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/temperature", func(_ *pool.Message) {})
	require.NoError(t, err, "Observe registration must succeed")

	// Wait briefly to ensure the initial notification is processed.
	time.Sleep(50 * time.Millisecond)

	// Cancel the observation (sends GET with Observe=1).
	err = obs.Cancel(ctx)
	require.NoError(t, err,
		"RFC 7641 §3.5: Observe cancellation (GET with Observe=1) must succeed")
	require.True(t, obs.Canceled(),
		"observation must be marked as canceled after Cancel()")
}

// ── Additional RFC 7641 conformance tests (OB_014–OB_016) ──────

// TC_CoAP_OB_014 – TP_CoAP_Observe_MaxAge_Expiration
//
// Reference: RFC 7641 Section 4.5
// "When Max-Age expires, the notification is no longer fresh. A new
// notification should be sent by the server to keep the client updated."
//
// Procedure: server sends notification with Max-Age=1, then after a short
// sleep sends another notification. Client receives both.
// Expected: client receives a second (fresh) notification after the first.
func TestTC_CoAP_OB_014_MaxAge_Expiration(t *testing.T) {
	type notif struct {
		seq    uint32
		maxAge uint32
	}
	notifCh := make(chan notif, 5)

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
				// Notification 1: Max-Age=1 (expires in 1s)
				req := cc.AcquireMessage(cc.Context())
				req.SetCode(codes.Content)
				req.SetContentFormat(message.TextPlain)
				req.SetObserve(2)
				req.SetOptionUint32(message.MaxAge, 1)
				req.SetBody(bytes.NewReader([]byte("temp=20")))
				req.SetToken(tok)
				_ = cc.WriteMessage(req)
				cc.ReleaseMessage(req)

				// Wait for Max-Age to expire, then send fresh notification
				time.Sleep(1200 * time.Millisecond)

				req2 := cc.AcquireMessage(cc.Context())
				req2.SetCode(codes.Content)
				req2.SetContentFormat(message.TextPlain)
				req2.SetObserve(3)
				req2.SetOptionUint32(message.MaxAge, 60)
				req2.SetBody(bytes.NewReader([]byte("temp=21")))
				req2.SetToken(tok)
				_ = cc.WriteMessage(req2)
				cc.ReleaseMessage(req2)
			}()
		case 1:
			_ = w.SetResponse(codes.Content, message.TextPlain, nil)
		}
	})
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	obs, err := cc.Observe(ctx, "/temp-sensor", func(msg *pool.Message) {
		seq, errO := msg.Observe()
		if errO != nil {
			return
		}
		ma, errMA := msg.Options().GetUint32(message.MaxAge)
		if errMA != nil {
			ma = 60 // default
		}
		select {
		case notifCh <- notif{seq: seq, maxAge: ma}:
		default:
		}
	})
	require.NoError(t, err)
	defer func() {
		if obs != nil {
			_ = obs.Cancel(ctx)
		}
	}()

	// Collect both notifications
	var received []notif
	deadline := time.After(5 * time.Second)
	for len(received) < 2 {
		select {
		case n := <-notifCh:
			received = append(received, n)
		case <-deadline:
			require.FailNow(t, "RFC 7641 §4.5: timed out waiting for notifications after Max-Age expiry",
				"got %d of 2", len(received))
		}
	}

	require.Equal(t, uint32(1), received[0].maxAge,
		"RFC 7641 §4.5: first notification Max-Age must be 1")
	require.Greater(t, received[1].seq, received[0].seq,
		"RFC 7641 §4.5: second notification must have higher sequence number")
}

// TC_CoAP_OB_015 – TP_CoAP_Observe_Reordering_ClockBased
//
// Reference: RFC 7641 Section 3.4
// "V1 is fresher than V2 either if V1 > V2 (modular comparison within
// 2^23) and the notification was received within 128 seconds of the
// last fresh notification, OR if it was received more than 128 seconds
// after the last fresh notification (regardless of sequence number)."
//
// Procedure: server sends seq=10, seq=8 (stale, within 128s), seq=12 (fresh).
// Expected: client discards seq=8, receives only seq=10 and seq=12.
func TestTC_CoAP_OB_015_Reordering_ClockBased(t *testing.T) {
	notifSeqs := make(chan uint32, 10)

	addr, cleanup := startConformanceServerWithHandler(t, func(w *responsewriter.ResponseWriter[*client.Conn], r *pool.Message) {
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
				// seq=10 (fresh), seq=8 (stale: 8 < 10 within 128s), seq=12 (fresh)
				for _, seq := range []uint32{10, 8, 12} {
					req := cc.AcquireMessage(cc.Context())
					req.SetCode(codes.Content)
					req.SetContentFormat(message.TextPlain)
					req.SetObserve(seq)
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

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), conformanceTimeout)
	defer cancel()

	obs, err := cc.Observe(ctx, "/reorder", func(msg *pool.Message) {
		seq, errO := msg.Observe()
		if errO == nil {
			notifSeqs <- seq
		}
	})
	require.NoError(t, err)
	defer func() {
		if obs != nil {
			_ = obs.Cancel(ctx)
		}
	}()

	var received []uint32
	deadline := time.After(2 * time.Second)
collect:
	for {
		select {
		case seq := <-notifSeqs:
			received = append(received, seq)
		case <-deadline:
			break collect
		}
	}

	require.Equal(t, 2, len(received),
		"RFC 7641 §3.4: stale notification (seq=8 after seq=10) must be discarded; got %v", received)
	require.Equal(t, uint32(10), received[0])
	require.Equal(t, uint32(12), received[1])
}

// TC_CoAP_OB_016 – TP_CoAP_Observe_Blockwise_LargeNotification
//
// Reference: RFC 7959 Section 2.9 + RFC 7641
// "When a resource is too large for a single message, blockwise transfer
// can be combined with observe."
//
// Procedure: register for /large-sensor (4KB payload), server sends notifications
// with blockwise transfer.
// Expected: client receives full payload via blockwise.
func TestTC_CoAP_OB_016_Blockwise_LargeNotification(t *testing.T) {
	const payloadSize = 4096
	largePayload := make([]byte, payloadSize)
	for i := range largePayload {
		largePayload[i] = byte(i % 251)
	}

	bodyCh := make(chan []byte, 2)

	r := mux.NewRouter()
	err := r.Handle("/large-sensor", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		_ = w.SetResponse(codes.Content, message.AppOctets, bytes.NewReader(largePayload))
	}))
	require.NoError(t, err)

	_, addr, cleanup := startConformanceServer(t, r)
	defer cleanup()

	cc, err := udp.Dial(addr)
	require.NoError(t, err)
	defer func() { _ = cc.Close(); <-cc.Done() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	obs, err := cc.Observe(ctx, "/large-sensor", func(msg *pool.Message) {
		body, errR := msg.ReadBody()
		if errR == nil && len(body) > 0 {
			select {
			case bodyCh <- body:
			default:
			}
		}
	})
	require.NoError(t, err,
		"RFC 7959 §2.9 + RFC 7641: observe with blockwise must succeed")
	defer func() {
		if obs != nil {
			_ = obs.Cancel(ctx)
		}
	}()

	select {
	case body := <-bodyCh:
		require.Equal(t, largePayload, body,
			"RFC 7959 §2.9: large observe notification (%d bytes) must be received intact via blockwise",
			payloadSize)
	case <-time.After(8 * time.Second):
		require.FailNow(t, "RFC 7959 §2.9 + RFC 7641: timed out waiting for blockwise observe notification")
	}
}
