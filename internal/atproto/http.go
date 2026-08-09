package atproto

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

const (
	httpTimeout = 10 * time.Second
	dialTimeout = 5 * time.Second
)

type ipLookup func(context.Context, string) ([]netip.Addr, error)

func newPublicHTTPClient(lookup ipLookup) *http.Client {
	if lookup == nil {
		resolver := net.DefaultResolver
		lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return resolver.LookupNetIP(ctx, "ip", host)
		}
	}
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext:           publicDialContext(dialer, lookup),
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   httpTimeout,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if !safePublicURL(request.URL) {
				return errorsUnsafeRedirect
			}
			return nil
		},
	}
}

func publicDialContext(dialer *net.Dialer, lookup ipLookup) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("validate outbound address: %w", err)
		}
		addresses, err := lookup(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve outbound host: %w", err)
		}
		if len(addresses) == 0 {
			return nil, errorsNoPublicAddress
		}
		for _, address := range addresses {
			if !isPublicIP(address) {
				return nil, errorsNoPublicAddress
			}
		}
		var lastErr error
		for _, address := range addresses {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, fmt.Errorf("connect to public host: %w", lastErr)
	}
}

var errorsNoPublicAddress = fmt.Errorf("outbound host has no permitted public address")
var errorsUnsafeRedirect = fmt.Errorf("outbound redirect is not a permitted HTTPS URL")

func safePublicURL(value *url.URL) bool {
	return value != nil && value.Scheme == "https" && value.Hostname() != "" && value.User == nil
}

func isPublicIP(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsValid() && address.IsGlobalUnicast() &&
		!address.IsLoopback() && !address.IsPrivate() &&
		!address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() &&
		!address.IsMulticast() && !address.IsUnspecified()
}
