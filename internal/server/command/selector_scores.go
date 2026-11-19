package command

import (
	"github.com/golem-mc/golem/internal/commandsyntax"
	"github.com/golem-mc/golem/internal/server/advancement"
)

// advancementsFilter passes a player whose progress matches every entry. An
// advancement the tree lacks, or a criterion the advancement lacks, fails.
func advancementsFilter(expected map[string]*commandsyntax.AdvancementWant) func(Entity) bool {
	return func(e Entity) bool {
		target, ok := e.(Target)
		if !ok {
			return false
		}
		progress := target.Advancements()
		if progress == nil {
			return false
		}
		// A cached selector must see the tree installed by a pack reload.
		tree := advancement.Current()
		for id, want := range expected {
			n := tree.Get(id)
			if n == nil {
				return false
			}
			if want.Criteria == nil {
				if progress.Done(n) != want.Done {
					return false
				}
				continue
			}
			for name, value := range want.Criteria {
				if !n.HasCriterion(name) || progress.Obtained(n, name) != value {
					return false
				}
			}
		}
		return true
	}
}

// scoresFilter passes an entity whose score holds, for every objective the
// scores option named, a value inside the range given.
// vanilla: EntitySelectorOptions scores option
func (sel *Selector) scoresFilter(src Source) func(Entity) bool {
	board := src.Scoreboard()
	return func(e Entity) bool {
		holder := entityHolder(e).Name
		for objective, want := range sel.Scores {
			score, ok := board.Score(holder, objective)
			if !ok || !want.Matches(float64(score.Value)) {
				return false
			}
		}
		return true
	}
}

// teamsFilter passes an entity whose team, "" for none, is each positive
// team option's name and none of the negated ones'.
// vanilla: EntitySelectorOptions team option
func (sel *Selector) teamsFilter(src Source) func(Entity) bool {
	board := src.Scoreboard()
	return func(e Entity) bool {
		team, _ := board.PlayerTeam(entityHolder(e).Name)
		if body, ok := e.(TeamEntity); ok {
			team = body.Team()
		}
		for _, want := range sel.Teams {
			if (team == want.Name) == want.Invert {
				return false
			}
		}
		return true
	}
}
