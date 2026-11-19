package command

import (
	"reflect"
	"testing"
)

// TestCheckBorderDistance pins the bounds and the Double.toString text of the
// two out-of-range errors.
func TestCheckBorderDistance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		word string
		key  string
		with []string
	}{
		{"-60000000", keyDoubleLow, []string{"-5.9999968E7", "-6.0E7"}},
		{"60000000", keyDoubleBig, []string{"5.9999968E7", "6.0E7"}},
		{"59999969", keyDoubleBig, []string{"5.9999968E7", "5.9999969E7"}},
	} {
		err := checkBorderDistance(tc.word)
		if err == nil || err.Key != tc.key || !reflect.DeepEqual(err.With, tc.with) {
			t.Errorf("checkBorderDistance(%q) = %+v, want key %s with %v", tc.word, err, tc.key, tc.with)
		}
	}
	for _, ok := range []string{"0", "59999968", "-59999968", "1.5"} {
		if err := checkBorderDistance(ok); err != nil {
			t.Errorf("checkBorderDistance(%q) = %+v, want nil", ok, err)
		}
	}
}
