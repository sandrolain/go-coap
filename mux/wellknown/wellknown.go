// Package wellknown provides helpers for RFC 6690 CoRE resource discovery
// via the /.well-known/core URI.
//
// RFC 6690 defines a link-format media type (application/link-format, ct=40)
// and a standard discovery endpoint. Clients may GET /.well-known/core to
// retrieve a list of resources hosted by the server in CoRE Link Format.
//
// Example:
//
//	m := mux.NewRouter()
//	err := wellknown.RegisterDiscovery(m, []wellknown.Resource{
//	    {Href: "/sensors/temp", ResourceType: "TemperatureSensor", ContentFormat: int(message.TextPlain)},
//	    {Href: "/sensors/light", ResourceType: "LightSensor"},
//	})
package wellknown

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
)

// WellKnownCoreURI is the standard CoAP resource discovery URI (RFC 6690 §2).
const WellKnownCoreURI = "/.well-known/core"

// Resource describes a CoAP resource to be advertised via RFC 6690
// link-format discovery.
type Resource struct {
	// Href is the resource URI-reference (required), e.g. "/sensors/temp".
	Href string

	// ResourceType is the "rt" link attribute (optional).
	ResourceType string

	// Interface is the "if" link attribute (optional).
	Interface string

	// ContentFormat is the "ct" link attribute value (optional).
	// Only included in the link when HasContentFormat is true.
	ContentFormat int

	// HasContentFormat indicates that ContentFormat should be emitted.
	// This is needed because 0 is a valid content format (text/plain).
	HasContentFormat bool

	// Title is the "title" link attribute (optional).
	Title string

	// Size is the "sz" link attribute representing the maximum response size
	// in bytes (optional). Use a negative value to exclude.
	Size int

	// Params contains additional arbitrary link parameters (optional).
	// An empty string value encodes the parameter as a flag (no value).
	Params map[string]string
}

// FormatLinkFormat encodes a slice of Resource values as a CoRE Link
// Format string (RFC 6690 §2).
func FormatLinkFormat(resources []Resource) string {
	parts := make([]string, 0, len(resources))
	for _, res := range resources {
		href := "/" + strings.TrimPrefix(res.Href, "/")
		link := fmt.Sprintf("<%s>", href)

		var attrs []string
		if res.ResourceType != "" {
			attrs = append(attrs, fmt.Sprintf("rt=%q", res.ResourceType))
		}
		if res.Interface != "" {
			attrs = append(attrs, fmt.Sprintf("if=%q", res.Interface))
		}
		if res.HasContentFormat {
			attrs = append(attrs, fmt.Sprintf("ct=%d", res.ContentFormat))
		}
		if res.Title != "" {
			attrs = append(attrs, fmt.Sprintf("title=%q", res.Title))
		}
		if res.Size > 0 {
			attrs = append(attrs, fmt.Sprintf("sz=%d", res.Size))
		}
		for k, v := range res.Params {
			if v != "" {
				attrs = append(attrs, fmt.Sprintf("%s=%s", k, v))
			} else {
				attrs = append(attrs, k)
			}
		}

		if len(attrs) > 0 {
			link += ";" + strings.Join(attrs, ";")
		}
		parts = append(parts, link)
	}
	return strings.Join(parts, ",")
}

// RegisterDiscovery registers a GET handler on /.well-known/core that
// responds with the given resources encoded in CoRE Link Format
// (application/link-format, RFC 6690 §2). Returns the error from
// mux.Router.Handle if pattern registration fails.
func RegisterDiscovery(router *mux.Router, resources []Resource) error {
	body := []byte(FormatLinkFormat(resources))
	return router.Handle(WellKnownCoreURI, mux.HandlerFunc(func(w mux.ResponseWriter, _ *mux.Message) {
		if err := w.SetResponse(codes.Content, message.AppLinkFormat, bytes.NewReader(body)); err != nil {
			return
		}
	}))
}
