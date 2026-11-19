package command

import "testing"

func TestSelectorRotationRangeParsing(t *testing.T) {
	for _, tc := range []struct {
		selector string
		key      string
	}{
		{"@a[x_rotation=1.2.3]", "parsing.float.invalid"},
		{"@a[y_rotation=1.2.3]", "parsing.float.invalid"},
		{"@a[x_rotation=..]", keyRangeEmpty},
		{"@a[distance=10..1]", keyRangeSwapped},
		{"@a[level=10..1]", keyRangeSwapped},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			_, err := parsedSelector(tc.selector, true)
			if err == nil || err.Key != tc.key {
				t.Fatalf("parse error = %v, want %s", err, tc.key)
			}
		})
	}
}
