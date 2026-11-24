package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/google/uuid"
)

// mobMetadataPayload builds a mob's Set Entity Metadata: health and every
// field not at the client's default.
func mobMetadataPayload(ent *mobEntity) []byte {
	state := mobMetadataStateOf(ent.mob)
	defaults := &mobMetaDefaults
	if ent.mob.Type == entity.TypeSnowGolem || ent.mob.Type == entity.TypeDolphin {
		kindDefaults := mobMetaDefaults
		if ent.mob.Type == entity.TypeSnowGolem {
			// vanilla: SnowGolem.defineSynchedData defines DATA_PUMPKIN_ID as 16.
			kindDefaults.golemFlags = 0x10
		} else {
			// vanilla: Dolphin.defineSynchedData defines MOISTNESS_LEVEL as 2400.
			kindDefaults.dolphinMoistness = 2400
		}
		defaults = &kindDefaults
	}
	var w mobMetaWriter
	w.b.PutVarInt(ent.mob.ID)
	mobMetadataTable(&w, ent, &state, defaults)
	w.b.EndMetadata()
	return w.b.Bytes()
}

// mobMetadataDeltaPayload builds a metadata update of the fields changed
// since was.
func mobMetadataDeltaPayload(ent *mobEntity, was mobMetadataState) []byte {
	now := mobMetadataStateOf(ent.mob)
	return mobMetadataDeltaFrom(ent, &now, &was)
}

// mobMetadataDeltaFrom encodes two captured revisions without reading the mob again.
func mobMetadataDeltaFrom(ent *mobEntity, now, was *mobMetadataState) []byte {
	w := mobMetaWriter{delta: true}
	w.b.PutVarInt(ent.mob.ID)
	mobMetadataTable(&w, ent, now, was)
	w.b.EndMetadata()
	return w.b.Bytes()
}

// metadataState shares an immutable revision with every watcher until a field changes.
func (ent *mobEntity) metadataState() *mobMetadataState {
	revision := ent.mob.MetadataRevision()
	if ent.metaSeen != nil && ent.metaRead == revision {
		return ent.metaSeen
	}
	var now mobMetadataState
	if ent.metaSeen == nil {
		now = mobMetadataStateOf(ent.mob)
	} else {
		now = *ent.metaSeen
		captureMobMetadata(ent.mob, &now, ent.mob.MetadataDirty())
		if slices.Equal(now.particles, ent.metaSeen.particles) {
			now.particles = ent.metaSeen.particles
		}
	}
	if ent.metaSeen == nil || mobMetadataTurned(now, *ent.metaSeen) {
		captured := now
		ent.metaSeen = &captured
	}
	ent.metaRead = revision
	ent.mob.ClearMetadataDirty()
	return ent.metaSeen
}

// mobMetadataDeltas reuses encodings for watchers sharing a baseline in one pass.
// A second baseline pays for a map; the usual shared baseline needs none.
type mobMetadataDeltas struct {
	ent                *mobEntity
	now, last          *mobMetadataState
	packets            []outbound
	others             map[*mobMetadataState][]outbound
	attributes, saddle []byte
}

func (d *mobMetadataDeltas) from(old *mobMetadataState) []outbound {
	if d.last == old {
		return d.packets
	}
	if packets, ok := d.others[old]; ok {
		return packets
	}
	if d.last != nil {
		if d.others == nil {
			d.others = make(map[*mobMetadataState][]outbound)
		}
		d.others[d.last] = d.packets
	}
	d.last, d.packets = old, nil
	now := d.now
	if !mobMetadataTurned(*now, *old) {
		return nil
	}
	if now.saddled != old.saddled {
		if d.saddle == nil {
			d.saddle = saddlePayload(d.ent.mob)
		}
		d.packets = append(d.packets, outbound{id: protocol.IDPlayEntityEquipment, data: d.saddle})
	}
	d.packets = append(d.packets, outbound{id: protocol.IDPlayEntityMetadata, data: mobMetadataDeltaFrom(d.ent, now, old)})
	speed := now.drinking != old.drinking || now.creepy != old.creepy || now.striderCold != old.striderCold
	baby := now.baby != old.baby && entity.Family(d.ent.mob.Type) == entity.TypeZombie
	if speed || baby {
		if d.attributes == nil {
			d.attributes = mobAttributesPayload(d.ent)
		}
		p := outbound{id: protocol.IDPlayUpdateAttributes, data: d.attributes}
		if speed {
			d.packets = append(d.packets, p)
		}
		if baby {
			d.packets = append(d.packets, p)
		}
	}
	return d.packets
}

// updateShownMetadata records an immediate broadcast without mutating shared revisions.
func (ent *mobEntity) updateShownMetadata(update func(*mobMetadataState)) {
	var before, after *mobMetadataState
	for _, tr := range ent.tracked.all() {
		if tr.own.meta != before {
			before = tr.own.meta
			now := *before
			update(&now)
			after = &now
		}
		tr.own.meta = after
	}
}

//go:generate go run ../../tools/gen mobmeta

// mobMetaDescriptor owns the projection and ordered encoders for a capture group.
// The generator parses the Go bodies in package server; no code runs during generation.
// Empty fields/capture denotes an opaque spawn-only helper; saddled has no encoder.
type mobMetaDescriptor struct {
	fields, capture string
	sources         mobMetaSources
	entries         []mobMetaEntry
}

type mobMetaEntry struct {
	spawn, delta int // -1 omits an entry; these positions are not protocol indices.
	encode       string
}

// mobMetaSource names storage in entity.Mob; key and condition run in package entity.
// A zero-argument method selector declares an external revision source.
type mobMetaSource struct{ path, typ, key, condition string }
type mobMetaSources []mobMetaSource

func mobMeta(fields string, sources mobMetaSources, capture string, entries ...mobMetaEntry) mobMetaDescriptor {
	return mobMetaDescriptor{fields, capture, sources, entries}
}

// mobMetadataSchema is shared by pairing, tick deltas and /data.
// Explicit positions preserve main's differing goat, bee and blaze order and spawn helpers.
var mobMetadataSchema = []mobMetaDescriptor{
	mobMeta("health", mobMetaSources{{"m.Health", "float64", "float32(m.Health)", ""}}, `s.health = float32(m.Health)`, mobMetaEntry{200, 200, `w.floatTold(protocol.MetadataIndexLivingHealth, now.health, was.health, true)`}),
	mobMeta("air", mobMetaSources{{"m.AirSupply", "int", "", ""}}, `s.air = m.AirSupply`, mobMetaEntry{140, 140, `w.varInt(protocol.MetadataIndexAir, protocol.MetadataVarInt, now.air, was.air)`}),
	mobMeta("frozen", mobMetaSources{{"m.TicksFrozen", "int", "", ""}}, `s.frozen = m.TicksFrozen`, mobMetaEntry{180, 180, `w.varInt(protocol.MetadataIndexTicksFrozen, protocol.MetadataVarInt, now.frozen, was.frozen)`}),
	mobMeta("arrows", mobMetaSources{{"m.ArrowCount", "int", "", ""}}, `s.arrows = m.ArrowCount`, mobMetaEntry{190, 190, `w.varInt(protocol.MetadataIndexArrowCount, protocol.MetadataVarInt, now.arrows, was.arrows)`}),
	mobMeta("baby", mobMetaSources{{"m.BabyAge", "int", "m.IsBaby()", ""}}, `s.baby = m.IsBaby()`, mobMetaEntry{550, 550, `if t == entity.TypePiglin { w.boolean(protocol.MetadataIndexPiglinBaby, now.baby, was.baby) } else { w.boolean(protocol.MetadataIndexBaby, now.baby, was.baby) }`}),
	mobMeta("immune", mobMetaSources{{"m.barter.Immune", "bool", "", ""}}, `s.immune = m.PiglinImmune()`, mobMetaEntry{540, 540, `if t == entity.TypePiglin || t == entity.TypePiglinBrute {
		// vanilla: AbstractPiglin.DATA_IMMUNE_TO_ZOMBIFICATION
		w.boolean(protocol.MetadataIndexPiglinImmune, now.immune, was.immune)
	}`}),
	mobMeta("dancing", mobMetaSources{{"m.barter.dance", "int", "m.Dancing()", ""}}, `s.dancing = m.Dancing()`, mobMetaEntry{552, 552, `if t == entity.TypePiglin { w.boolean(protocol.MetadataIndexPiglinDancing, now.dancing, was.dancing) }`}),
	mobMeta("particles particleRevision ambient baseFlags", mobMetaSources{{"m.LivingEffects.EffectRevision()", "uint64", "", ""}, {"m.Glowing", "bool", "", ""}, {"m.FireTicks", "int", "m.OnFire()", ""}, {"m.VisualFire", "bool", "", ""}, {"m.Swimming", "bool", "", ""}, {"m.Sprinting", "bool", "", ""}}, `shown := mobEffectShown(m)
s.particles, s.ambient = shown.particles, shown.ambient
s.particleRevision = effectParticleRevision(m, shown.particles)
if shown.invisible {
		s.baseFlags |= protocol.EntityFlagInvisible
	}
if shown.glowing || m.Glowing {
		s.baseFlags |= protocol.EntityFlagGlowing
	}
if m.OnFire() || m.VisualFire {
		s.baseFlags |= protocol.EntityFlagOnFire
	}
if m.Swimming {
		s.baseFlags |= protocol.EntityFlagSwimming
	}
if m.Sprinting {
		s.baseFlags |= protocol.EntityFlagSprinting
	}`, mobMetaEntry{130, 130, `w.byte(protocol.MetadataIndexEntityFlags, now.baseFlags, was.baseFlags)`}, mobMetaEntry{220, 220, `shown := bodyShown{particles: now.particles, ambient: now.ambient}
if w.delta {
		putBodyMetadata(&w.b, shown, now.particleRevision != was.particleRevision || !slices.Equal(now.particles, was.particles),
			now.ambient != was.ambient, false)
	} else {
		putBodyMetadata(&w.b, shown, len(now.particles) > 0, now.ambient, false)
	}`}),
	mobMeta("mobFlags", mobMetaSources{{"m.Aggressive", "bool", "", ""}, {"m.NoAI", "bool", "", ""}, {"m.LeftHanded", "bool", "", ""}}, `if m.Aggressive {
		s.mobFlags |= protocol.EntityFlagAggressive
	}
if m.NoAI {
		s.mobFlags |= protocol.EntityFlagNoAI
	}
if m.LeftHanded {
		s.mobFlags |= protocol.EntityFlagLeftHanded
	}`, mobMetaEntry{230, 230, `w.byte(protocol.MetadataIndexMobFlags, now.mobFlags, was.mobFlags)`}),
	mobMeta("pose sleepingPos", mobMetaSources{{"m.Crouching", "bool", "", ""}, {"m.warden.pose", "int", "", ""}, {"m.breeze.pose", "int32", "", ""}, {"m.frog.phase", "int", "m.FrogUsingTongue()", ""}, {"m.Goat.jump.posed", "bool", "m.LongJumping()", ""}, {"m.frog.jump.posed", "bool", "m.LongJumping()", ""}, {"m.frog.croaking", "bool", "", ""}, {"m.camel.lastPose", "int64", "m.CamelSitting()", ""}, {"m.Sleeping", "bool", "", ""}, {"m.vb.sleepPos.X", "int32", "", ""}, {"m.vb.sleepPos.Y", "int32", "", ""}, {"m.vb.sleepPos.Z", "int32", "", ""}}, `if m.Crouching {
		s.pose = protocol.PoseSneaking
	}
if m.Type == entity.TypeWarden {
		s.pose = int32(m.WardenPose())
	}
if m.Type == entity.TypeBreeze {
		s.pose = m.BreezePose()
	}
if m.FrogUsingTongue() {
		s.pose = protocol.PoseUsingTongue
	}
if m.LongJumping() {
		s.pose = protocol.PoseLongJumping
	}
if m.FrogCroaking() {
		s.pose = protocol.PoseCroaking
	}
if entity.IsCamel(m.Type) && m.CamelSitting() { s.pose = protocol.PoseSitting }
if x, y, z, ok := m.SleepingPos(); ok {
		s.pose = protocol.PoseSleeping
		s.sleepingPos = sleepSpot{true, x, y, z}
	}`, mobMetaEntry{160, 160, `w.varInt(protocol.MetadataIndexPose, protocol.MetadataPose, int(now.pose), int(was.pose))
w.sleepSpot(protocol.MetadataIndexSleepingPos, now.sleepingPos, was.sleepingPos)`}),
	mobMeta("wardenAnger", mobMetaSources{{"m.warden.clientAnger", "int", "", ""}}, `if m.Type == entity.TypeWarden { s.wardenAnger = m.WardenClientAnger() }`, mobMetaEntry{300, 300, `if t == entity.TypeWarden {
		w.varInt(protocol.MetadataIndexWardenAnger, protocol.MetadataVarInt, now.wardenAnger, was.wardenAnger)
	}`}),
	mobMeta("frogTongue", mobMetaSources{{"m.frog.phase", "int", "m.FrogTongueTarget()", ""}, {"m.frog.target", "int32", "", ""}}, `if id := m.FrogTongueTarget(); id != 0 {
		s.frogTongue = int(id) + 1
	}`, mobMetaEntry{60, 60, `if t == entity.TypeFrog {
		w.varInt(protocol.MetadataIndexFrogTongueTarget, protocol.MetadataOptionalVarInt, now.frogTongue, was.frogTongue)
	}`}),
	mobMeta("camelDash camelPoseTick", mobMetaSources{{"m.camel.dashing", "bool", "", ""}, {"m.camel.lastPose", "int64", "", ""}}, `if entity.IsCamel(m.Type) { s.camelDash, s.camelPoseTick = m.CamelDashing(), m.CamelPoseTick() }`, mobMetaEntry{731, 731, `if entity.IsCamel(t) { w.boolean(protocol.MetadataIndexCamelDash, now.camelDash, was.camelDash); w.varLong(protocol.MetadataIndexCamelLastPoseChangeTick, now.camelPoseTick, was.camelPoseTick) }`}),
	mobMeta("climbing", mobMetaSources{{"m.Climbing", "bool", "", ""}, {"m.Resting", "bool", "", ""}}, `switch entity.Family(m.Type) { case entity.TypeSpider: s.climbing = m.Climbing; case entity.TypeBat: s.climbing = m.Resting }`, mobMetaEntry{330, 330, `w.flag(protocol.MetadataIndexSpiderClimbing, protocol.EntityFlagClimbing, now.climbing, was.climbing)`}),
	mobMeta("swell ignited powered", mobMetaSources{{"m.SwellDir", "int", "", ""}, {"m.Ignited", "bool", "", ""}, {"m.Powered", "bool", "", ""}}, `if entity.Family(m.Type) == entity.TypeCreeper { s.swell = int32(m.SwellDir); s.ignited = m.Ignited; s.powered = m.Powered }`, mobMetaEntry{360, 360, `if t == entity.TypeCreeper {
		w.varInt(protocol.MetadataIndexCreeperSwell, protocol.MetadataVarInt, int(now.swell), int(was.swell))
		w.boolean(protocol.MetadataIndexCreeperPowered, now.powered, was.powered)
		// vanilla: Creeper.DATA_IS_IGNITED
		w.boolean(protocol.MetadataIndexCreeperIgnited, now.ignited, was.ignited)
	}`}),
	mobMeta("handState", mobMetaSources{{"m.UsingItem", "bool", "", ""}, {"m.UseOffHand", "bool", "", ""}}, `switch entity.Family(m.Type) { case entity.TypeZombie, entity.TypeSkeleton, entity.TypePillager, entity.TypePiglin, entity.TypeWanderingTrader, entity.TypeIllusioner: s.handState = mobHandState(m) }`, mobMetaEntry{380, 380, `w.byte(protocol.MetadataIndexHandState, now.handState, was.handState)`}),
	mobMeta("charging", mobMetaSources{{"m.ChargingCrossbow", "bool", "", ""}}, `switch entity.Family(m.Type) { case entity.TypePillager, entity.TypePiglin: s.charging = m.ChargingCrossbow }`, mobMetaEntry{400, 400, `if t == entity.TypePillager {
		w.boolean(protocol.MetadataIndexPillagerCharging, now.charging, was.charging)
	}`}, mobMetaEntry{551, 551, `if t == entity.TypePiglin { w.boolean(protocol.MetadataIndexPiglinCharging, now.charging, was.charging) }`}),
	mobMeta("spell", mobMetaSources{{"m.spell.current", "uint8", "", ""}}, `switch entity.Family(m.Type) { case entity.TypeEvoker, entity.TypeIllusioner: s.spell = m.Spell() }`, mobMetaEntry{410, 410, `w.byte(protocol.MetadataIndexSpellcasting, now.spell, was.spell)`}),
	mobMeta("vexCharging", mobMetaSources{{"m.vex.charging", "bool", "", ""}}, `if entity.Family(m.Type) == entity.TypeVex { s.vexCharging = m.VexCharging() }`, mobMetaEntry{420, 420, `w.flag(protocol.MetadataIndexVexFlags, 1, now.vexCharging, was.vexCharging)`}),
	mobMeta("sheepColour", mobMetaSources{{"m.Colour", "string", "", ""}, {"m.Sheared", "bool", "", ""}}, `if m.Type == entity.TypeSheep {
		s.sheepColour = sheepColourByte(m)
	}`, mobMetaEntry{240, 240, `w.byte(protocol.MetadataIndexSheepColour, now.sheepColour, was.sheepColour)`}),
	mobMeta("golemFlags", mobMetaSources{{"m.PlayerCreated", "bool", "", ""}, {"m.Sheared", "bool", "", ""}}, `if m.Type == entity.TypeIronGolem && m.PlayerCreated {
		// A built iron golem sets bit 0x01; a summoned one keeps the default 0.
		s.golemFlags = 0x01
	}
if m.Type == entity.TypeSnowGolem && !m.Sheared {
		// vanilla: SnowGolem.PUMPKIN_FLAG
		s.golemFlags = 0x10
	}`, mobMetaEntry{350, 350, `w.byte(protocol.MetadataIndexGolemFlags, now.golemFlags, was.golemFlags)`}),
	mobMeta("mooshroom", mobMetaSources{{"m.Mushroom", "string", "", ""}}, `if m.Type == entity.TypeMooshroom {
		// vanilla: MushroomCow.DATA_TYPE, Variant.RED's 0 left as the client's
		// default.
		s.mooshroom = mooshroomVariantID(m)
	}`, mobMetaEntry{320, 320, `w.varInt(protocol.MetadataIndexMooshroomVariant, protocol.MetadataVarInt, int(now.mooshroom), int(was.mooshroom))`}),
	mobMeta("celebrating", mobMetaSources{{"m.Celebrating", "bool", "", ""}}, `s.celebrating = m.Celebrating`, mobMetaEntry{390, 390, `w.boolean(protocol.MetadataIndexRaiderCelebrating, now.celebrating, was.celebrating)`}),
	mobMeta("rabbit", mobMetaSources{{"m.RabbitType", "int32", "", ""}}, `s.rabbit = m.RabbitType`, mobMetaEntry{260, 260, `w.varInt(protocol.MetadataIndexRabbitType, protocol.MetadataVarInt, int(now.rabbit), int(was.rabbit))`}),
	mobMeta("trusting", mobMetaSources{{"m.Trusting", "bool", "", ""}}, `s.trusting = m.Trusting`, mobMetaEntry{270, 270, `w.boolean(protocol.MetadataIndexOcelotTrusting, now.trusting, was.trusting)`}),
	mobMeta("armadilloState", mobMetaSources{{"m.armadilloState", "int", "", ""}}, `s.armadilloState = m.ArmadilloState()`, mobMetaEntry{10, 10, `if t == entity.TypeArmadillo {
		w.varInt(protocol.MetadataIndexArmadilloState, protocol.MetadataArmadilloState, now.armadilloState, was.armadilloState)
	}`}),
	mobMeta("allayDancing allayCooling", mobMetaSources{{"m.allay.dancing", "bool", "", ""}, {"m.allay.cooldown", "int64", "m.AllayCanDuplicate()", ""}}, `s.allayDancing, s.allayCooling = m.AllayDancing(), !m.AllayCanDuplicate()`, mobMetaEntry{20, 20, `if t == entity.TypeAllay {
		w.boolean(protocol.MetadataIndexAllayDancing, now.allayDancing, was.allayDancing)
		// DATA_CAN_DUPLICATE defaults to true, so the state holds its inverse.
		if now.allayCooling != was.allayCooling {
			w.putBool(protocol.MetadataIndexAllayCanDuplicate, !now.allayCooling)
		}
	}`}),
	mobMeta("copperWeather copperState", mobMetaSources{{"m.copper.weather", "int", "", ""}, {"m.copper.state", "int", "", ""}}, `s.copperWeather, s.copperState = m.CopperWeather(), m.CopperState()`, mobMetaEntry{30, 30, `if t == entity.TypeCopperGolem {
		w.varInt(protocol.MetadataIndexCopperGolemWeather, protocol.MetadataWeatheringCopperState, now.copperWeather, was.copperWeather)
		w.varInt(protocol.MetadataIndexCopperGolemState, protocol.MetadataCopperGolemState, now.copperState, was.copperState)
	}`}),
	mobMeta("snifferState snifferDropSeedAt", mobMetaSources{{"m.sniffer.state", "SnifferState", "", ""}, {"m.sniffer.dropSeedAt", "int", "", ""}}, `s.snifferState, s.snifferDropSeedAt = int(m.SnifferStateOf()), m.SnifferDropSeedAt()`, mobMetaEntry{40, 40, `if t == entity.TypeSniffer {
		w.varInt(protocol.MetadataIndexSnifferState, protocol.MetadataSnifferState, now.snifferState, was.snifferState)
		w.varInt(protocol.MetadataIndexSnifferDropSeedAt, protocol.MetadataVarInt, now.snifferDropSeedAt, was.snifferDropSeedAt)
	}`}),
	mobMeta("pandaUnhappy pandaMain pandaHidden pandaSneeze pandaEat pandaFlags", mobMetaSources{{"m.panda.unhappy", "int", "", ""}, {"m.panda.main", "PandaGene", "", ""}, {"m.panda.hidden", "PandaGene", "", ""}, {"m.panda.sneeze", "int", "", ""}, {"m.panda.eat", "int", "", ""}, {"m.panda.flags", "uint8", "", ""}}, `if m.Type == entity.TypePanda {
		s.pandaUnhappy, s.pandaMain, s.pandaHidden = m.PandaUnhappy(), uint8(m.PandaMainGene()), uint8(m.PandaHiddenGene())
		s.pandaSneeze, s.pandaEat, s.pandaFlags = m.PandaSneezeCounter(), m.PandaEatCounter(), m.PandaFlags()
	}`, mobMetaEntry{50, 50, `if t == entity.TypePanda {
		w.varInt(protocol.MetadataIndexPandaUnhappyCounter, protocol.MetadataVarInt, now.pandaUnhappy, was.pandaUnhappy)
		w.byte(protocol.MetadataIndexPandaMainGene, now.pandaMain, was.pandaMain)
		w.byte(protocol.MetadataIndexPandaHiddenGene, now.pandaHidden, was.pandaHidden)
		w.varInt(protocol.MetadataIndexPandaSneezeCounter, protocol.MetadataVarInt, now.pandaSneeze, was.pandaSneeze)
		w.varInt(protocol.MetadataIndexPandaEatCounter, protocol.MetadataVarInt, now.pandaEat, was.pandaEat)
		w.byte(protocol.MetadataIndexPandaFlags, now.pandaFlags, was.pandaFlags)
	}`}),
	mobMeta("standing", mobMetaSources{{"m.Standing", "bool", "", ""}}, `s.standing = m.Standing`, mobMetaEntry{280, 280, `w.boolean(protocol.MetadataIndexPolarBearStanding, now.standing, was.standing)`}),
	mobMeta("goatFlags", mobMetaSources{{"m.Goat.Screaming", "bool", "", ""}, {"m.Goat.NoLeftHorn", "bool", "", ""}, {"m.Goat.NoRightHorn", "bool", "", ""}}, `s.goatFlags = goatMetadataFlags(m)`, mobMetaEntry{290, 250, `
		if t == entity.TypeGoat {
			putGoatFlags(&w.b, now.goatFlags, was.goatFlags)
		}
	`}),
	mobMeta("fox foxFlags", mobMetaSources{{"m.Fox.Variant", "int32", "", ""}, {"m.Fox.Sitting", "bool", "", ""}, {"m.Fox.Crouching", "bool", "", ""}, {"m.Fox.Interested", "bool", "", ""}, {"m.Fox.Pouncing", "bool", "", ""}, {"m.Fox.Sleeping", "bool", "", ""}, {"m.Fox.Faceplanted", "bool", "", ""}, {"m.Fox.Defending", "bool", "", ""}}, `if m.Type == entity.TypeFox {
		s.fox = m.Fox.Variant
		s.foxFlags = m.FoxFlags()
	}`, mobMetaEntry{310, 310, `if t == entity.TypeFox {
		// vanilla: Fox.DATA_TYPE_ID, red the default
		w.varInt(protocol.MetadataIndexFoxType, protocol.MetadataVarInt, int(now.fox), int(was.fox))
		w.byte(protocol.MetadataIndexFoxFlags, now.foxFlags, was.foxFlags)
	}`}),
	mobMeta("beeFlags", mobMetaSources{{"m.bee.rolling", "bool", "", ""}, {"m.bee.stung", "bool", "", ""}, {"m.bee.nectar", "bool", "", ""}}, `s.beeFlags = m.BeeFlags()`, mobMetaEntry{721, -1, `w.byte(protocol.MetadataIndexBeeFlags, now.beeFlags, was.beeFlags)`}, mobMetaEntry{-1, 340, `w.byte(protocol.MetadataIndexBeeFlags, now.beeFlags, was.beeFlags)`}),
	mobMeta("chest horseFlags horseVariant llamaStrength", mobMetaSources{{"m.Horse.Chest", "bool", "", ""}, {"m.Horse.Tame", "bool", "", ""}, {"m.Horse.Bred", "bool", "", ""}, {"m.Horse.Eating", "bool", "", ""}, {"m.Horse.Standing", "int", "m.Horse.Standing > 0", ""}, {"m.Horse.mouthTicks", "int", "m.Horse.mouthTicks > 0", ""}, {"m.Horse.Variant", "int32", "", ""}, {"m.Horse.Strength", "int32", "", ""}}, `if entity.IsEquine(m.Type) {
		s.chest = m.Horse.Chest
		s.horseFlags = m.HorseFlags(protocol.HorseFlagTame, protocol.HorseFlagBred, protocol.HorseFlagEating, protocol.HorseFlagStanding, protocol.HorseFlagMouthOpen)
		s.horseVariant = m.Horse.Variant
		s.llamaStrength = m.Horse.Strength
	}`, mobMetaEntry{730, 730, `switch t {
case entity.TypeHorse, entity.TypeDonkey, entity.TypeMule, entity.TypeSkeletonHorse, entity.TypeZombieHorse, entity.TypeLlama, entity.TypeTraderLlama, entity.TypeCamel, entity.TypeCamelHusk:
 w.byte(protocol.MetadataIndexHorseFlags, now.horseFlags, was.horseFlags)
 if entity.IsLlama(t) {
  w.boolean(protocol.MetadataIndexChestedHorseChest, now.chest, was.chest)
  w.varInt(protocol.MetadataIndexLlamaStrength, protocol.MetadataVarInt, int(now.llamaStrength), int(was.llamaStrength))
  w.varInt(protocol.MetadataIndexLlamaVariant, protocol.MetadataVarInt, int(now.horseVariant), int(was.horseVariant))
 } else if t == entity.TypeHorse { w.varInt(protocol.MetadataIndexHorseVariant, protocol.MetadataVarInt, int(now.horseVariant), int(was.horseVariant))
 } else if !entity.IsCamel(t) && !entity.IsUndeadHorse(t) { w.boolean(protocol.MetadataIndexChestedHorseChest, now.chest, was.chest) }
}`}),
	mobMeta("saddled", mobMetaSources{{"m.Horse.Saddle", "protocol.ItemStack", "!m.SaddleSlot().Empty()", ""}, {"m.Antenna", "protocol.ItemStack", "!m.SaddleSlot().Empty()", ""}}, `s.saddled = !m.SaddleSlot().Empty()`),
	mobMeta("creepy staredAt carried", mobMetaSources{{"m.TargetID", "int32", "m.Creepy()", ""}, {"m.enderman.staredAt", "bool", "", ""}, {"m.enderman.carried", "uint16", "", ""}}, `s.creepy, s.staredAt = m.Creepy(), m.StaredAt()
s.carried = m.CarriedBlock()`, mobMetaEntry{370, 370, `if t == entity.TypeEnderman {
		// vanilla: EnderMan.DATA_CARRY_STATE
		w.varInt(protocol.MetadataIndexEndermanCarried, protocol.MetadataOptionalBlockState, int(now.carried), int(was.carried))
		// vanilla: EnderMan.DATA_CREEPY
		w.boolean(protocol.MetadataIndexEndermanScreaming, now.creepy, was.creepy)
		// vanilla: EnderMan.DATA_STARED_AT
		w.boolean(protocol.MetadataIndexEndermanStaring, now.staredAt, was.staredAt)
	}`}),
	mobMeta("drinking", mobMetaSources{{"m.witch.drinking", "bool", "", ""}}, `s.drinking = m.DrinkingPotion()`, mobMetaEntry{430, 430, `w.boolean(protocol.MetadataIndexWitchDrinking, now.drinking, was.drinking)`}),
	mobMeta("angerEnd", mobMetaSources{{"m.GameTime", "int64", "m.AngerEndTime()", "m.Type == TypeWolf || m.Type == TypeBee"}, {"m.anger.ticks", "int", "m.AngerEndTime()", "m.Type == TypeWolf || m.Type == TypeBee"}}, `if m.Type == entity.TypeWolf || m.Type == entity.TypeBee {
		s.angerEnd = m.AngerEndTime()
	}`, mobMetaEntry{722, -1, `if t == entity.TypeBee { w.varLong(protocol.MetadataIndexBeeAngerEnd, now.angerEnd, was.angerEnd) }`}, mobMetaEntry{734, 734, `if t == entity.TypeWolf { w.varLong(protocol.MetadataIndexWolfAngerEnd, now.angerEnd, was.angerEnd) }`}, mobMetaEntry{-1, 341, `if t == entity.TypeBee { w.varLong(protocol.MetadataIndexBeeAngerEnd, now.angerEnd, was.angerEnd) }`}),
	mobMeta("tamableFlags nautilusDash parrotVariant owner collar lying relaxing", mobMetaSources{{"m.Tamable.Tame", "bool", "", ""}, {"m.Tamable.SittingPose", "bool", "", ""}, {"m.nautilus.dashing", "bool", "", ""}, {"m.parrot.variant", "int", "", ""}, {"m.Tamable.Owner", "uuid.UUID", "", ""}, {"m.Collar", "string", "", ""}, {"m.Lying", "bool", "", ""}, {"m.RelaxStateOne", "bool", "", ""}}, `if m.Type == entity.TypeWolf || m.Type == entity.TypeCat || entity.IsNautilus(m.Type) || m.Type == entity.TypeParrot {
		if m.Tamable.Tame {
			s.tamableFlags |= protocol.TamableTame
		}
		if m.Tamable.SittingPose {
			s.tamableFlags |= protocol.TamableSitting
		}
		s.nautilusDash = m.NautilusDashing()
		s.parrotVariant = m.ParrotVariant()
		s.owner = m.Tamable.Owner
		s.collar = dyeOfColour(m.Collar)
		s.lying, s.relaxing = m.Lying, m.RelaxStateOne
	}`, mobMetaEntry{732, 732, `switch t { case entity.TypeWolf, entity.TypeCat, entity.TypeNautilus, entity.TypeZombieNautilus, entity.TypeParrot:
		w.byte(protocol.MetadataIndexTamableFlags, now.tamableFlags, was.tamableFlags)
		w.owner(protocol.MetadataIndexTamableOwner, now.owner, was.owner)
		switch t {
		case entity.TypeCat:
			w.boolean(protocol.MetadataIndexCatLying, now.lying, was.lying)
			w.boolean(protocol.MetadataIndexCatRelaxing, now.relaxing, was.relaxing)
			w.varInt(protocol.MetadataIndexCatCollar, protocol.MetadataVarInt, int(now.collar), int(was.collar))
		case entity.TypeWolf:
			w.varInt(protocol.MetadataIndexWolfCollar, protocol.MetadataVarInt, int(now.collar), int(was.collar))
		}
		if t == entity.TypeParrot {
			w.varInt(protocol.MetadataIndexParrotVariant, protocol.MetadataVarInt, now.parrotVariant, was.parrotVariant)
		}
		if entity.IsNautilus(t) {
			w.boolean(protocol.MetadataIndexNautilusDash, now.nautilusDash, was.nautilusDash)
		}
	}`}),
	mobMeta("begging", mobMetaSources{{"m.Begging", "bool", "", ""}}, `if m.Type == entity.TypeWolf { s.begging = m.Begging }`, mobMetaEntry{733, 733, `if t == entity.TypeWolf { w.boolean(protocol.MetadataIndexWolfBegging, now.begging, was.begging) }`}),
	mobMeta("tropicalVariant", mobMetaSources{{"m.TropicalVariant", "int32", "", ""}}, `s.tropicalVariant = int(m.TropicalVariant)`, mobMetaEntry{660, 660, `if t == entity.TypeTropicalFish {
		w.varInt(protocol.MetadataIndexTropicalFishVariant, protocol.MetadataVarInt, now.tropicalVariant, was.tropicalVariant)
	}`}),
	mobMeta("sheared", mobMetaSources{{"m.Sheared", "bool", "", ""}}, `s.sheared = m.Type == entity.TypeBogged && m.Sheared`, mobMetaEntry{440, 440, `w.boolean(protocol.MetadataIndexBoggedSheared, now.sheared, was.sheared)`}),
	mobMeta("villager", mobMetaSources{{"m.Villager.Type", "string", "", ""}, {"m.Villager.Profession", "string", "", ""}, {"m.Villager.Level", "int", "", ""}, {"m.Villager.Finalized", "bool", "", ""}}, `s.villager = m.Villager`, mobMetaEntry{460, 460, `if t == entity.TypeZombieVillager || t == entity.TypeVillager {
		// A spawn always sends the data: a client rolls its own profession for a
		// zombie villager told none.
		putVillagerMetadata(&w.b, t, now.villager, was.villager)
	}`}),
	mobMeta("unhappy", mobMetaSources{{"m.Unhappy", "int", "", ""}}, `s.unhappy = m.Unhappy`, mobMetaEntry{450, 450, `if t == entity.TypeVillager {
		// vanilla: AbstractVillager.DATA_UNHAPPY_COUNTER
		w.varInt(protocol.MetadataIndexVillagerUnhappy, protocol.MetadataVarInt, now.unhappy, was.unhappy)
	}`}),
	mobMeta("size", mobMetaSources{{"m.Size", "int", "", ""}}, `s.size = m.Size`, mobMetaEntry{510, 510, `w.varIntTold(protocol.MetadataIndexCubeSize, protocol.MetadataVarInt, now.size, was.size, now.size > 1)`}),
	mobMeta("phantomSize", mobMetaSources{{"m.phantom.size", "int", "", ""}}, `s.phantomSize = m.PhantomSize()`, mobMetaEntry{500, 500, `if t == entity.TypePhantom {
		w.varIntTold(protocol.MetadataIndexPhantomSize, protocol.MetadataVarInt, now.phantomSize, was.phantomSize, now.phantomSize > 0)
	}`}),
	mobMeta("fromBucket", mobMetaSources{{"m.FromBucket", "bool", "", ""}}, `s.fromBucket = m.FromBucket`, mobMetaEntry{521, 521, `if t == entity.TypeSulfurCube { w.boolean(protocol.MetadataIndexSulfurCubeFromBucket, now.fromBucket, was.fromBucket) }`}),
	mobMeta("maxFuse", mobMetaSources{{"m.sulfur.maxFuse", "int", "m.SulfurMaxFuse()", ""}}, `s.maxFuse = m.SulfurMaxFuse()`, mobMetaEntry{520, 520, `if t == entity.TypeSulfurCube { w.varIntTold(protocol.MetadataIndexSulfurCubeMaxFuse, protocol.MetadataVarInt, now.maxFuse, was.maxFuse, now.maxFuse != -1) }`}),
	mobMeta("hasEgg layingEgg", mobMetaSources{{"m.HasEgg", "bool", "", ""}, {"m.LayingEgg", "bool", "", ""}}, `s.hasEgg, s.layingEgg = m.HasEgg, m.LayingEgg`, mobMetaEntry{560, 560, `w.boolean(protocol.MetadataIndexTurtleHasEgg, now.hasEgg, was.hasEgg)`}, mobMetaEntry{570, 570, `w.boolean(protocol.MetadataIndexTurtleLayingEgg, now.layingEgg, was.layingEgg)`}),
	mobMeta("ageLocked", mobMetaSources{{"m.AgeLocked", "bool", "", ""}}, `s.ageLocked = m.AgeLocked`, mobMetaEntry{90, 90, `if t == entity.TypeTadpole || entity.Ageable(t) {
		w.boolean(protocol.MetadataIndexAgeLocked, now.ageLocked, was.ageLocked)
	}`}),
	mobMeta("axolotlVariant axolotlPlayingDead axolotlFromBucket", mobMetaSources{{"m.axolotl.variant", "int", "", ""}, {"m.axolotl.playingDead", "bool", "", ""}, {"m.FromBucket", "bool", "", ""}}, `if m.Type == entity.TypeAxolotl {
		s.axolotlVariant = m.AxolotlVariant()
		s.axolotlPlayingDead = m.PlayingDead()
		s.axolotlFromBucket = m.FromBucket
	}`, mobMetaEntry{680, 680, `if t == entity.TypeAxolotl {
		w.varInt(protocol.MetadataIndexAxolotlVariant, protocol.MetadataVarInt, now.axolotlVariant, was.axolotlVariant)
		w.boolean(protocol.MetadataIndexAxolotlPlayingDead, now.axolotlPlayingDead, was.axolotlPlayingDead)
		w.boolean(protocol.MetadataIndexAxolotlFromBucket, now.axolotlFromBucket, was.axolotlFromBucket)
	}`}),
	mobMeta("dolphinGotFish dolphinMoistness", mobMetaSources{{"m.dolphin.gotFish", "bool", "", ""}, {"m.dolphin.moistness", "int", "", ""}}, `s.dolphinGotFish, s.dolphinMoistness = m.DolphinData()`, mobMetaEntry{670, 670, `if t == entity.TypeDolphin {
		w.boolean(protocol.MetadataIndexDolphinGotFish, now.dolphinGotFish, was.dolphinGotFish)
		w.varInt(protocol.MetadataIndexDolphinMoistness, protocol.MetadataVarInt, now.dolphinMoistness, was.dolphinMoistness)
	}`}),
	mobMeta("puffState", mobMetaSources{{"m.puffer.state", "int", "", ""}}, `s.puffState = m.PuffState()`, mobMetaEntry{80, 80, `if t == entity.TypePufferfish {
		w.varInt(protocol.MetadataIndexPufferfishPuff, protocol.MetadataVarInt, now.puffState, was.puffState)
	}`}),
	mobMeta("guardianMoving guardianTarget", mobMetaSources{{"m.guardian.moving", "bool", "", ""}, {"m.guardian.target", "int32", "", ""}}, `s.guardianMoving, s.guardianTarget = m.GuardianMoving(), m.GuardianTarget()`, mobMetaEntry{690, 690, `w.boolean(protocol.MetadataIndexGuardianMoving, now.guardianMoving, was.guardianMoving)`}, mobMetaEntry{700, 700, `w.varInt(protocol.MetadataIndexGuardianTarget, protocol.MetadataVarInt, int(now.guardianTarget), int(was.guardianTarget))`}),
	mobMeta("shulker", mobMetaSources{{"m.shulker.face", "uint8", "", ""}, {"m.shulker.peek", "uint8", "", ""}, {"m.shulker.colorOff", "uint8", "", ""}}, `if m.IsShulker() {
		s.shulker = shulkerMetadata{m.ShulkerFace(), m.ShulkerPeek(), m.ShulkerColor() - 16}
	}`, mobMetaEntry{710, 710, `if t == entity.TypeShulker {
		putShulkerMetadata(&w.b, now.shulker, was.shulker)
	}`}),
	mobMeta("blazeCharged", mobMetaSources{{"m.blaze.charged", "bool", "", ""}}, `s.blazeCharged = m.BlazeCharged()`, mobMetaEntry{590, 630, `
		w.flag(protocol.MetadataIndexBlazeFlags, 1, now.blazeCharged, was.blazeCharged)
	`}),
	mobMeta("dragonPhase", mobMetaSources{{"m.dragon.phase", "int", "m.DragonPhase()", ""}}, `s.dragonPhase = m.DragonPhase()`, mobMetaEntry{70, 70, `if t == entity.TypeEnderDragon {
		w.varInt(protocol.MetadataIndexDragonPhase, protocol.MetadataVarInt, now.dragonPhase, was.dragonPhase)
	}`}),
	mobMeta("ghastCharging", mobMetaSources{{"m.ghast.charging", "bool", "", ""}}, `s.ghastCharging = m.GhastCharging()`, mobMetaEntry{600, 600, `w.boolean(protocol.MetadataIndexGhastCharging, now.ghastCharging, was.ghastCharging)`}),
	mobMeta("witherHeads witherInvul", mobMetaSources{{"m.wither.heads", "[3]int32", "", ""}, {"m.wither.invul", "int", "", ""}}, `s.witherHeads, s.witherInvul = m.WitherHeads(), m.WitherInvul()`, mobMetaEntry{610, 610, `if t == entity.TypeWither {
		// vanilla: WitherBoss.DATA_TARGET_A to C, DATA_ID_INV
		for i, index := range [3]uint8{protocol.MetadataIndexWitherTargetA, protocol.MetadataIndexWitherTargetB, protocol.MetadataIndexWitherTargetC} {
			w.varInt(index, protocol.MetadataVarInt, int(now.witherHeads[i]), int(was.witherHeads[i]))
		}
		w.varInt(protocol.MetadataIndexWitherInvul, protocol.MetadataVarInt, now.witherInvul, was.witherInvul)
	}`}),
	mobMeta("staysStill", mobMetaSources{{"m.stillTimeout", "int", "m.StaysStill()", ""}}, `s.staysStill = m.StaysStill()`, mobMetaEntry{580, 580, `w.boolean(protocol.MetadataIndexHappyGhastStaysStill, now.staysStill, was.staysStill)`}),
	mobMeta("creaking", mobMetaSources{{"m.creaking.frozen", "bool", "", ""}, {"m.creaking.active", "bool", "", ""}, {"m.creaking.tearingDown", "bool", "", ""}, {"m.creaking.bound", "bool", "", ""}, {"m.creaking.home.X", "int32", "", ""}, {"m.creaking.home.Y", "int32", "", ""}, {"m.creaking.home.Z", "int32", "", ""}}, `if m.IsCreaking() {
		s.creaking = m.CreakingView()
	}`, mobMetaEntry{620, 620, `if t == entity.TypeCreaking {
		putCreakingMetadata(&w.b, was.creaking, now.creaking)
	}`}),
	mobMeta("striderCold", mobMetaSources{{"m.strider.cold", "bool", "", ""}}, `s.striderCold = m.StriderCold()`, mobMetaEntry{650, 650, `w.boolean(protocol.MetadataIndexStriderCold, now.striderCold, was.striderCold)`}),
	mobMeta("itemBoost", mobMetaSources{{"m.itemSteering.boostTotal", "int", "", ""}}, `s.itemBoost = m.ItemSteeringBoostTime()`, mobMetaEntry{640, 640, `w.varInt(protocol.MetadataIndexPigBoost, protocol.MetadataVarInt, int(now.itemBoost), int(was.itemBoost))`}),
	mobMeta("curing", mobMetaSources{{"m.cure.active", "bool", "", ""}}, `s.curing = m.Curing()`, mobMetaEntry{470, 470, `w.boolean(protocol.MetadataIndexZombieVillagerCuring, now.curing, was.curing)`}),
	mobMeta("drowning", mobMetaSources{{"m.DrownedConverting", "bool", "", ""}}, `s.drowning = m.DrownedConverting`, mobMetaEntry{480, 480, `w.boolean(protocol.MetadataIndexZombieDrowning, now.drowning, was.drowning)`}),
	mobMeta("straying", mobMetaSources{{"m.StrayConverting", "bool", "", ""}}, `s.straying = m.StrayConverting`, mobMetaEntry{490, 490, `w.boolean(protocol.MetadataIndexSkeletonStraying, now.straying, was.straying)`}),
	mobMeta("dark", mobMetaSources{{"m.DarkTicks", "int", "", ""}}, `if m.Type == entity.TypeGlowSquid {
		s.dark = m.DarkTicks
	}`, mobMetaEntry{530, 530, `w.varIntTold(protocol.MetadataIndexGlowSquidDark, protocol.MetadataVarInt, now.dark, was.dark, now.dark > 0)`}),
	mobMeta("", nil, ``, mobMetaEntry{150, -1, `
		putMobNameMetadata(&w.b, ent)
		w.spawnFlag(protocol.MetadataIndexSilent, m.Silent)
	`}),
	mobMeta("noGravity", mobMetaSources{{"m.NoGravity", "bool", "", ""}}, `s.noGravity = m.NoGravity`, mobMetaEntry{151, 151, `w.boolean(protocol.MetadataIndexNoGravity, now.noGravity, was.noGravity)`}),
	mobMeta("", nil, ``, mobMetaEntry{720, -1, `w.spawnFlag(protocol.MetadataIndexFishFromBucket, m.IsFish() && m.FromBucket)`}),
	mobMeta("", nil, ``, mobMetaEntry{723, -1, `if t == entity.TypeSalmon && m.SalmonSize != entity.SalmonMedium { w.putVarInt(protocol.MetadataIndexSalmonSize, protocol.MetadataVarInt, m.SalmonSize) }; putMobAnimalMetadata(&w.b, m)`}),
}

// defaultCollarDye is the dye ID of the collar a wolf or cat starts with.
var defaultCollarDye = dyeOfColour(entity.WolfDefaultCollar)

// mobMetaDefaults is what a client assumes of an untold mob, compared by a
// spawn walk; unset fields default to zero.
var mobMetaDefaults = mobMetadataState{
	// vanilla: EnderDragon.DATA_PHASE defaults to HOVERING.
	dragonPhase: 10,
	air:         rules.MaxAirSupply,
	pose:        protocol.PoseStanding,
	// 1 while the fuse runs, the idle -1 the client's default.
	swell: -1,
	// A level no data has, so a spawn tells every villager's data.
	villager: entity.VillagerData{Level: -1},
	collar:   defaultCollarDye,
	// vanilla: Wolf.DATA_ANGER_END_TIME, -1 for none
	angerEnd: -1,
}

// mobMetaWriter is a spawn or delta walk of mobMetadataTable. Field methods
// compare inline and write out of line, so a walk costs one comparison per
// field.
type mobMetaWriter struct {
	b     protocol.Buffer
	delta bool
}

// byte is a Byte field.
func (w *mobMetaWriter) byte(index, now, was uint8) {
	if now != was {
		w.putByte(index, now)
	}
}

// mobHandState is the hand-state byte of a mob: the use and its off-hand bit.
func mobHandState(m *entity.Mob) uint8 {
	var state uint8
	if m.UsingItem {
		state = protocol.EntityFlagHandActive
	}
	if m.UseOffHand {
		state |= protocol.EntityFlagActiveHandOff
	}
	return state
}

// flag is a boolean sent as a Byte field: bit when true, 0 when false.
func (w *mobMetaWriter) flag(index, bit uint8, now, was bool) {
	if now != was {
		w.putFlag(index, bit, now)
	}
}

// boolean is a Boolean field.
func (w *mobMetaWriter) boolean(index uint8, now, was bool) {
	if now != was {
		w.putBool(index, now)
	}
}

// spawnFlag is a Boolean a spawn alone tells, while true.
func (w *mobMetaWriter) spawnFlag(index uint8, v bool) {
	if v {
		w.putBool(index, true)
	}
}

// varInt is a field sent as a VarInt (VarInt, Pose, Optional Block State).
func (w *mobMetaWriter) varInt(index uint8, serializer int32, now, was int) {
	if now != was {
		w.putVarInt(index, serializer, now)
	}
}

// varIntTold is varInt where a spawn sends the field when told, not
// whenever it is not the default.
func (w *mobMetaWriter) varIntTold(index uint8, serializer int32, now, was int, told bool) {
	if w.delta && now != was || !w.delta && told {
		w.putVarInt(index, serializer, now)
	}
}

// floatTold is a Float field a spawn tells where told says.
func (w *mobMetaWriter) floatTold(index uint8, now, was float32, told bool) {
	if w.delta && now != was || !w.delta && told {
		w.putFloat(index, now)
	}
}

// sleepSpot is the Optional BlockPos of the bed a body lies in.
func (w *mobMetaWriter) sleepSpot(index uint8, now, was sleepSpot) {
	if now != was {
		w.b.PutMetadata(index, protocol.MetadataOptionalPosition)
		w.b.PutBool(now.set)
		if now.set {
			w.b.PutPosition(now.x, now.y, now.z)
		}
	}
}

// varLong is a VarLong field.
func (w *mobMetaWriter) varLong(index uint8, now, was int64) {
	if now != was {
		w.putVarLong(index, now)
	}
}

// owner is an Optional Living Entity Reference field, the zero UUID none.
func (w *mobMetaWriter) owner(index uint8, now, was uuid.UUID) {
	if now != was {
		w.putOwner(index, now)
	}
}

// The put methods write one entry; they stay out of line so the field
// methods above inline.

//go:noinline
func (w *mobMetaWriter) putByte(index, v uint8) {
	w.b.PutMetadata(index, protocol.MetadataByte)
	w.b.PutU8(v)
}

//go:noinline
func (w *mobMetaWriter) putFlag(index, bit uint8, v bool) {
	w.b.PutMetadata(index, protocol.MetadataByte)
	if v {
		w.b.PutU8(bit)
	} else {
		w.b.PutU8(0)
	}
}

//go:noinline
func (w *mobMetaWriter) putBool(index uint8, v bool) {
	w.b.PutMetadata(index, protocol.MetadataBoolean)
	w.b.PutBool(v)
}

//go:noinline
func (w *mobMetaWriter) putVarInt(index uint8, serializer int32, v int) {
	w.b.PutMetadata(index, serializer)
	w.b.PutVarInt(int32(v))
}

//go:noinline
func (w *mobMetaWriter) putFloat(index uint8, v float32) {
	w.b.PutMetadata(index, protocol.MetadataFloat)
	w.b.PutF32(v)
}

//go:noinline
func (w *mobMetaWriter) putVarLong(index uint8, v int64) {
	w.b.PutMetadata(index, protocol.MetadataVarLong)
	w.b.PutVarLong(v)
}

//go:noinline
func (w *mobMetaWriter) putOwner(index uint8, owner uuid.UUID) {
	w.b.PutMetadata(index, protocol.MetadataOptionalLivingEntityReference)
	w.b.PutBool(owner != uuid.Nil)
	if owner != uuid.Nil {
		w.b.PutUUID(owner)
	}
}

// mobMetadataFields is the comparable part of the metadata state, compared
// whole by mobMetadataTurned so a new field cannot be missed.
type mobMetadataFields struct {
	armadilloState int
	copperWeather  int
	copperState    int
	// snifferState and snifferDropSeedAt are Sniffer.DATA_STATE and DATA_DROP_SEED_AT_TICK.
	snifferState, snifferDropSeedAt int
	pandaUnhappy                    int
	pandaMain                       uint8
	pandaHidden                     uint8
	pandaSneeze, pandaEat           int
	pandaFlags                      uint8

	frogTongue  int
	health      float32
	air         int
	frozen      int
	arrows      int
	mobFlags    uint8
	baseFlags   uint8
	noGravity   bool
	dragonPhase int
	wardenAnger int
	pose        int32
	sheepColour uint8
	golemFlags  uint8
	beeFlags    uint8
	mooshroom   int32
	rabbit      int32
	trusting    bool
	standing    bool
	goatFlags   uint8
	fox         int32
	foxFlags    uint8
	climbing    bool
	swell       int32
	ignited     bool
	powered     bool
	creepy      bool
	staredAt    bool
	carried     uint16
	handState   uint8
	charging    bool
	celebrating bool
	spell       uint8
	vexCharging bool
	drinking    bool
	baby        bool
	immune      bool
	dancing     bool
	sheared     bool
	villager    entity.VillagerData
	fromBucket  bool
	maxFuse     int
	size        int
	phantomSize int
	curing      bool
	drowning    bool
	straying    bool
	dark        int
	unhappy     int
	hasEgg      bool
	layingEgg   bool
	// guardianMoving and guardianTarget are Guardian's two fields.
	ageLocked                             bool
	allayDancing, allayCooling            bool
	tropicalVariant                       int
	dolphinGotFish                        bool
	dolphinMoistness                      int
	axolotlVariant                        int
	parrotVariant                         int
	axolotlPlayingDead, axolotlFromBucket bool
	guardianMoving                        bool
	puffState                             int
	// striderCold is Strider.DATA_SUFFOCATING.
	striderCold    bool
	itemBoost      int32
	guardianTarget int32
	shulker        shulkerMetadata
	blazeCharged   bool
	ghastCharging  bool
	witherHeads    [3]int32
	witherInvul    int
	// staysStill is HappyGhast.STAYS_STILL, which keeps a client from steering.
	staysStill bool
	creaking   entity.CreakingView
	// horseFlags and horseVariant are AbstractHorse's flag byte and Horse's
	// variant; saddled (saddle slot filled) is told by an equipment packet.
	horseFlags   uint8
	horseVariant int32
	// llamaStrength is Llama.DATA_STRENGTH_ID.
	llamaStrength int32
	saddled       bool
	chest         bool
	// tamableFlags and owner are TamableAnimal's flag byte and owner
	// reference; the zero UUID is no owner.
	tamableFlags uint8
	nautilusDash bool
	// camelDash and camelPoseTick are Camel.DASH and LAST_POSE_CHANGE_TICK.
	camelDash     bool
	camelPoseTick int64
	owner         uuid.UUID
	// collar is a wolf's or cat's collar dye ID.
	collar uint8
	// lying and relaxing are a cat's bed poses.
	lying, relaxing bool
	// begging is a wolf's interested flag.
	begging bool
	// angerEnd is a wolf's or bee's DATA_ANGER_END_TIME: the game tick its
	// anger ends, -1 for none.
	angerEnd int64
	// particleRevision is the mob's effect revision when particles carries a
	// colored entity_effect option.
	particleRevision uint64
	// ambient is the mob's effect particle frequency flag.
	ambient bool
	// sleepingPos is a sleeping villager's bed head.
	sleepingPos sleepSpot
}

// sleepSpot is an Optional BlockPos: the bed a sleeping body lies in.
type sleepSpot struct {
	set     bool
	x, y, z int32
}

// mobMetadataState is an immutable revision shared by watchers told the same
// metadata. particles is outside the comparable fields because a slice makes
// a struct incomparable with ==.
type mobMetadataState struct {
	mobMetadataFields
	// particles is the body's effect particles, the baselines mobEffectTells
	// carries.
	particles []effect.Particle
}

// mobMetadataTurned reports whether two states differ, slices compared by
// content.
func mobMetadataTurned(now, was mobMetadataState) bool {
	return now.mobMetadataFields != was.mobMetadataFields ||
		!slices.Equal(now.particles, was.particles)
}

// putVillagerMetadata writes changed Villager Data and finalized flag; the
// villager and zombie villager register them at different indices.
func putVillagerMetadata(b *protocol.Buffer, t entity.MobType, now, was entity.VillagerData) {
	dataIndex, finalizedIndex := uint8(protocol.MetadataIndexVillagerData), uint8(protocol.MetadataIndexVillagerDataFinalized)
	if t == entity.TypeVillager {
		dataIndex, finalizedIndex = protocol.MetadataIndexVillagerVillagerData, protocol.MetadataIndexVillagerFinalized
	}
	if now.Type != was.Type || now.Profession != was.Profession || now.Level != was.Level {
		if typ, profession, ok := now.IDs(); ok {
			b.PutMetadata(dataIndex, protocol.MetadataVillagerData)
			b.PutVarInt(typ)
			b.PutVarInt(profession)
			b.PutVarInt(int32(now.Level))
		}
	}
	if now.Finalized != was.Finalized {
		b.PutMetadata(finalizedIndex, protocol.MetadataBoolean)
		b.PutBool(now.Finalized)
	}
}
