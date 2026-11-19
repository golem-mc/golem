package registry

import "testing"

func TestEveryWallVariantHasAStandingBlock(t *testing.T) {
	n := 0
	for name, typ := range BlockTypes {
		if !wallVariantTypes[typ] {
			continue
		}
		n++
		s, ok := StandingVariant(name)
		if !ok {
			t.Errorf("%s has no standing block", name)
			continue
		}
		sb, _ := BlockByName(s)
		wb, _ := BlockByName(name)
		if w, _ := sb.WallVariant(); w != wb {
			t.Errorf("WallVariant(%s) = %s, want %s", s, w.Name(), name)
		}
	}
	if n == 0 {
		t.Error("no wall variants")
	}
}
