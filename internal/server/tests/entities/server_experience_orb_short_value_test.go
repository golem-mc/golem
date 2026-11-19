package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestExperienceOrbValueReadsShortLikeVanilla covers the saved-file load and
// /data's second read with int values outside the short range. Vanilla narrows
// both to a short instead of keeping their full int value.
// vanilla: ExperienceOrb.readAdditionalSaveData
func TestExperienceOrbValueReadsShortLikeVanilla(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:experience_orb"),
		"UUID": servertest.UuidWords(servertest.FixtureUUID(4, 72, 0, 10)),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(5.5), nbt.Double(70), nbt.Double(5.5),
		}},
		"Motion":    nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0), nbt.Double(0), nbt.Double(0)}},
		"NoGravity": nbt.Byte(1),
		"Value":     nbt.Int(65537),
	})

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Keeper"))
	c := connect()
	c.Login("Keeper")
	c.Configure()
	shown := c.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeExperienceOrb: "orb",
	})
	orb := servertest.CapturedOr(t, c, shown, "orb", protocol.EntityTypeExperienceOrb)
	srv.StopTick()
	if entry := servertest.MetaOf(orb.Meta, protocol.MetadataIndexOrbValue); entry == nil {
		t.Fatal("restored orb carries no value")
	} else if entry.Typ != protocol.MetadataVarInt {
		t.Errorf("orb value field type = %d, want %d (a varint)", entry.Typ, protocol.MetadataVarInt)
	} else if entry.VarintVal != 1 {
		t.Errorf("restored orb is worth %d after loading Value:65537, want the narrowed short 1", entry.VarintVal)
	}

	display := servertest.EntityDisplayComponent("minecraft:experience_orb", "entity.minecraft.experience_orb", orb.Uuid)
	c.ChatCommand("data merge entity " + orb.Uuid.String() + " {Value:65538}")
	servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.modified", display)
	for {
		entry := servertest.MetaOf(c.AwaitMetadataFor(orb.EntityID), protocol.MetadataIndexOrbValue)
		if entry == nil {
			continue
		}
		if entry.Typ != protocol.MetadataVarInt {
			t.Fatalf("orb value field type after data merge = %d, want %d (a varint)", entry.Typ, protocol.MetadataVarInt)
		}
		if entry.VarintVal != 2 {
			t.Errorf("orb is worth %d after data merging Value:65538, want the narrowed short 2", entry.VarintVal)
		}
		break
	}
}
