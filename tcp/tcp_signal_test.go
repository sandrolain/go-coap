package tcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/plgd-dev/go-coap/v3/message/codes"
	coapNet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/options"
	"github.com/plgd-dev/go-coap/v3/tcp/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func TestConnSendRelease(t *testing.T) {
	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)
	defer func() {
		errC := l.Close()
		require.NoError(t, errC)
	}()
	var wg sync.WaitGroup
	defer wg.Wait()

	// The server detects the Release signal via TCPSignalReceivedHandler.
	var releaseReceived atomic.Bool
	s := NewServer(
		options.WithOnNewConn(func(cc *client.Conn) {
			cc.SetTCPSignalReceivedHandler(func(code codes.Code) {
				if code == codes.Release {
					releaseReceived.Store(true)
				}
			})
		}),
	)
	defer s.Stop()

	wg.Add(1)
	go func() {
		defer wg.Done()
		errS := s.Serve(l)
		assert.NoError(t, errS)
	}()

	cc, err := Dial(l.Addr().String())
	require.NoError(t, err)
	defer func() {
		errC := cc.Close()
		require.NoError(t, errC)
		<-cc.Done()
	}()

	// Ping first to ensure the server-side connection has been established
	// and the TCPSignalReceivedHandler has been installed.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = cc.Ping(ctx)
	require.NoError(t, err)

	// Send Release without options (just a graceful shutdown notice).
	err = cc.SendRelease("", 0)
	require.NoError(t, err)

	// Give the signal time to reach the server.
	require.Eventually(t, releaseReceived.Load, time.Second*3, time.Millisecond*50,
		"server did not receive Release signal")
}

func TestConnSendReleaseWithOptions(t *testing.T) {
	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)
	defer func() {
		errC := l.Close()
		require.NoError(t, errC)
	}()
	var wg sync.WaitGroup
	defer wg.Wait()

	var releaseReceived atomic.Bool
	s := NewServer(
		options.WithOnNewConn(func(cc *client.Conn) {
			cc.SetTCPSignalReceivedHandler(func(code codes.Code) {
				if code == codes.Release {
					releaseReceived.Store(true)
				}
			})
		}),
	)
	defer s.Stop()

	wg.Add(1)
	go func() {
		defer wg.Done()
		errS := s.Serve(l)
		assert.NoError(t, errS)
	}()

	cc, err := Dial(l.Addr().String())
	require.NoError(t, err)
	defer func() {
		errC := cc.Close()
		require.NoError(t, errC)
		<-cc.Done()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = cc.Ping(ctx)
	require.NoError(t, err)

	// Send Release with alternative address and hold-off (RFC 8323 §6.5).
	err = cc.SendRelease("coap://alternative.example.com:5683", 30)
	require.NoError(t, err)

	require.Eventually(t, releaseReceived.Load, time.Second*3, time.Millisecond*50,
		"server did not receive Release signal with options")
}

func TestConnSendAbort(t *testing.T) {
	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)
	defer func() {
		errC := l.Close()
		require.NoError(t, errC)
	}()
	var wg sync.WaitGroup
	defer wg.Wait()

	var abortReceived atomic.Bool
	s := NewServer(
		options.WithOnNewConn(func(cc *client.Conn) {
			cc.SetTCPSignalReceivedHandler(func(code codes.Code) {
				if code == codes.Abort {
					abortReceived.Store(true)
				}
			})
		}),
	)
	defer s.Stop()

	wg.Add(1)
	go func() {
		defer wg.Done()
		errS := s.Serve(l)
		assert.NoError(t, errS)
	}()

	cc, err := Dial(l.Addr().String())
	require.NoError(t, err)
	defer func() {
		errC := cc.Close()
		require.NoError(t, errC)
		<-cc.Done()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = cc.Ping(ctx)
	require.NoError(t, err)

	// Send Abort without Bad-CSM-Option.
	err = cc.SendAbort(0)
	require.NoError(t, err)

	require.Eventually(t, abortReceived.Load, time.Second*3, time.Millisecond*50,
		"server did not receive Abort signal")
}

func TestConnSendAbortWithBadCSMOption(t *testing.T) {
	l, err := coapNet.NewTCPListener("tcp", "")
	require.NoError(t, err)
	defer func() {
		errC := l.Close()
		require.NoError(t, errC)
	}()
	var wg sync.WaitGroup
	defer wg.Wait()

	var abortReceived atomic.Bool
	s := NewServer(
		options.WithOnNewConn(func(cc *client.Conn) {
			cc.SetTCPSignalReceivedHandler(func(code codes.Code) {
				if code == codes.Abort {
					abortReceived.Store(true)
				}
			})
		}),
	)
	defer s.Stop()

	wg.Add(1)
	go func() {
		defer wg.Done()
		errS := s.Serve(l)
		assert.NoError(t, errS)
	}()

	cc, err := Dial(l.Addr().String())
	require.NoError(t, err)
	defer func() {
		errC := cc.Close()
		require.NoError(t, errC)
		<-cc.Done()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = cc.Ping(ctx)
	require.NoError(t, err)

	// Send Abort with Bad-CSM-Option set to an imaginary unrecognised option (RFC 8323 §6.6).
	err = cc.SendAbort(0xFFFF)
	require.NoError(t, err)

	require.Eventually(t, abortReceived.Load, time.Second*3, time.Millisecond*50,
		"server did not receive Abort signal with Bad-CSM-Option")
}
