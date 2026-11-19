package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// benchMob is a mob of the type as NewMob stands it up.
func benchMob(t entity.MobType) *mobEntity {
	return &mobEntity{mob: entity.NewMob(t, 7, 0.5, 64, 0.5, 11)}
}

// BenchmarkMobMetadataDelta builds the metadata delta a tick sends for one
// mob whose state turned since the watcher was told: a hurt zombie, the
// commonest case, and a sheep and a wolf whose own fields turned.
func BenchmarkMobMetadataDelta(b *testing.B) {
	for _, c := range []struct {
		name string
		typ  entity.MobType
		turn func(m *entity.Mob)
	}{
		{"zombie-hurt", entity.TypeZombie, func(m *entity.Mob) { m.Health-- }},
		{"sheep-sheared", entity.TypeSheep, func(m *entity.Mob) { m.Sheared = true }},
		{"wolf-sitting", entity.TypeWolf, func(m *entity.Mob) {
			m.Tamable.Tame, m.Tamable.SittingPose = true, true
			m.Aggressive = true
		}},
	} {
		b.Run(c.name, func(b *testing.B) {
			ent := benchMob(c.typ)
			was := mobMetadataStateOf(ent.mob)
			c.turn(ent.mob)
			b.ReportAllocs()
			for b.Loop() {
				mobMetadataDeltaPayload(ent, was)
			}
		})
	}
}

// BenchmarkMobMetadataSpawn builds spawn metadata for a fresh zombie, a dressed
// wolf, and both default and non-default snow-golem pumpkin and dolphin moistness states.
func BenchmarkMobMetadataSpawn(b *testing.B) {
	wolf := benchMob(entity.TypeWolf)
	wolf.name = protocol.TextTag(protocol.Text{Text: "Bob"})
	wolf.mob.FireTicks, wolf.mob.BabyAge, wolf.mob.Collar = 100, -24000, "blue"
	wolf.mob.Tamable.Tame, wolf.mob.Tamable.SittingPose = true, true
	wolf.mob.Tamable.Owner = uuid.MustParse("0b6e8f8e-1f6f-4b84-8a0c-3d6b1f0c9e21")
	pumpkinless := benchMob(entity.TypeSnowGolem)
	pumpkinless.mob.Sheared = true
	dryDolphin := benchMob(entity.TypeDolphin)
	dryDolphin.mob.LoadFrom(nbt.Compound{"Moistness": nbt.Int(0)}, nil, nil)
	for _, c := range []struct {
		name string
		ent  *mobEntity
	}{
		{"zombie-fresh", benchMob(entity.TypeZombie)},
		{"wolf-dressed", wolf},
		{"snow-golem-pumpkin", benchMob(entity.TypeSnowGolem)},
		{"snow-golem-pumpkinless", pumpkinless},
		{"dolphin-moistness-2400", benchMob(entity.TypeDolphin)},
		{"dolphin-moistness-0", dryDolphin},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				mobMetadataPayload(c.ent)
			}
		})
	}
}
