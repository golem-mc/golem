package inventory

import "testing"

func TestLecternPress(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		button   int32
		mayBuild bool
		target   int
		action   LecternAction
	}{
		{"next", LecternButtonNextPage, true, 4, LecternTurn},
		{"previous", LecternButtonPrevPage, false, 2, LecternTurn},
		{"jump", LecternButtonPageJump + 7, false, 7, LecternTurn},
		{"take", LecternButtonTakeBook, true, 3, LecternTake},
		{"take refused", LecternButtonTakeBook, false, 3, LecternNothing},
		{"unknown", 50, true, 3, LecternNothing},
	}
	for _, c := range cases {
		target, action := LecternPress(c.button, 3, c.mayBuild)
		if target != c.target || action != c.action {
			t.Errorf("%s: LecternPress = %d, %d; want %d, %d", c.name, target, action, c.target, c.action)
		}
	}
}
