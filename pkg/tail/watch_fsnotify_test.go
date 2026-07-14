// LLM usage: generated with deepseek-v4-pro and manually modified.
package tail

import "testing"

func TestFSNotifyWatcher(t *testing.T) {
	RunWatcherTests(t, func(t testing.TB, path string) Watcher {
		w, err := NewFSNotifyWatcher(path)
		if err != nil {
			t.Fatalf("NewFSNotifyWatcher(%s): %v", path, err)
		}
		return w
	})
}
