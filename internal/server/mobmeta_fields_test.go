package server

import (
	"reflect"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestMobMetadataEveryTrackedFieldEmitsADelta(t *testing.T) {
	cases := []struct {
		name string
		turn func(*mobMetadataState)
	}{
		{"armadilloState", func(s *mobMetadataState) { s.armadilloState = 1 }},
		{"copperWeather", func(s *mobMetadataState) { s.copperWeather = 1 }},
		{"copperState", func(s *mobMetadataState) { s.copperState = 1 }},
		{"snifferState", func(s *mobMetadataState) { s.snifferState = 1 }},
		{"snifferDropSeedAt", func(s *mobMetadataState) { s.snifferDropSeedAt = 1 }},
		{"pandaUnhappy", func(s *mobMetadataState) { s.pandaUnhappy = 1 }},
		{"pandaMain", func(s *mobMetadataState) { s.pandaMain = 1 }},
		{"pandaHidden", func(s *mobMetadataState) { s.pandaHidden = 1 }},
		{"pandaSneeze", func(s *mobMetadataState) { s.pandaSneeze = 1 }},
		{"pandaEat", func(s *mobMetadataState) { s.pandaEat = 1 }},
		{"pandaFlags", func(s *mobMetadataState) { s.pandaFlags = 1 }},
		{"frogTongue", func(s *mobMetadataState) { s.frogTongue = 1 }},
		{"health", func(s *mobMetadataState) { s.health = 1 }},
		{"air", func(s *mobMetadataState) { s.air = 1 }},
		{"frozen", func(s *mobMetadataState) { s.frozen = 1 }},
		{"arrows", func(s *mobMetadataState) { s.arrows = 1 }},
		{"mobFlags", func(s *mobMetadataState) { s.mobFlags = 1 }},
		{"baseFlags", func(s *mobMetadataState) { s.baseFlags = 1 }},
		{"noGravity", func(s *mobMetadataState) { s.noGravity = true }},
		{"dragonPhase", func(s *mobMetadataState) { s.dragonPhase = 1 }},
		{"wardenAnger", func(s *mobMetadataState) { s.wardenAnger = 1 }},
		{"pose", func(s *mobMetadataState) { s.pose = 1 }},
		{"sheepColour", func(s *mobMetadataState) { s.sheepColour = 1 }},
		{"golemFlags", func(s *mobMetadataState) { s.golemFlags = 1 }},
		{"beeFlags", func(s *mobMetadataState) { s.beeFlags = 1 }},
		{"mooshroom", func(s *mobMetadataState) { s.mooshroom = 1 }},
		{"rabbit", func(s *mobMetadataState) { s.rabbit = 1 }},
		{"trusting", func(s *mobMetadataState) { s.trusting = true }},
		{"standing", func(s *mobMetadataState) { s.standing = true }},
		{"goatFlags", func(s *mobMetadataState) { s.goatFlags = 1 }},
		{"fox", func(s *mobMetadataState) { s.fox = 1 }},
		{"foxFlags", func(s *mobMetadataState) { s.foxFlags = 1 }},
		{"climbing", func(s *mobMetadataState) { s.climbing = true }},
		{"swell", func(s *mobMetadataState) { s.swell = 1 }},
		{"ignited", func(s *mobMetadataState) { s.ignited = true }},
		{"powered", func(s *mobMetadataState) { s.powered = true }},
		{"creepy", func(s *mobMetadataState) { s.creepy = true }},
		{"staredAt", func(s *mobMetadataState) { s.staredAt = true }},
		{"carried", func(s *mobMetadataState) { s.carried = 1 }},
		{"handState", func(s *mobMetadataState) { s.handState = 1 }},
		{"charging", func(s *mobMetadataState) { s.charging = true }},
		{"celebrating", func(s *mobMetadataState) { s.celebrating = true }},
		{"spell", func(s *mobMetadataState) { s.spell = 1 }},
		{"vexCharging", func(s *mobMetadataState) { s.vexCharging = true }},
		{"drinking", func(s *mobMetadataState) { s.drinking = true }},
		{"baby", func(s *mobMetadataState) { s.baby = true }},
		{"immune", func(s *mobMetadataState) { s.immune = true }},
		{"dancing", func(s *mobMetadataState) { s.dancing = true }},
		{"sheared", func(s *mobMetadataState) { s.sheared = true }},
		{"villager.Type", func(s *mobMetadataState) { s.villager.Type = "minecraft:desert" }},
		{"villager.Profession", func(s *mobMetadataState) { s.villager.Profession = "minecraft:farmer" }},
		{"villager.Level", func(s *mobMetadataState) { s.villager.Level = 2 }},
		{"villager.Finalized", func(s *mobMetadataState) { s.villager.Finalized = true }},
		{"fromBucket", func(s *mobMetadataState) { s.fromBucket = true }},
		{"maxFuse", func(s *mobMetadataState) { s.maxFuse = 1 }},
		{"size", func(s *mobMetadataState) { s.size = 1 }},
		{"phantomSize", func(s *mobMetadataState) { s.phantomSize = 1 }},
		{"curing", func(s *mobMetadataState) { s.curing = true }},
		{"drowning", func(s *mobMetadataState) { s.drowning = true }},
		{"straying", func(s *mobMetadataState) { s.straying = true }},
		{"dark", func(s *mobMetadataState) { s.dark = 1 }},
		{"unhappy", func(s *mobMetadataState) { s.unhappy = 1 }},
		{"hasEgg", func(s *mobMetadataState) { s.hasEgg = true }},
		{"layingEgg", func(s *mobMetadataState) { s.layingEgg = true }},
		{"ageLocked", func(s *mobMetadataState) { s.ageLocked = true }},
		{"allayDancing", func(s *mobMetadataState) { s.allayDancing = true }},
		{"allayCooling", func(s *mobMetadataState) { s.allayCooling = true }},
		{"tropicalVariant", func(s *mobMetadataState) { s.tropicalVariant = 1 }},
		{"dolphinGotFish", func(s *mobMetadataState) { s.dolphinGotFish = true }},
		{"dolphinMoistness", func(s *mobMetadataState) { s.dolphinMoistness = 1 }},
		{"axolotlVariant", func(s *mobMetadataState) { s.axolotlVariant = 1 }},
		{"parrotVariant", func(s *mobMetadataState) { s.parrotVariant = 1 }},
		{"axolotlPlayingDead", func(s *mobMetadataState) { s.axolotlPlayingDead = true }},
		{"axolotlFromBucket", func(s *mobMetadataState) { s.axolotlFromBucket = true }},
		{"guardianMoving", func(s *mobMetadataState) { s.guardianMoving = true }},
		{"puffState", func(s *mobMetadataState) { s.puffState = 1 }},
		{"striderCold", func(s *mobMetadataState) { s.striderCold = true }},
		{"itemBoost", func(s *mobMetadataState) { s.itemBoost = 1 }},
		{"guardianTarget", func(s *mobMetadataState) { s.guardianTarget = 1 }},
		{"shulker.0", func(s *mobMetadataState) { s.shulker[0] = 2 }},
		{"shulker.1", func(s *mobMetadataState) { s.shulker[1] = 1 }},
		{"shulker.2", func(s *mobMetadataState) { s.shulker[2] = 255 }},
		{"blazeCharged", func(s *mobMetadataState) { s.blazeCharged = true }},
		{"ghastCharging", func(s *mobMetadataState) { s.ghastCharging = true }},
		{"witherHeads.0", func(s *mobMetadataState) { s.witherHeads[0] = 1 }},
		{"witherHeads.1", func(s *mobMetadataState) { s.witherHeads[1] = 1 }},
		{"witherHeads.2", func(s *mobMetadataState) { s.witherHeads[2] = 1 }},
		{"witherInvul", func(s *mobMetadataState) { s.witherInvul = 1 }},
		{"staysStill", func(s *mobMetadataState) { s.staysStill = true }},
		{"creaking.Frozen", func(s *mobMetadataState) { s.creaking.Frozen = true }},
		{"creaking.Active", func(s *mobMetadataState) { s.creaking.Active = true }},
		{"creaking.TearingDown", func(s *mobMetadataState) { s.creaking.TearingDown = true }},
		{"creaking.Bound", func(s *mobMetadataState) { s.creaking.Bound = true }},
		{"creaking.Home", func(s *mobMetadataState) { s.creaking.Bound = true; s.creaking.Home[0] = 1 }},
		{"horseFlags", func(s *mobMetadataState) { s.horseFlags = 1 }},
		{"horseVariant", func(s *mobMetadataState) { s.horseVariant = 1 }},
		{"llamaStrength", func(s *mobMetadataState) { s.llamaStrength = 1 }},
		{"chest", func(s *mobMetadataState) { s.chest = true }},
		{"tamableFlags", func(s *mobMetadataState) { s.tamableFlags = 1 }},
		{"nautilusDash", func(s *mobMetadataState) { s.nautilusDash = true }},
		{"camelDash", func(s *mobMetadataState) { s.camelDash = true }},
		{"camelPoseTick", func(s *mobMetadataState) { s.camelPoseTick = 1 }},
		{"owner", func(s *mobMetadataState) { s.owner = uuid.UUID{1} }},
		{"collar", func(s *mobMetadataState) { s.collar = 1 }},
		{"lying", func(s *mobMetadataState) { s.lying = true }},
		{"relaxing", func(s *mobMetadataState) { s.relaxing = true }},
		{"begging", func(s *mobMetadataState) { s.begging = true }},
		{"angerEnd", func(s *mobMetadataState) { s.angerEnd = 1 }},
		{"ambient", func(s *mobMetadataState) { s.ambient = true }},
		{"sleepingPos", func(s *mobMetadataState) { s.sleepingPos = sleepSpot{set: true, x: 1} }},
		{"particles", func(s *mobMetadataState) {
			s.particles = []effect.Particle{{Type: "minecraft:entity_effect", Color: 1}}
		}},
	}
	covered := map[string]bool{"saddled": true} // Saddle changes use Set Equipment, covered by the tracking test.
	for _, test := range cases {
		covered[strings.Split(test.name, ".")[0]] = true
		t.Run(test.name, func(t *testing.T) {
			was := mobMetadataState{
				villager:  entity.VillagerData{Type: "minecraft:plains", Profession: "minecraft:none", Level: 1},
				particles: []effect.Particle{{Type: "minecraft:entity_effect"}},
			}
			now := was
			test.turn(&now)
			if !mobMetadataTurned(now, was) {
				t.Fatal("field change did not invalidate metadata")
			}
			emitted := false
			for kind := range entity.MobType(registry.EntityTypeCount) {
				if !entity.KnownMobKind(kind) {
					continue
				}
				ent := &mobEntity{mob: &entity.Mob{ID: 7, Type: kind}}
				forward := mobMetadataDeltaFrom(ent, &now, &was)
				if len(forward) <= 2 {
					continue
				}
				emitted = true
				reverse := mobMetadataDeltaFrom(ent, &was, &now)
				for _, packet := range [][]byte{forward, reverse} {
					r := protocol.NewReader(packet)
					if r.ReadVarInt() != 7 || packet[len(packet)-1] != 0xff {
						t.Fatal("invalid metadata envelope")
					}
					if len(packet) <= 2 {
						t.Fatal("return to previous value omitted the field")
					}
				}
			}
			if !emitted {
				t.Fatal("no mob kind sent the changed field")
			}
		})
	}
	// A new state field must get a packet assertion, including non-comparable particles.
	fields := reflect.TypeFor[mobMetadataFields]()
	for field := range fields.Fields() {
		if !covered[field.Name] {
			t.Errorf("metadata field %s lacks a packet test", field.Name)
		}
	}
	if !covered["particles"] {
		t.Fatal("particle list lacks a packet test")
	}
}
