// LLM usage: generated with deepseek-v4-pro and manually modified.
package tail

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// WatcherFactory creates a Watcher for the given file path.
type WatcherFactory func(t testing.TB, path string) Watcher

// awaitChange runs BlockUntilChange in a goroutine and returns the result.
// If the watcher doesn't return within 5 seconds, the test is failed.
func awaitChange(t testing.TB, w Watcher) (Change, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type result struct {
		change Change
		err    error
	}
	ch := make(chan result, 1)

	go func() {
		c, err := w.BlockUntilChange(ctx)
		ch <- result{change: c, err: err}
	}()

	r := <-ch
	return r.change, r.err
}

// writeFile creates or overwrites a file with the given content.
func writeFile(t testing.TB, path string, content []byte) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeFile: MkdirAll(%s): %v", dir, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writeFile: WriteFile(%s): %v", path, err)
	}
}

// appendFile appends content to an existing file.
func appendFile(t testing.TB, path string, content []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("appendFile: OpenFile(%s): %v", path, err)
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		t.Fatalf("appendFile: Write(%s): %v", path, err)
	}
}

// RunWatcherTests runs the full test suite for a Watcher implementation.
// Each implementation's test file should call this with its factory.
func RunWatcherTests(t *testing.T, factory WatcherFactory) {
	t.Helper()

	t.Run("normal appends", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		writeFile(t, path, []byte("initial\n"))

		w := factory(t, path)
		defer w.Close()

		// Start BlockUntilChange, then append while it waits.
		resultCh := make(chan struct {
			change Change
			err    error
		}, 1)
		go func() {
			c, err := awaitChange(t, w)
			resultCh <- struct {
				change Change
				err    error
			}{c, err}
		}()

		// Give the watcher a moment to start watching.
		time.Sleep(50 * time.Millisecond)
		appendFile(t, path, []byte("more data\n"))

		result := <-resultCh
		if result.err != nil {
			t.Fatalf("expected no error, got: %v", result.err)
		}
		if result.change != ChangeModified {
			t.Fatalf("expected ChangeModified, got %v", result.change)
		}
	})

	t.Run("file created", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")

		w := factory(t, path)
		defer w.Close()

		resultCh := make(chan struct {
			change Change
			err    error
		}, 1)
		go func() {
			c, err := awaitChange(t, w)
			resultCh <- struct {
				change Change
				err    error
			}{c, err}
		}()

		// Give the watcher a moment to start watching.
		time.Sleep(50 * time.Millisecond)
		writeFile(t, path, []byte("new file\n"))

		result := <-resultCh
		if result.err != nil {
			t.Fatalf("expected no error, got: %v", result.err)
		}
		if result.change != ChangeCreated {
			t.Fatalf("expected ChangeCreated, got %v", result.change)
		}
	})

	t.Run("file deleted", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		writeFile(t, path, []byte("some content\n"))

		w := factory(t, path)
		defer w.Close()

		resultCh := make(chan struct {
			change Change
			err    error
		}, 1)
		go func() {
			c, err := awaitChange(t, w)
			resultCh <- struct {
				change Change
				err    error
			}{c, err}
		}()

		time.Sleep(50 * time.Millisecond)
		if err := os.Remove(path); err != nil {
			t.Fatalf("os.Remove: %v", err)
		}

		result := <-resultCh
		if result.err != nil {
			t.Fatalf("expected no error, got: %v", result.err)
		}
		if result.change != ChangeDeleted {
			t.Fatalf("expected ChangeDeleted, got %v", result.change)
		}
	})

	t.Run("truncation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		writeFile(t, path, []byte("some content to truncate\n"))

		w := factory(t, path)
		defer w.Close()

		resultCh := make(chan struct {
			change Change
			err    error
		}, 1)
		go func() {
			c, err := awaitChange(t, w)
			resultCh <- struct {
				change Change
				err    error
			}{c, err}
		}()

		time.Sleep(50 * time.Millisecond)
		if err := os.Truncate(path, 0); err != nil {
			t.Fatalf("os.Truncate: %v", err)
		}

		result := <-resultCh
		if result.err != nil {
			t.Fatalf("expected no error, got: %v", result.err)
		}
		if result.change != ChangeTruncated {
			t.Fatalf("expected ChangeTruncated, got %v", result.change)
		}
	})

	t.Run("log rotate", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		rotatedPath := path + ".1"
		writeFile(t, path, []byte("original content\n"))

		w := factory(t, path)
		defer w.Close()

		// Step 1: rename + create new file -> ChangeDeleted.
		// Do both atomically so PollWatcher skips the "not exist" window
		// and sees the new inode directly.
		if err := os.Rename(path, rotatedPath); err != nil {
			t.Fatalf("os.Rename: %v", err)
		}
		writeFile(t, path, []byte("new content\n"))

		change, err := awaitChange(t, w)
		if err != nil {
			t.Fatalf("step 1: expected no error, got: %v", err)
		}
		if change != ChangeDeleted {
			t.Fatalf("step 1: expected ChangeDeleted after rename, got %v", change)
		}

		// Step 2: FSNotifyWatcher may have a queued ChangeCreated for the
		// new file; PollWatcher does not. Drain it with a short timeout.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer drainCancel()
		if c, err := w.BlockUntilChange(drainCtx); err == nil {
			if c != ChangeCreated {
				t.Fatalf("step 2: expected ChangeCreated if present, got %v", c)
			}
		}

		// Step 3: append to new file -> ChangeModified.
		resultCh := make(chan struct {
			change Change
			err    error
		}, 1)
		go func() {
			c, err := awaitChange(t, w)
			resultCh <- struct {
				change Change
				err    error
			}{c, err}
		}()

		time.Sleep(50 * time.Millisecond)
		appendFile(t, path, []byte("more data\n"))

		result := <-resultCh
		if result.err != nil {
			t.Fatalf("step 3: expected no error, got: %v", result.err)
		}
		if result.change != ChangeModified {
			t.Fatalf("step 3: expected ChangeModified, got %v", result.change)
		}
	})
}
