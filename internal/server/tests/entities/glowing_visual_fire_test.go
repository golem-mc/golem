package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestGlowingAndVisualFireTagsAreDrawn summons non-mob bodies with Glowing
// and HasVisualFire set: each spawn's shared flags carry the glowing bit,
// the stand waits for its first tick to draw flames, and a zombie with
// HasVisualFire keeps the on-fire bit without burning.
// vanilla: Entity.load, Entity.setGlowingTag, Entity.setSharedFlagOnFire
func TestGlowingAndVisualFireTagsAreDrawn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)
	// Night, so the zombie's only flames are the drawn ones.
	c.ChatCommand("time set midnight")

	for _, body := range []struct {
		command string
		typ     int32
		want    uint8
	}{
		{`summon minecraft:item 0.5 64 2.5 {Item:{id:"minecraft:stone",count:1},Glowing:1b,NoGravity:1b}`,
			protocol.EntityTypeItem, protocol.EntityFlagGlowing},
		{`summon minecraft:armor_stand 1.5 64 2.5 {Glowing:1b,HasVisualFire:1b,NoGravity:1b}`,
			protocol.EntityTypeArmorStand, protocol.EntityFlagGlowing},
		// PrimedTnt.tick skips baseTick, so its visual fire is never drawn.
		{`summon minecraft:tnt 2.5 64 2.5 {Glowing:1b,HasVisualFire:1b,fuse:200,NoGravity:1b}`,
			protocol.EntityTypeTnt, protocol.EntityFlagGlowing},
		{`summon minecraft:falling_block 3.5 64 2.5 {Glowing:1b,HasVisualFire:1b,NoGravity:1b}`,
			protocol.EntityTypeFallingBlock, protocol.EntityFlagGlowing},
		// AbstractMinecart.tick skips baseTick too.
		{`summon minecraft:minecart 4.5 64 2.5 {Glowing:1b,HasVisualFire:1b}`, protocol.EntityTypeMinecart, protocol.EntityFlagGlowing},
		{`summon minecraft:oak_boat 6.5 64 2.5 {Glowing:1b,HasVisualFire:1b,NoGravity:1b}`,
			protocol.EntityTypeOakBoat, protocol.EntityFlagGlowing | protocol.EntityFlagOnFire},
	} {
		c.ChatCommand(body.command)
		spawn := c.AwaitSpawn(body.typ)
		e := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexEntityFlags)
		if e == nil || e.ByteVal != body.want {
			t.Errorf("%s: the spawn's shared flags = %+v, want 0x%02x", body.command, e, body.want)
		}
	}

	c.ChatCommand(`summon minecraft:zombie 0.5 64 4.5 {HasVisualFire:1b,NoAI:1b,NoGravity:1b}`)
	zombie := c.AwaitSpawn(protocol.EntityTypeZombie)
	if e := servertest.MetaOf(c.AwaitMetadataFor(zombie.EntityID), protocol.MetadataIndexEntityFlags); e == nil || e.ByteVal&protocol.EntityFlagOnFire == 0 {
		t.Fatalf("the zombie's spawn shared flags = %+v, want the on-fire bit", e)
	}
	servertest.RunTicks(t, srv, 40)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != zombie.EntityID {
			return
		}
		entries, _ := servertest.ReadMetadata(r)
		if e := servertest.MetaOf(entries, protocol.MetadataIndexEntityFlags); e != nil && e.ByteVal&protocol.EntityFlagOnFire == 0 {
			t.Errorf("the zombie's shared flags turned to 0x%02x, want the on-fire bit kept", e.ByteVal)
		}
	})
	if hp, ok := srv.MobHealth(zombie.EntityID); !ok || hp != 20 {
		t.Errorf("the zombie's health after 40 ticks = %v (found %v), want 20: visual fire does no damage", hp, ok)
	}
}
