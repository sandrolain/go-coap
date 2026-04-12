// Package udp_test — RFC 6690 "CoRE Link Format" conformance tests.
//
// Test IDs: WK_001 – WK_010
// Reference: https://www.rfc-editor.org/rfc/rfc6690
//
// These tests exercise the /.well-known/core discovery endpoint and link-format
// encoding as defined in RFC 6690.  The wellknown helper (feat/rfc6690-wellknown-discovery)
// is used where available; tests that probe raw behaviour are also included.
package udp_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/stretchr/testify/require"
)

// ── Helpers ────────────────────────────────────────────────────────────────

// contentFormatLinkFormat is the CoAP Content-Format for application/link-format
// (RFC 6690 §7.3, registered as 40).
const contentFormatLinkFormat = message.MediaType(40)

// ── Tests ──────────────────────────────────────────────────────────────────

// TC_CoAP_WK_001 – TP_CoAP_Discovery_WellKnown_ContentFormat
//
// Reference: RFC 6690 Section 4
// "The resource representation of /.well-known/core MUST be the CoRE Link
// Format described in Section 2."  GET /.well-known/core MUST respond with
// Content-Format = 40 (application/link-format).
//
// Procedure: manually register /.well-known/core handler via mux and check CF.
func TestTC_CoAP_WK_001_WellKnown_ContentFormat(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, contentFormatLinkFormat,
			bytes.NewReader([]byte(`</sensors/temp>`)))
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

	resp, err := cc.Get(ctx, "/.well-known/core")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code(),
		"RFC 6690 §4: GET /.well-known/core MUST return 2.05 Content")
	cf, err := resp.ContentFormat()
	require.NoError(t, err)
	require.Equal(t, contentFormatLinkFormat, cf,
		"RFC 6690 §4: response MUST use Content-Format = 40 (application/link-format)")
}

// TC_CoAP_WK_002 – TP_CoAP_Discovery_WellKnown_LinkFormat
//
// Reference: RFC 6690 Section 2
// "Multiple resource descriptions in a representation are separated by commas."
//
// Procedure: server returns two links. Client verifies body is valid link-format.
func TestTC_CoAP_WK_002_WellKnown_LinkFormat_TwoResources(t *testing.T) {
	const body = `</sensors/temp>,</sensors/light>`
	m := mux.NewRouter()
	err := m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, contentFormatLinkFormat, bytes.NewReader([]byte(body)))
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

	resp, err := cc.Get(ctx, "/.well-known/core")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body())
	require.NoError(t, err)
	respBody := buf.String()
	require.Contains(t, respBody, "</sensors/temp>",
		"RFC 6690 §2: link-format body must contain first resource href")
	require.Contains(t, respBody, "</sensors/light>",
		"RFC 6690 §2: link-format body must contain second resource href")
	require.Contains(t, respBody, ",",
		"RFC 6690 §2: multiple links MUST be separated by commas")
}

// TC_CoAP_WK_003 – TP_CoAP_Discovery_WellKnown_ResourceType
//
// Reference: RFC 6690 Section 3.1
// "The Resource Type 'rt' attribute is an opaque string used to assign an
// application-specific semantic type to a resource."
//
// Procedure: server returns link with rt= attribute. Client verifies it is present.
func TestTC_CoAP_WK_003_WellKnown_ResourceType_Attribute(t *testing.T) {
	const body = `</sensors/temp>;rt="temperature-c"`
	m := mux.NewRouter()
	err := m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, contentFormatLinkFormat, bytes.NewReader([]byte(body)))
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

	resp, err := cc.Get(ctx, "/.well-known/core")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body())
	require.NoError(t, err)
	require.Contains(t, buf.String(), `rt=`, "RFC 6690 §3.1: rt= attribute must be present in link-format")
}

// TC_CoAP_WK_004 – TP_CoAP_Discovery_WellKnown_ContentFormatAttribute
//
// Reference: RFC 6690 Section 3 / CoAP §12.3
// The 'ct' link attribute communicates the media type of a resource.
//
// Procedure: server returns link with ct=0 attribute. Client verifies it.
func TestTC_CoAP_WK_004_WellKnown_ContentFormat_Attribute(t *testing.T) {
	const body = `</data>;ct=0`
	m := mux.NewRouter()
	err := m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, contentFormatLinkFormat, bytes.NewReader([]byte(body)))
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

	resp, err := cc.Get(ctx, "/.well-known/core")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body())
	require.NoError(t, err)
	require.Contains(t, buf.String(), "ct=0", "RFC 6690 §3: ct= attribute must be present")
}

// TC_CoAP_WK_005 – TP_CoAP_Discovery_WellKnown_EmptyOnNoResources
//
// Reference: RFC 6690 Section 4
// "In the absence of any links, a zero-length payload is returned."
//
// Procedure: server registers /.well-known/core with empty payload.
func TestTC_CoAP_WK_005_WellKnown_EmptyPayloadOnNoResources(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, contentFormatLinkFormat, bytes.NewReader([]byte("")))
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

	resp, err := cc.Get(ctx, "/.well-known/core")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code(),
		"RFC 6690 §4: empty /.well-known/core MUST still return 2.05 Content")

	// resp.Body() returns nil when the payload is empty; guard against panic.
	buf := new(bytes.Buffer)
	if body := resp.Body(); body != nil {
		_, err = buf.ReadFrom(body)
		require.NoError(t, err)
	}
	require.Equal(t, "", buf.String(),
		"RFC 6690 §4: zero-length payload when no links are registered")
}

// TC_CoAP_WK_006 – TP_CoAP_Discovery_WellKnown_QueryFilter_rt
//
// Reference: RFC 6690 Section 4.1
// "A server implementing this specification MAY recognize the query part of a
// resource discovery URI as a filter on the resources to be returned."
//
// Procedure: server handles ?.well-known/core?rt=temperature-c and filters.
func TestTC_CoAP_WK_006_WellKnown_QueryFilter_rt(t *testing.T) {
	m := mux.NewRouter()
	err := m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, r *mux.Message) {
		// Apply rt= filter manually
		query, _ := r.Options().GetString(message.URIQuery)
		var body string
		if strings.Contains(query, "rt=temperature-c") {
			body = `</sensors/temp>;rt="temperature-c"`
		} else {
			body = `</sensors/temp>;rt="temperature-c",</sensors/light>`
		}
		errS := w.SetResponse(codes.Content, contentFormatLinkFormat, bytes.NewReader([]byte(body)))
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

	// With filter: should return only temp resource
	resp, err := cc.Get(ctx, "/.well-known/core?rt=temperature-c")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body())
	require.NoError(t, err)
	body := buf.String()
	require.Contains(t, body, "temperature-c",
		"RFC 6690 §4.1: query filter should return matching resources")
	require.NotContains(t, body, "</sensors/light>",
		"RFC 6690 §4.1: query filter should exclude non-matching resources")
}

// TC_CoAP_WK_007 – TP_CoAP_Discovery_WellKnown_QueryFilter_NoSupport_IgnoresQuery
//
// Reference: RFC 6690 Section 4.1
// "Implementations not supporting filtering MUST simply ignore the query string
// and return the whole resource for unicast requests."
//
// Procedure: server ignores query; returns all resources regardless of filter.
func TestTC_CoAP_WK_007_WellKnown_QueryFilter_NoSupport_ReturnsAll(t *testing.T) {
	const fullBody = `</sensors/temp>,</sensors/light>`
	m := mux.NewRouter()
	err := m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		// Server ignores query string, always returns full list
		errS := w.SetResponse(codes.Content, contentFormatLinkFormat, bytes.NewReader([]byte(fullBody)))
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

	// Even with an unrecognized query param, server returns all resources
	resp, err := cc.Get(ctx, "/.well-known/core?unknown=value")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body())
	require.NoError(t, err)
	body := buf.String()
	require.Contains(t, body, "</sensors/temp>",
		"RFC 6690 §4.1: server ignoring query must return all resources")
	require.Contains(t, body, "</sensors/light>",
		"RFC 6690 §4.1: server ignoring query must return all resources")
}

// TC_CoAP_WK_008 – TP_CoAP_Discovery_WellKnown_SzAttribute
//
// Reference: RFC 6690 Section 3.3
// "The maximum size estimate attribute 'sz' is an integer indicating the
// estimated total resource size in bytes."
//
// Procedure: server returns link with sz attribute. Client verifies.
func TestTC_CoAP_WK_008_WellKnown_SzAttribute(t *testing.T) {
	const body = `</firmware>;sz=262144`
	m := mux.NewRouter()
	err := m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, contentFormatLinkFormat, bytes.NewReader([]byte(body)))
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

	resp, err := cc.Get(ctx, "/.well-known/core")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body())
	require.NoError(t, err)
	require.Contains(t, buf.String(), "sz=", "RFC 6690 §3.3: sz= attribute must be present in link-format")
}

// TC_CoAP_WK_009 – TP_CoAP_Discovery_WellKnown_IfAttribute
//
// Reference: RFC 6690 Section 3.2
// "The Interface Description 'if' attribute is a string used to provide a
// name or URI indicating a specific interface definition used to interact
// with the target resource."
//
// Procedure: server returns link with if= attribute. Client verifies.
func TestTC_CoAP_WK_009_WellKnown_IfAttribute(t *testing.T) {
	const body = `</actuators/led>;if="core.a"`
	m := mux.NewRouter()
	err := m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, contentFormatLinkFormat, bytes.NewReader([]byte(body)))
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

	resp, err := cc.Get(ctx, "/.well-known/core")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body())
	require.NoError(t, err)
	require.Contains(t, buf.String(), `if=`, "RFC 6690 §3.2: if= attribute must be present in link-format")
}

// TC_CoAP_WK_010 – TP_CoAP_Discovery_WellKnown_MultipleAttributes
//
// Reference: RFC 6690 Sections 2, 3
// A single link may carry multiple attributes.
//
// Procedure: server returns link with rt=, if=, ct= and sz= combined.
func TestTC_CoAP_WK_010_WellKnown_MultipleAttributes(t *testing.T) {
	const body = `</sensors/temp>;rt="temperature-c";if="sensor";ct=0;sz=4`
	m := mux.NewRouter()
	err := m.Handle("/.well-known/core", mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		errS := w.SetResponse(codes.Content, contentFormatLinkFormat, bytes.NewReader([]byte(body)))
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

	resp, err := cc.Get(ctx, "/.well-known/core")
	require.NoError(t, err)
	require.Equal(t, codes.Content, resp.Code())

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body())
	require.NoError(t, err)
	respBody := buf.String()
	require.Contains(t, respBody, `rt=`, "RFC 6690: rt= must be present")
	require.Contains(t, respBody, `if=`, "RFC 6690: if= must be present")
	require.Contains(t, respBody, `ct=`, "RFC 6690: ct= must be present")
	require.Contains(t, respBody, `sz=`, "RFC 6690: sz= must be present")
}
