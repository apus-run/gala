package mcp

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestServer(t *testing.T) {
	var (
		ctx = context.Background()
		srv = NewServer("test", "v1.0.0", Address(":0"))
	)
	go func() {
		if err := srv.Start(ctx); err != nil {
			panic(err)
		}
	}()
	time.Sleep(time.Second)
	if err := srv.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointUsesListenerAddress(t *testing.T) {
	srv := NewServer("test", "v1.0.0", Address("127.0.0.1:0"))

	endpoint, err := srv.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Scheme != "http" {
		t.Fatalf("endpoint scheme = %q, want http", endpoint.Scheme)
	}

	host, port, err := net.SplitHostPort(endpoint.Host)
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("endpoint host = %q, want 127.0.0.1", host)
	}
	if port == "" || port == "0" {
		t.Fatalf("endpoint port = %q, want allocated port", port)
	}

	if err := srv.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
