package nbt

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestStringSetContains(t *testing.T) {
	var s StringSet
	if s.Contains("") || s.Contains("missing") {
		t.Fatal("zero set contains a key")
	}
	keys := []string{"", "BB", "Aa", "é", "😀"}
	// Equal Java hashes force a collision tree after the table grows.
	for i := range 32 {
		var key strings.Builder
		for bit := range 5 {
			if i&(1<<bit) == 0 {
				key.WriteString("Aa")
			} else {
				key.WriteString("BB")
			}
		}
		keys = append(keys, key.String())
	}
	for i := range 1024 {
		keys = append(keys, fmt.Sprintf("tag%d", i))
	}
	s.Reset(keys)
	for _, key := range keys {
		if !s.Contains(key) {
			t.Fatalf("missing inserted key %q", key)
		}
	}
	for _, key := range keys {
		if !s.Remove(key) || s.Contains(key) {
			t.Fatalf("removal of %q did not remove membership", key)
		}
	}
	if s.Len() != 0 || s.Contains("missing") {
		t.Fatal("emptied set contains a key")
	}
	s.Reset([]string{"BB", "Aa"})
	if allocs := testing.AllocsPerRun(100, func() {
		if !s.Contains("Aa") || s.Contains("missing") {
			t.Fatal("membership after reset")
		}
	}); allocs != 0 {
		t.Fatalf("membership allocated %g times", allocs)
	}
}

func TestStringSetHistory(t *testing.T) {
	t.Parallel()
	s := NewStringSet([]string{"BB", "Aa", "order", "written"})
	if got := s.Values(); !slices.Equal(got, []string{"BB", "Aa", "written", "order"}) {
		t.Fatalf("order = %v", got)
	}
	clone := NewStringSet(s.Values())
	if !s.Remove("BB") || s.Remove("missing") || s.Add("Aa") {
		t.Fatal("set membership")
	}
	s.Add("BB")
	if got := s.Values(); !slices.Equal(got, []string{"Aa", "BB", "written", "order"}) {
		t.Fatalf("reinsert = %v", got)
	}
	if got := clone.Values(); !slices.Equal(got, []string{"BB", "Aa", "written", "order"}) {
		t.Fatalf("clone changed: %v", got)
	}
	s.Reset(strings.Fields("order order written"))
	if got := s.Values(); !slices.Equal(got, []string{"written", "order"}) {
		t.Fatalf("reset = %v", got)
	}
}
