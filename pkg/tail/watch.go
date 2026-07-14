package tail

import "context"

type Change uint8

const (
	ChangeCreated Change = iota
	ChangeDeleted
	ChangeModified
	ChangeTruncated
)

// Watcher watches for file changes for tailing.
// Watcher is NOT concurrent-safe.
type Watcher interface {
	BlockUntilChange(ctx context.Context) (Change, error)
	Close() error
}
