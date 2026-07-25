package ban

import (
	"context"
	"errors"
	"net/netip"
)

// ErrNotFound is returned by Get when the address is not in the jail.
var ErrNotFound = errors.New("record not found")

// Jail stores Records.
//
// Jail may return expired records.
// Jail is free to decide whether and when to clean up expired records.
type Jail interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
	Add(ctx context.Context, r *Record) error
	Del(ctx context.Context, addr netip.Addr) error
	Get(ctx context.Context, addr netip.Addr) (Record, error)
	ForEach(ctx context.Context, start netip.Addr, end netip.Addr, f func(r *Record) error) error
	Stats() any
}
