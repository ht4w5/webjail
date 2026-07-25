/*
This package implements PrefixTrie and SuffixTrie for rule engine expressions.
Trie is immutable once built and concurrent-safe.
*/

package trie

type node[T any] struct {
	children [256]int // 0 -> empty
	valueIdx int      // 0 -> empty
}

// --- PrefixTrie ---

type PrefixTrie[T any] struct {
	nodes  []node[T]
	values []T
}

type PrefixTrieBuilder[T any] PrefixTrie[T]

func NewPrefixTrie[T any]() PrefixTrie[T] {
	return PrefixTrie[T]{
		nodes: []node[T]{{}},
	}
}

func NewPrefixTrieBuilder[T any]() PrefixTrieBuilder[T] {
	return PrefixTrieBuilder[T](NewPrefixTrie[T]())
}

func (tb *PrefixTrieBuilder[T]) Add(prefix string, value T) {
	cur := 0
	// Find node (create if not exist)
	for i := range len(prefix) {
		b := prefix[i]
		next := tb.nodes[cur].children[b]
		if next == 0 {
			// Create new node
			tb.nodes = append(tb.nodes, node[T]{})
			next = len(tb.nodes) - 1
			tb.nodes[cur].children[b] = next
		}
		cur = next
	}

	// Store value
	old := tb.nodes[cur].valueIdx
	if old != 0 {
		tb.values[old-1] = value // Overwrite
	} else {
		tb.values = append(tb.values, value)
		tb.nodes[cur].valueIdx = len(tb.values)
	}
}

func (tb PrefixTrieBuilder[T]) Build() PrefixTrie[T] {
	return PrefixTrie[T](tb)
}

func (trie PrefixTrie[T]) PrecisePrefixMatch(s string) (value T, ok bool) {
	cur := 0
	for i := range len(s) {
		next := trie.nodes[cur].children[s[i]]
		if next == 0 {
			return
		}
		cur = next
	}
	idx := trie.nodes[cur].valueIdx
	if idx == 0 {
		return
	}
	return trie.values[idx-1], true
}

func (trie PrefixTrie[T]) longestPrefixMatch(s string) (value T, length int, ok bool) {
	cur := 0
	bestValueIdx := 0
	bestLen := 0
	if trie.nodes[0].valueIdx != 0 {
		bestValueIdx = trie.nodes[0].valueIdx
	}
	for i := range len(s) {
		next := trie.nodes[cur].children[s[i]]
		if next == 0 {
			break
		}
		cur = next
		if idx := trie.nodes[cur].valueIdx; idx != 0 {
			bestValueIdx = idx
			bestLen = i + 1
		}
	}
	if bestValueIdx == 0 {
		return
	}
	return trie.values[bestValueIdx-1], bestLen, true
}

func (trie PrefixTrie[T]) LongestPrefixMatch(s string) (value T, ok bool) {
	v, _, ok := trie.longestPrefixMatch(s)
	return v, ok
}

func (trie PrefixTrie[T]) LongestPrefixMatchWithLen(s string) (value T, length int, ok bool) {
	return trie.longestPrefixMatch(s)
}

// --- SuffixTrie ---

type SuffixTrie[T any] struct {
	nodes  []node[T]
	values []T
}

type SuffixTrieBuilder[T any] SuffixTrie[T]

func NewSuffixTrie[T any]() SuffixTrie[T] {
	return SuffixTrie[T]{
		nodes: []node[T]{{}},
	}
}

func NewSuffixTrieBuilder[T any]() SuffixTrieBuilder[T] {
	return SuffixTrieBuilder[T](NewSuffixTrie[T]())
}

func (tb *SuffixTrieBuilder[T]) Add(suffix string, value T) {
	cur := 0
	// Find node (create if not exist)
	for i := len(suffix) - 1; i >= 0; i-- {
		b := suffix[i]
		next := tb.nodes[cur].children[b]
		if next == 0 {
			// Create new node
			tb.nodes = append(tb.nodes, node[T]{})
			next = len(tb.nodes) - 1
			tb.nodes[cur].children[b] = next
		}
		cur = next
	}

	// Store value
	old := tb.nodes[cur].valueIdx
	if old != 0 {
		tb.values[old-1] = value // Overwrite
	} else {
		tb.values = append(tb.values, value)
		tb.nodes[cur].valueIdx = len(tb.values)
	}
}

func (tb SuffixTrieBuilder[T]) Build() SuffixTrie[T] {
	return SuffixTrie[T](tb)
}

func (trie SuffixTrie[T]) PreciseSuffixMatch(s string) (value T, ok bool) {
	if len(trie.nodes) == 0 {
		return
	}
	cur := 0
	for i := len(s) - 1; i >= 0; i-- {
		next := trie.nodes[cur].children[s[i]]
		if next == 0 {
			return
		}
		cur = next
	}
	idx := trie.nodes[cur].valueIdx
	if idx == 0 {
		return
	}
	return trie.values[idx-1], true
}

func (trie SuffixTrie[T]) longestSuffixMatch(s string) (value T, length int, ok bool) {
	if len(trie.nodes) == 0 {
		return
	}
	cur := 0
	bestValueIdx := 0
	bestLen := 0
	if trie.nodes[0].valueIdx != 0 {
		bestValueIdx = trie.nodes[0].valueIdx
	}
	for i := len(s) - 1; i >= 0; i-- {
		next := trie.nodes[cur].children[s[i]]
		if next == 0 {
			break
		}
		cur = next
		if idx := trie.nodes[cur].valueIdx; idx != 0 {
			bestValueIdx = idx
			bestLen = len(s) - i
		}
	}
	if bestValueIdx == 0 {
		return
	}
	return trie.values[bestValueIdx-1], bestLen, true
}

func (trie SuffixTrie[T]) LongestSuffixMatch(s string) (value T, ok bool) {
	v, _, ok := trie.longestSuffixMatch(s)
	return v, ok
}

func (trie SuffixTrie[T]) LongestSuffixMatchWithLen(s string) (value T, length int, ok bool) {
	return trie.longestSuffixMatch(s)
}
