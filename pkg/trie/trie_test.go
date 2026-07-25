/*
LLM usage: this test file was generated with claude-sonnet-4-6 with manual modifications.
*/
package trie_test

import (
	"testing"

	"github.com/HT4w5/webjail/pkg/trie"
)

// ---------- PrefixTrie ----------

func TestPrefixTrie_PrecisePrefixMatch(t *testing.T) {
	tb := trie.NewPrefixTrieBuilder[int]()
	tb.Add("foo", 1)
	tb.Add("foobar", 2)
	tb.Add("baz", 3)
	tr := tb.Build()

	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"foo", 1, true},
		{"foobar", 2, true},
		{"baz", 3, true},
		{"fo", 0, false},
		{"fooba", 0, false},
		{"foobarx", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := tr.PrecisePrefixMatch(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("PrecisePrefixMatch(%q) = (%v,%v), want (%v,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestPrefixTrie_LongestPrefixMatch(t *testing.T) {
	tb := trie.NewPrefixTrieBuilder[string]()
	tb.Add("", "root")
	tb.Add("f", "f")
	tb.Add("fo", "fo")
	tb.Add("foo", "foo")
	tb.Add("foobar", "foobar")
	tr := tb.Build()

	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"foobar", "foobar", true},
		{"foobarbaz", "foobar", true}, // longest is "foobar"
		{"foo", "foo", true},
		{"fo", "fo", true},
		{"f", "f", true},
		{"", "root", true},
		{"xyz", "root", true}, // empty string matches
	}
	for _, c := range cases {
		got, ok := tr.LongestPrefixMatch(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("LongestPrefixMatch(%q) = (%v,%v), want (%v,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestPrefixTrie_LongestPrefixMatch_NoMatch(t *testing.T) {
	tb := trie.NewPrefixTrieBuilder[int]()
	tb.Add("foo", 1)
	tr := tb.Build()

	_, ok := tr.LongestPrefixMatch("bar")
	if ok {
		t.Error("expected no match for 'bar'")
	}
}

func TestPrefixTrie_LongestPrefixMatchWithLen(t *testing.T) {
	tb := trie.NewPrefixTrieBuilder[string]()
	tb.Add("", "root")
	tb.Add("f", "f")
	tb.Add("fo", "fo")
	tb.Add("foo", "foo")
	tb.Add("foobar", "foobar")
	tr := tb.Build()

	cases := []struct {
		in     string
		wantV  string
		wantL  int
		wantOK bool
	}{
		{"foobar", "foobar", 6, true},
		{"foobarbaz", "foobar", 6, true},
		{"foo", "foo", 3, true},
		{"fo", "fo", 2, true},
		{"f", "f", 1, true},
		{"", "root", 0, true},
		{"xyz", "root", 0, true},
	}
	for _, c := range cases {
		gotV, gotL, ok := tr.LongestPrefixMatchWithLen(c.in)
		if ok != c.wantOK || gotV != c.wantV || gotL != c.wantL {
			t.Errorf("LongestPrefixMatchWithLen(%q) = (%v,%v,%v), want (%v,%v,%v)",
				c.in, gotV, gotL, ok, c.wantV, c.wantL, c.wantOK)
		}
	}
}

func TestPrefixTrie_LongestPrefixMatchWithLen_NoMatch(t *testing.T) {
	tb := trie.NewPrefixTrieBuilder[int]()
	tb.Add("foo", 1)
	tr := tb.Build()

	_, l, ok := tr.LongestPrefixMatchWithLen("bar")
	if ok {
		t.Error("expected no match for 'bar'")
	}
	if l != 0 {
		t.Errorf("expected length 0 on no match, got %d", l)
	}
}

func TestPrefixTrie_LongestPrefixMatchWithLen_EmptyTrie(t *testing.T) {
	tr := trie.NewPrefixTrie[int]()
	_, l, ok := tr.LongestPrefixMatchWithLen("anything")
	if ok {
		t.Error("empty trie should not match")
	}
	if l != 0 {
		t.Errorf("expected length 0 on empty trie, got %d", l)
	}
}

func TestPrefixTrie_OverwriteValue(t *testing.T) {
	tb := trie.NewPrefixTrieBuilder[int]()
	tb.Add("key", 1)
	tb.Add("key", 99)
	tr := tb.Build()

	got, ok := tr.PrecisePrefixMatch("key")
	if !ok || got != 99 {
		t.Errorf("expected 99, got %v %v", got, ok)
	}
}

func TestPrefixTrie_Empty(t *testing.T) {
	tr := trie.NewPrefixTrie[int]()
	if _, ok := tr.PrecisePrefixMatch("anything"); ok {
		t.Error("empty trie should not match")
	}
	if _, ok := tr.LongestPrefixMatch("anything"); ok {
		t.Error("empty trie should not match")
	}
}

func TestPrefixTrie_EmptyKey(t *testing.T) {
	tb := trie.NewPrefixTrieBuilder[string]()
	tb.Add("", "empty")
	tr := tb.Build()

	got, ok := tr.PrecisePrefixMatch("")
	if !ok || got != "empty" {
		t.Errorf("expected 'empty', got %v %v", got, ok)
	}
	got, ok = tr.LongestPrefixMatch("anything")
	if !ok || got != "empty" {
		t.Errorf("empty prefix should match any string, got %v %v", got, ok)
	}
}

// ---------- SuffixTrie ----------

func TestSuffixTrie_PreciseSuffixMatch(t *testing.T) {
	tb := trie.NewSuffixTrieBuilder[int]()
	tb.Add(".com", 1)
	tb.Add(".org", 2)
	tb.Add("example.com", 3)
	tr := tb.Build()

	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{".com", 1, true},
		{".org", 2, true},
		{"example.com", 3, true},
		{"com", 0, false},
		{"example.org", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := tr.PreciseSuffixMatch(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("SuffixTrie.PreciseSuffixMatch(%q) = (%v,%v), want (%v,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSuffixTrie_LongestSuffixMatch(t *testing.T) {
	tb := trie.NewSuffixTrieBuilder[string]()
	tb.Add("", "root")
	tb.Add("m", "m")
	tb.Add("om", "om")
	tb.Add(".com", ".com")
	tb.Add("e.com", "e.com")
	tr := tb.Build()

	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"example.com", "e.com", true},
		{"foo.com", ".com", true},
		{"foo.com.au", "root", true}, // no registered suffix matches; falls back to empty-string entry
		{"anything", "root", true},
		{"", "root", true},
	}
	for _, c := range cases {
		got, ok := tr.LongestSuffixMatch(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("LongestSuffixMatch(%q) = (%v,%v), want (%v,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSuffixTrie_LongestSuffixMatch_NoMatch(t *testing.T) {
	tb := trie.NewSuffixTrieBuilder[int]()
	tb.Add(".com", 1)
	tr := tb.Build()

	_, ok := tr.LongestSuffixMatch(".org")
	if ok {
		t.Error("expected no match")
	}
}

func TestSuffixTrie_LongestSuffixMatchWithLen(t *testing.T) {
	tb := trie.NewSuffixTrieBuilder[string]()
	tb.Add("", "root")
	tb.Add("m", "m")
	tb.Add("om", "om")
	tb.Add(".com", ".com")
	tb.Add("e.com", "e.com")
	tr := tb.Build()

	cases := []struct {
		in     string
		wantV  string
		wantL  int
		wantOK bool
	}{
		{"example.com", "e.com", 5, true},
		{"foo.com", ".com", 4, true},
		{"foo.com.au", "root", 0, true},
		{"anything", "root", 0, true},
		{"", "root", 0, true},
	}
	for _, c := range cases {
		gotV, gotL, ok := tr.LongestSuffixMatchWithLen(c.in)
		if ok != c.wantOK || gotV != c.wantV || gotL != c.wantL {
			t.Errorf("LongestSuffixMatchWithLen(%q) = (%v,%v,%v), want (%v,%v,%v)",
				c.in, gotV, gotL, ok, c.wantV, c.wantL, c.wantOK)
		}
	}
}

func TestSuffixTrie_LongestSuffixMatchWithLen_NoMatch(t *testing.T) {
	tb := trie.NewSuffixTrieBuilder[int]()
	tb.Add(".com", 1)
	tr := tb.Build()

	_, l, ok := tr.LongestSuffixMatchWithLen(".org")
	if ok {
		t.Error("expected no match")
	}
	if l != 0 {
		t.Errorf("expected length 0 on no match, got %d", l)
	}
}

func TestSuffixTrie_LongestSuffixMatchWithLen_EmptyTrie(t *testing.T) {
	tr := trie.NewSuffixTrie[int]()
	_, l, ok := tr.LongestSuffixMatchWithLen("anything")
	if ok {
		t.Error("empty trie should not match")
	}
	if l != 0 {
		t.Errorf("expected length 0 on empty trie, got %d", l)
	}
}

func TestSuffixTrie_OverwriteValue(t *testing.T) {
	tb := trie.NewSuffixTrieBuilder[int]()
	tb.Add(".com", 1)
	tb.Add(".com", 42)
	tr := tb.Build()

	got, ok := tr.PreciseSuffixMatch(".com")
	if !ok || got != 42 {
		t.Errorf("expected 42, got %v %v", got, ok)
	}
}

func TestSuffixTrie_Empty(t *testing.T) {
	tr := trie.NewSuffixTrie[int]()
	if _, ok := tr.PreciseSuffixMatch("anything"); ok {
		t.Error("empty trie should not match")
	}
	if _, ok := tr.LongestSuffixMatch("anything"); ok {
		t.Error("empty trie should not match")
	}
}

// ---------- Benchmarks ----------

func BenchmarkPrefixTrie_LongestPrefixMatch(b *testing.B) {
	tb := trie.NewPrefixTrieBuilder[int]()
	tb.Add("", 0)
	tb.Add("http", 1)
	tb.Add("https", 2)
	tb.Add("https://example", 3)
	tb.Add("https://example.com", 4)
	tr := tb.Build()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.LongestPrefixMatch("https://example.com/path/to/resource")
	}
}

func BenchmarkSuffixTrie_LongestSuffixMatch(b *testing.B) {
	tb := trie.NewSuffixTrieBuilder[int]()
	tb.Add("", 0)
	tb.Add("m", 1)
	tb.Add("om", 2)
	tb.Add(".com", 3)
	tb.Add("e.com", 4)
	tb.Add("example.com", 5)
	tr := tb.Build()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.LongestSuffixMatch("sub.example.com")
	}
}
