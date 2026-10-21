// Package idgen package provides an interface for generating unique IDs
//
//go:generate mockgen -source=$GOFILE -destination=mocks/$GOFILE -package=mocks
package idgen

import (
	"context"
)

// IDGenerator returns IDs within the configured uniqueness domain.
// GenMultiIDs rejects negative counts; zero returns an empty slice.
// Errors return no partial batch, but may consume IDs. Cancellation or an
// uncertain response must not reclaim a previously allocated interval.
type IDGenerator interface {
	GenID(ctx context.Context) (int64, error)
	GenMultiIDs(ctx context.Context, counts int) ([]int64, error)
}
