package command

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/nbt"
)

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
