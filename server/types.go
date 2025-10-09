package server

import (
	"context"
	"net/url"
)

// Server is transport server.
type Server interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Health() bool
}

// Endpointer is registry endpoint.
type Endpointer interface {
	Endpoint() (*url.URL, error)
}
