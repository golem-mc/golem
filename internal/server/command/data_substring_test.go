package command

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSubstringProcessorCountsUTF16Units(t *testing.T) {
	run := func(text string, words ...string) (string, *pathError) {
		t.Helper()
		processor, perr := substringProcessor(nil, words)
		if perr != nil || processor == nil {
			t.Fatalf("substringProcessor(%q) did not parse: %v", words, perr)
		}
		return processor(text)
	}
	for _, c := range []struct {
		text  string
		words []string
		want  string
	}{
		{"héllo", []string{"1", "3"}, "él"},
		{"añ", []string{"-1"}, "ñ"},
		{"a😀b", []string{"0", "4"}, "a😀b"},
		{"hello", []string{"2"}, "llo"},
	} {
		got, err := run(c.text, c.words...)
		if err != nil || got != c.want {
			t.Errorf("%q range %q = (%q, %v), want %q", c.text, c.words, got, err, c.want)
		}
	}

	// The cut falls inside the surrogate pair; the result stays valid UTF-8.
	got, err := run("a😀b", "0", "2")
	if err != nil || !utf8.ValidString(got) || !strings.HasPrefix(got, "a") {
		t.Errorf("a😀b range 0 2 = (%q, %v), want valid UTF-8 starting with a", got, err)
	}

	_, err = run("a😀b", "0", "5")
	if err == nil || err.key != dataInvalidSubstringKey || !slices.Equal(err.with, []string{"0", "5"}) {
		t.Errorf("a😀b range 0 5 refusal = %+v, want %s [0 5]", err, dataInvalidSubstringKey)
	}
}
