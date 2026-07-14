package tail

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

var _ Watcher = (*FSNotifyWatcher)(nil)

type FSNotifyWatcher struct {
	watcher  *fsnotify.Watcher
	filepath string
	size     int64
}

func NewFSNotifyWatcher(path string) (*FSNotifyWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	cleanPath := filepath.Clean(path)
	parentDir := filepath.Dir(cleanPath)

	// Watch parent and filter by Event.Name
	err = watcher.Add(parentDir)
	if err != nil {
		watcher.Close()
		return nil, err
	}

	// Get initial size
	var size int64
	fi, err := os.Stat(path)
	if err == nil {
		size = fi.Size()
	}

	return &FSNotifyWatcher{
		watcher:  watcher,
		filepath: cleanPath,
		size:     size,
	}, nil
}

func (fw *FSNotifyWatcher) Close() error {
	return fw.watcher.Close()
}

func (fw *FSNotifyWatcher) BlockUntilChange(ctx context.Context) (Change, error) {
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return 0, errors.New("watcher channel closed")
			}
			return 0, err

		case event, ok := <-fw.watcher.Events:
			if !ok {
				return 0, errors.New("watcher channel closed")
			}

			if filepath.Clean(event.Name) != fw.filepath {
				continue
			}

			switch {
			case event.Has(fsnotify.Create):
				if fi, err := os.Stat(fw.filepath); err == nil {
					fw.size = fi.Size()
				}
				return ChangeCreated, nil

			case event.Has(fsnotify.Write):
				if fi, err := os.Stat(fw.filepath); err == nil {
					lastSize := fw.size
					fw.size = fi.Size()

					if fw.size < lastSize {
						return ChangeTruncated, nil
					} else {
						return ChangeModified, nil
					}
				} else {
					return 0, fmt.Errorf("os.Stat failed: %w", err)
				}

			case event.Has(fsnotify.Remove),
				event.Has(fsnotify.Rename):
				return ChangeDeleted, nil
			}
		}
	}
}
