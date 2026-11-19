package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// itemEnchContext is the holder's ENCHANTED_ENTITY context; it has no tool.
// vanilla: Enchantment.entityContext
func (c *client) itemEnchContext() *rules.EnchContext {
	pos := c.player.Position()
	ctx := entityItemEnchContext(c.theWorld(), pos.X, pos.Y, pos.Z, protocol.EntityTypePlayer)
	ctx.Rand = c.ownDice()
	ctx.User = rules.EnchUser{Riding: c.isPassenger(), FallFlying: c.player.Appearance().FallFlying,
		InWater: c.waterAt(pos) != rules.Dry, IsPlayer: true,
		GameMode: [...]string{"survival", "creative", "adventure", "spectator"}[c.player.GameMode()], Food: c.player.Vitals().Food}
	return ctx
}

func entityItemEnchContext(w *world.World, x, y, z float64, kind int32) *rules.EnchContext {
	ctx := new(rules.EnchContext)
	fillEntityItemEnchContext(ctx, w, x, y, z, kind)
	return ctx
}

// fillEntityItemEnchContext overwrites ctx with entityItemEnchContext's value.
func fillEntityItemEnchContext(ctx *rules.EnchContext, w *world.World, x, y, z float64, kind int32) {
	bx, by, bz := rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z)
	*ctx = rules.EnchContext{Victim: kind, Attacker: -1, DirectAttacker: -1,
		Thundering: w.IsThundering(), CanSeeSky: w.SkyLightAt(bx, by, bz) == 15,
		Block: registry.BlockOf(w.BlockAt(bx, by, bz))}
}
