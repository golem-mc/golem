package registry

import "testing"

func TestCloneItemStatePropsCoverTestBlockAndStatue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		block string
		props map[string]string
		key   string
	}{
		{"minecraft:test_block", map[string]string{"mode": "fail"}, "mode"},
		{"minecraft:copper_golem_statue", map[string]string{"copper_golem_pose": "running", "facing": "north", "waterlogged": "false"}, "copper_golem_pose"},
		{"minecraft:waxed_oxidized_copper_golem_statue", map[string]string{"copper_golem_pose": "sitting", "facing": "north", "waterlogged": "false"}, "copper_golem_pose"},
	}
	for _, c := range cases {
		state, ok := FindState(c.block, c.props)
		if !ok {
			t.Fatalf("no state for %s %v", c.block, c.props)
		}
		got, ok := CloneItemStateProps(state, false)
		if !ok || got[c.key] != c.props[c.key] || len(got) != 1 {
			t.Errorf("%s: CloneItemStateProps = %v, %v; want only %s=%s", c.block, got, ok, c.key, c.props[c.key])
		}
	}
}
