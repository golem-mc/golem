package rules_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// The expected values are the first eight bytes of the SHA-256 of the
// little-endian seed, read little-endian, as Guava's hashLong(seed).asLong().
func TestObfuscateSeed(t *testing.T) {
	for seed, want := range map[int64]int64{
		12345: 293737985876514017,
	} {
		if got := rules.ObfuscateSeed(seed); got != want {
			t.Errorf("ObfuscateSeed(%d) = %d, want %d", seed, got, want)
		}
	}
}
