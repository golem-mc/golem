package command

import "testing"

// TestWildcardMatch checks the resource selector's matching: * spans any
// run, ? one character, and the case counts.
// vanilla: FilenameUtils.wildcardMatch
func TestWildcardMatch(t *testing.T) {
	for _, tc := range []struct {
		name, pattern string
		want          bool
	}{
		{"minecraft:foo", "minecraft:foo", true},
		{"minecraft:foo", "*:foo", true},
		{"minecraft:foo", "*", true},
		{"minecraft:foo", "minecraft:f?o", true},
		{"minecraft:foo", "minecraft:f?", false},
		{"minecraft:foo/bar", "minecraft:*bar", true},
		{"minecraft:foo", "minecraft:*o*o", true},
		{"minecraft:foo", "minecraft:FOO", false},
		{"minecraft:foo", "pk:*", false},
		{"", "*", true},
	} {
		if got := wildcardMatch(tc.name, tc.pattern); got != tc.want {
			t.Errorf("wildcardMatch(%q, %q) = %v, want %v", tc.name, tc.pattern, got, tc.want)
		}
	}
}

// TestReadResourcePattern checks that a pattern stops at the first
// character it cannot hold and takes the minecraft namespace when it names
// none. vanilla: ResourceSelectorArgument.readPattern, ensureNamespaced
func TestReadResourcePattern(t *testing.T) {
	for _, tc := range []struct {
		word, pattern string
		n             int
	}{
		{"foo", "minecraft:foo", 3},
		{"*:foo", "*:foo", 5},
		{"pk:a?c!x", "pk:a?c", 6},
		{"Foo", "minecraft:", 0},
	} {
		if pattern, n := readResourcePattern(tc.word); pattern != tc.pattern || n != tc.n {
			t.Errorf("readResourcePattern(%q) = %q, %d, want %q, %d", tc.word, pattern, n, tc.pattern, tc.n)
		}
	}
}
