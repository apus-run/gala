// Package idgen package provides an interface for generating unique IDs
//
//go:generate mockgen -source=$GOFILE -destination=mocks/$GOFILE -package=mocks
package idgen

import (
	"context"
)

type IDGenerator interface {
	GenID(ctx context.Context) (int64, error)
	GenMultiIDs(ctx context.Context, counts int) ([]int64, error)
}
