package tail

import (
	"context"
	"os"
	"time"
)

var _ Watcher = (*PollWatcher)(nil)

type PollWatcher struct {
	interval time.Duration
	filepath string
	fi       os.FileInfo
	size     int64
	modified time.Time
}

func NewPollWatcher(path string, interval time.Duration) (*PollWatcher, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			fi = nil
		} else {
			return nil, err
		}
	}

	pw := &PollWatcher{
		filepath: path,
		interval: interval,
		fi:       fi,
	}

	if fi != nil {
		pw.size = fi.Size()
		pw.modified = fi.ModTime()
	}

	return pw, nil
}

func (pw *PollWatcher) BlockUntilChange(ctx context.Context) (Change, error) {
	ticker := time.NewTicker(pw.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
			fi, err := os.Stat(pw.filepath)
			if err != nil {
				if os.IsNotExist(err) {
					if pw.fi != nil {
						pw.fi = nil
						return ChangeDeleted, nil
					}
					continue
				}
				return 0, err
			}

			lastSize := pw.size
			lastModified := pw.modified
			lastFi := pw.fi
			pw.size = fi.Size()
			pw.modified = fi.ModTime()
			pw.fi = fi

			if lastFi == nil {
				return ChangeCreated, nil
			}

			if !os.SameFile(lastFi, fi) {
				return ChangeDeleted, nil
			}

			if pw.size < lastSize {
				return ChangeTruncated, nil
			}

			if pw.size > lastSize || !pw.modified.Equal(lastModified) {
				return ChangeModified, nil
			}
		}
	}
}

func (pw *PollWatcher) Close() error {
	return nil // noop
}
