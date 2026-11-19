package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// scoreHolder is the player as the scoreboard names a score holder.
func (c *client) scoreHolder() scoreboard.Holder {
	return scoreboard.Holder{Name: c.player.Name, Display: func() nbt.Tag { return protocol.TextTag(c.DisplayName()) }}
}

// tickScoreCriteria writes the health, food, air, armor, xp and level
// objectives when their values change. Only the tick loop calls it.
// vanilla: ServerPlayer.doTick
func (c *client) tickScoreCriteria() {
	reset := c.scoreResample.Swap(false)
	if reset {
		c.scoreSampler.Reset()
	}
	v := c.player.Vitals()
	var armor float64
	c.player.Attributes(func(a *player.Attributes) { armor, _ = rules.ArmourValue(a) })
	samples := scoreboard.Samples{
		Health: int32(math.Ceil(v.Health + v.Absorption)),
		Food:   v.Food,
		Air:    int32(v.AirSupply),
		Armor:  int32(armor),
		XP:     int32(v.ExperienceTotal),
		Level:  int32(v.ExperienceLevel),
	}
	c.scoreSampler.Apply(c.srv.scoreboard, c.scoreHolder, samples)
	for _, board := range c.srv.privateScoreboards() {
		if c.privateScoreSamplers == nil {
			c.privateScoreSamplers = make(map[*scoreboard.Board]*scoreboard.Sampler)
		}
		sampler := c.privateScoreSamplers[board]
		if sampler == nil {
			sampler = &scoreboard.Sampler{}
			c.privateScoreSamplers[board] = sampler
		}
		if reset {
			sampler.Reset()
		}
		sampler.Apply(board, c.scoreHolder, samples)
	}
}

// scoreDeath counts a death for the deathCount objectives.
// vanilla: ServerPlayer.die
func (c *client) scoreDeath() { c.bumpCriteria("deathCount") }

// scoreKill counts a kill of victim: player_kills or mob_kills,
// totalKillCount, playerKillCount for a player victim, and the
// teamkill.<color> and killedByTeam.<color> objectives of each side's team.
// vanilla: ServerPlayer.awardKillScore
func (c *client) scoreKill(victim scoreboard.Holder, victimIsPlayer bool) {
	c.bumpCriteria("totalKillCount")
	if victimIsPlayer {
		c.awardStat(player.Custom(registry.CustomStatPlayerKills), 1)
		c.bumpCriteria("playerKillCount")
	} else {
		c.awardStat(player.Custom(registry.CustomStatMobKills), 1)
	}
	self := c.scoreHolder()
	c.scoreTeamKill(self, victim, "teamkill.")
	c.scoreTeamKill(victim, self, "killedByTeam.")
}

// scoreTeamKill bumps source in every prefix+<color> objective, where color
// is the colour of target's team; nothing when target has no coloured team.
// vanilla: ServerPlayer.handleTeamKill
func (c *client) scoreTeamKill(source, target scoreboard.Holder, prefix string) {
	c.scoreTeamKillOn(c.srv.scoreboard, source, target, prefix)
	for _, board := range c.srv.privateScoreboards() {
		c.scoreTeamKillOn(board, source, target, prefix)
	}
}
func (c *client) scoreTeamKillOn(board *scoreboard.Board, source, target scoreboard.Holder, prefix string) {
	team, ok := board.MemberTeam(target.Name)
	if !ok || team.Color == scoreboard.NoColor {
		return
	}
	board.UpdateCriteria(prefix+scoreboard.TeamColors()[team.Color], source, func(v int32) int32 { return v + 1 })
}

func (c *client) bumpCriteria(criteria string) {
	c.srv.scoreboard.UpdateCriteria(criteria, c.scoreHolder(), func(v int32) int32 { return v + 1 })
	for _, board := range c.srv.privateScoreboards() {
		board.UpdateCriteria(criteria, c.scoreHolder(), func(v int32) int32 { return v + 1 })
	}
}
