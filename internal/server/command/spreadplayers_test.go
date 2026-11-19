package command

import (
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/nbt"
)

func TestJavaFixed(t *testing.T) {
	for _, c := range []struct {
		in   float64
		prec int
		want string
	}{
		{0.125, 2, "0.13"},
		{0, 2, "0.00"},
		{12.344, 2, "12.34"},
		{9.999, 2, "10.00"},
		{1.005, 2, "1.01"},
		{10.25, 1, "10.3"},
		{10.5, 0, "11"},
		{-0.125, 2, "-0.13"},
		{-0.001, 2, "-0.00"},
		{math.Copysign(0, -1), 2, "-0.00"},
		{1e21, 1, "1" + zeros(21) + ".0"},
		{math.MaxFloat64, 2, "17976931348623157" + zeros(292) + ".00"},
		{math.Inf(1), 2, "Infinity"},
		{math.Inf(-1), 2, "-Infinity"},
		{math.NaN(), 2, "NaN"},
	} {
		if got := javaFixed(c.in, c.prec); got != c.want {
			t.Errorf("javaFixed(%v, %d) = %q, want %q", c.in, c.prec, got, c.want)
		}
	}
}

func zeros(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '0'
	}
	return string(b)
}

type spreadPlayer struct {
	Target
	name string
}

func (p spreadPlayer) Name() string { return p.name }

func TestSpreadGroupsSharesOnePointWhenRespectingTeams(t *testing.T) {
	board := scoreboard.New(nil)
	board.AddTeam("red", nbt.String("red"))
	board.AddTeam("blue", nbt.String("blue"))
	board.JoinTeam("red", []string{"Alice", "Carol", "mob"})
	board.JoinTeam("blue", []string{"Bob"})
	entities := []Entity{
		spreadPlayer{name: "Bob"}, spreadPlayer{name: "Alice"},
		spreadPlayer{name: "Carol"}, spreadPlayer{name: "Teamless"},
		spreadPlayer{name: "AlsoTeamless"}, spreadMob{},
	}
	for _, tc := range []struct {
		name         string
		entities     []Entity
		respectTeams bool
		want         []int
		count        int
	}{
		{"teams in encounter order", entities, true, []int{0, 1, 1, 2, 2, 2}, 3},
		{"individuals", entities, false, []int{0, 1, 2, 3, 4, 5}, 6},
		{"teamless", entities[3:], true, []int{0, 0, 0}, 1},
		{"mob first", []Entity{spreadMob{}, entities[0], entities[3]}, true, []int{0, 1, 0}, 2},
		{"empty", nil, true, []int{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := spreadGroups(tc.entities, tc.respectTeams, board)
			if got.count != tc.count || !slices.Equal(got.of, tc.want) {
				t.Errorf("groups = %+v, want count %d, groups %v", got, tc.count, tc.want)
			}
		})
	}
}

type spreadMob struct{ Entity }

func (spreadMob) Name() string { return "mob" }
