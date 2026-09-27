package nntp

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
)

// untrustedListener serves an NNTP greeting over TLS with httptest's
// self-signed certificate, which no system root trusts.
func untrustedListener(t *testing.T) int {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	srv.StartTLS()
	cert := srv.TLS.Certificates[0]
	srv.Close()

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				if _, err := io.WriteString(conn, "200 ready\r\n"); err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, conn)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// A provider whose certificate does not verify is dropped during the TLS
// handshake, before any credentials are sent; insecure_skip_verify connects.
func TestCreateConnectionVerifiesTLS(t *testing.T) {
	provider := config.UsenetProvider{Host: "127.0.0.1", Port: untrustedListener(t), SSL: true}
	c := &Client{logger: zerolog.Nop()}

	conn, err := c.createConnection(t.Context(), provider)
	if err == nil {
		_ = conn.Close()
		t.Fatal("connected to a server whose certificate does not verify")
	}
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); !ok {
		t.Fatalf("error = %v, want a certificate verification error", err)
	}

	provider.InsecureSkipVerify = true
	conn, err = c.createConnection(t.Context(), provider)
	if err != nil {
		t.Fatalf("insecure_skip_verify: %v", err)
	}
	_ = conn.Close()
}
