package wellknown_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	"github.com/plgd-dev/go-coap/v3/mux/wellknown"
	coapNet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatLinkFormat(t *testing.T) {
	tests := []struct {
		name      string
		resources []wellknown.Resource
		want      string
	}{
		{
			name:      "empty",
			resources: nil,
			want:      "",
		},
		{
			name: "single-href-only",
			resources: []wellknown.Resource{
				{Href: "/sensors/temp"},
			},
			want: "</sensors/temp>",
		},
		{
			name: "single-with-rt",
			resources: []wellknown.Resource{
				{Href: "/sensors/temp", ResourceType: "TemperatureSensor"},
			},
			want: `</sensors/temp>;rt="TemperatureSensor"`,
		},
		{
			name: "with-ct",
			resources: []wellknown.Resource{
				{Href: "/sensors/temp", ContentFormat: int(message.TextPlain), HasContentFormat: true},
			},
			want: "</sensors/temp>;ct=0",
		},
		{
			name: "without-ct",
			resources: []wellknown.Resource{
				{Href: "/sensors/temp", ContentFormat: 0},
			},
			want: "</sensors/temp>",
		},
		{
			name: "multiple-resources",
			resources: []wellknown.Resource{
				{Href: "/a"},
				{Href: "/b", ResourceType: "SomeType"},
			},
			want: "</a>,</b>;rt=\"SomeType\"",
		},
		{
			name: "href-normalization",
			resources: []wellknown.Resource{
				{Href: "sensors/temp"},
			},
			want: "</sensors/temp>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wellknown.FormatLinkFormat(tt.resources)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRegisterDiscovery(t *testing.T) {
	l, err := coapNet.NewListenUDP("udp", "")
	require.NoError(t, err)
	defer func() {
		errC := l.Close()
		require.NoError(t, errC)
	}()
	var wg sync.WaitGroup
	defer wg.Wait()

	m := mux.NewRouter()
	resources := []wellknown.Resource{
		{Href: "/sensors/temp", ResourceType: "TemperatureSensor"},
		{Href: "/sensors/light"},
	}
	err = wellknown.RegisterDiscovery(m, resources)
	require.NoError(t, err)

	s := udp.NewServer(options.WithMux(m))
	defer s.Stop()

	wg.Add(1)
	go func() {
		defer wg.Done()
		errS := s.Serve(l)
		assert.NoError(t, errS)
	}()

	cc, err := udp.Dial(l.LocalAddr().String())
	require.NoError(t, err)
	defer func() {
		errC := cc.Close()
		require.NoError(t, errC)
		<-cc.Done()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	got, err := cc.Get(ctx, wellknown.WellKnownCoreURI)
	require.NoError(t, err)
	require.Equal(t, codes.Content, got.Code())

	ct, err := got.ContentFormat()
	require.NoError(t, err)
	assert.Equal(t, message.AppLinkFormat, ct)

	body, err := io.ReadAll(got.Body())
	require.NoError(t, err)

	bodyStr := string(body)
	assert.True(t, strings.Contains(bodyStr, "</sensors/temp>"), "expected /sensors/temp in body: %s", bodyStr)
	assert.True(t, strings.Contains(bodyStr, "</sensors/light>"), "expected /sensors/light in body: %s", bodyStr)

	// Test that the response is the formatted link-format
	expected := wellknown.FormatLinkFormat(resources)
	assert.Equal(t, expected, bodyStr)
}

func TestRegisterDiscoveryBodyIsReusable(t *testing.T) {
	// Ensure multiple GET requests return the same body (body reader is reset each time)
	l, err := coapNet.NewListenUDP("udp", "")
	require.NoError(t, err)
	defer func() {
		errC := l.Close()
		require.NoError(t, errC)
	}()
	var wg sync.WaitGroup
	defer wg.Wait()

	m := mux.NewRouter()
	err = wellknown.RegisterDiscovery(m, []wellknown.Resource{
		{Href: "/res"},
	})
	require.NoError(t, err)

	s := udp.NewServer(options.WithMux(m))
	defer s.Stop()

	wg.Add(1)
	go func() {
		defer wg.Done()
		errS := s.Serve(l)
		assert.NoError(t, errS)
	}()

	cc, err := udp.Dial(l.LocalAddr().String())
	require.NoError(t, err)
	defer func() {
		errC := cc.Close()
		require.NoError(t, errC)
		<-cc.Done()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	for i := range 3 {
		got, errG := cc.Get(ctx, wellknown.WellKnownCoreURI)
		require.NoError(t, errG, "request %d", i)
		require.Equal(t, codes.Content, got.Code())

		body, errR := io.ReadAll(got.Body())
		require.NoError(t, errR)
		assert.Equal(t, "</res>", string(body), "request %d", i)
	}

	// Also verify that FormatLinkFormat produces correct output for coverage
	assert.Equal(t, "</res>", wellknown.FormatLinkFormat([]wellknown.Resource{{Href: "/res"}}))
	assert.Equal(t, "", wellknown.FormatLinkFormat(nil))
	assert.Equal(t, 1, bytes.Count([]byte("a,b"), []byte(",")))
}
