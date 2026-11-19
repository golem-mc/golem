package entity

import "testing"

func TestBlowIgniteSecondsTruncatesEffective(t *testing.T) {
	t.Parallel()
	if got := BlowIgniteSeconds(2.75, 0.1); got != 4 {
		t.Errorf("effective 2.75 ignited %d seconds, want 4", got)
	}
	if got := BlowIgniteSeconds(2.75, 0.9); got != 0 {
		t.Errorf("a roll above effective*0.3 ignited %d seconds, want 0", got)
	}
}
