// LLM usage: generated with deepseek-v4-pro and manually modified.
package tail

import (
	"testing"
	"time"
)

func TestPollWatcher(t *testing.T) {
	RunWatcherTests(t, func(t testing.TB, path string) Watcher {
		w, err := NewPollWatcher(path, 10*time.Millisecond)
		if err != nil {
			t.Fatalf("NewPollWatcher(%s, 10ms): %v", path, err)
		}
		return w
	})
}
