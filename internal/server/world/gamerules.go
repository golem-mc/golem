package world

import (
	"errors"
	"log/slog"
	"math"

	"github.com/golem-mc/golem/nbt"
)

// The game rules: one flat table of booleans and integers under /gamerule's
// names, from the GameRules class's registrations: 59 rules, each with kind and
// default, integers carrying the parser's bounds.
//
// Vanilla keeps them in their own saved-data file,
// world/data/minecraft/game_rules.dat, each rule under its minecraft:-prefixed
// name (boolean as byte, integer as int). It is read on load and written on
// save, so a rule set in either server survives in the other's files.
//
// max_minecart_speed is registered behind the MINECART_IMPROVEMENTS feature
// flag, which no default world enables, so vanilla's file leaves it out. This
// server has no feature flags and matches that: the rule is settable while
// running but not saved.
const gameRulesFile = "data/minecraft/game_rules.dat"

// RuleKind is whether a rule is a boolean or an integer.
type RuleKind uint8

const (
	// RuleBool is a boolean rule, stored on disk as a byte.
	RuleBool RuleKind = iota
	// RuleInt is an integer rule, stored on disk as an int.
	RuleInt
)

// GameRule is one table row: the id (/gamerule's name, stored with a minecraft:
// prefix), kind, default, and for integers the value argument's range.
type GameRule struct {
	ID   string
	Kind RuleKind
	// DefBool and DefInt are the defaults. Only the kind's own is used.
	DefBool bool
	DefInt  int32
	// Min and Max bound an integer rule, the parser's own bounds; MaxInt32 is
	// unbounded.
	Min int32
	Max int32
	// FeatureGated marks the one rule no default world carries to disk.
	FeatureGated bool
}

// boolRule, intRule are the two shorthand constructors of the table below.
func boolRule(id string, def bool) GameRule {
	return GameRule{ID: id, Kind: RuleBool, DefBool: def,
		Min: math.MinInt32, Max: math.MaxInt32}
}

func intRule(id string, def, min int32) GameRule {
	return GameRule{ID: id, Kind: RuleInt, DefInt: def, Min: min, Max: math.MaxInt32}
}

// GameRuleTable lists the rules in GameRules' registration order, which its
// bytecode carries alphabetically; a /gamerule tree's literal order is
// GameRuleTreeOrder.
var GameRuleTable = [ruleCount]GameRule{
	RuleAdvanceTime:                      boolRule("advance_time", true),
	RuleAdvanceWeather:                   boolRule("advance_weather", true),
	RuleAllowEnteringNether:              boolRule("allow_entering_nether_using_portals", true),
	RuleBlockDrops:                       boolRule("block_drops", true),
	RuleBlockExplosionDropDecay:          boolRule("block_explosion_drop_decay", true),
	RuleCommandBlocksWork:                boolRule("command_blocks_work", true),
	RuleCommandBlockOutput:               boolRule("command_block_output", true),
	RuleDrowningDamage:                   boolRule("drowning_damage", true),
	RuleElytraMovementCheck:              boolRule("elytra_movement_check", true),
	RuleEnderPearlsVanishOnDeath:         boolRule("ender_pearls_vanish_on_death", true),
	RuleEntityDrops:                      boolRule("entity_drops", true),
	RuleFallDamage:                       boolRule("fall_damage", true),
	RuleFireDamage:                       boolRule("fire_damage", true),
	RuleFireSpread:                       intRule("fire_spread_radius_around_player", 128, -1),
	RuleForgiveDeadPlayers:               boolRule("forgive_dead_players", true),
	RuleFreezeDamage:                     boolRule("freeze_damage", true),
	RuleGlobalSoundEvents:                boolRule("global_sound_events", true),
	RuleImmediateRespawn:                 boolRule("immediate_respawn", false),
	RuleKeepInventory:                    boolRule("keep_inventory", false),
	RuleLavaSourceConversion:             boolRule("lava_source_conversion", false),
	RuleLimitedCrafting:                  boolRule("limited_crafting", false),
	RuleLocatorBar:                       boolRule("locator_bar", true),
	RuleLogAdminCommands:                 boolRule("log_admin_commands", true),
	RuleMaxBlockMods:                     intRule("max_block_modifications", 32768, 1),
	RuleMaxCommandForks:                  intRule("max_command_forks", 65536, 0),
	RuleMaxCommandSequence:               intRule("max_command_sequence_length", 65536, 0),
	RuleMaxCramming:                      intRule("max_entity_cramming", 24, 0),
	RuleMaxMinecartSpeed:                 {ID: "max_minecart_speed", Kind: RuleInt, DefInt: 8, Min: 1, Max: 1000, FeatureGated: true},
	RuleMaxSnowHeight:                    {ID: "max_snow_accumulation_height", Kind: RuleInt, DefInt: 1, Min: 0, Max: 8},
	RuleMobDrops:                         boolRule("mob_drops", true),
	RuleMobExplosionDropDecay:            boolRule("mob_explosion_drop_decay", true),
	RuleMobGriefing:                      boolRule("mob_griefing", true),
	RuleNaturalRegen:                     boolRule("natural_health_regeneration", true),
	RulePlayerMovementCheck:              boolRule("player_movement_check", true),
	RulePlayersNetherPortalCreativeDelay: intRule("players_nether_portal_creative_delay", 0, 0),
	RulePlayersNetherPortalDefaultDelay:  intRule("players_nether_portal_default_delay", 80, 0),
	RulePlayersSleeping:                  intRule("players_sleeping_percentage", 100, 0),
	RuleProjectilesCanBreakBlocks:        boolRule("projectiles_can_break_blocks", true),
	RulePvp:                              boolRule("pvp", true),
	RuleRaids:                            boolRule("raids", true),
	RuleRandomTickSpeed:                  intRule("random_tick_speed", 3, 0),
	RuleReducedDebug:                     boolRule("reduced_debug_info", false),
	RuleRespawnRadius:                    intRule("respawn_radius", 10, 0),
	RuleSendFeedback:                     boolRule("send_command_feedback", true),
	RuleShowAdvancementMsgs:              boolRule("show_advancement_messages", true),
	RuleShowDeathMsg:                     boolRule("show_death_messages", true),
	RuleSpawnerBlocks:                    boolRule("spawner_blocks_work", true),
	RuleSpawnMobs:                        boolRule("spawn_mobs", true),
	RuleSpawnMonsters:                    boolRule("spawn_monsters", true),
	RuleSpawnPatrols:                     boolRule("spawn_patrols", true),
	RuleSpawnPhantoms:                    boolRule("spawn_phantoms", true),
	RuleSpawnTraders:                     boolRule("spawn_wandering_traders", true),
	RuleSpawnWardens:                     boolRule("spawn_wardens", true),
	RuleSpectatorsGenerateChunks:         boolRule("spectators_generate_chunks", true),
	RuleSpreadVines:                      boolRule("spread_vines", true),
	RuleTNTExplodes:                      boolRule("tnt_explodes", true),
	RuleTNTExplosionDropDecay:            boolRule("tnt_explosion_drop_decay", false),
	RuleUniversalAnger:                   boolRule("universal_anger", false),
	RuleWaterSourceConversion:            boolRule("water_source_conversion", true),
}

// GameRuleTreeOrder is the literal order of a /gamerule tree in vanilla's
// Commands packet, checked against a reference server, which differs from
// registration order. The feature-gated rule is absent, as a vanilla build
// omits it.
var GameRuleTreeOrder = []string{
	"log_admin_commands",
	"spread_vines",
	"advance_weather",
	"show_death_messages",
	"forgive_dead_players",
	"natural_health_regeneration",
	"universal_anger",
	"projectiles_can_break_blocks",
	"immediate_respawn",
	"block_explosion_drop_decay",
	"tnt_explodes",
	"lava_source_conversion",
	"allow_entering_nether_using_portals",
	"ender_pearls_vanish_on_death",
	"spawn_monsters",
	"spawner_blocks_work",
	"elytra_movement_check",
	"limited_crafting",
	"players_nether_portal_creative_delay",
	"respawn_radius",
	"players_nether_portal_default_delay",
	"fall_damage",
	"spawn_phantoms",
	"spectators_generate_chunks",
	"mob_drops",
	"locator_bar",
	"reduced_debug_info",
	"raids",
	"mob_explosion_drop_decay",
	"entity_drops",
	"spawn_mobs",
	"pvp",
	"water_source_conversion",
	"spawn_wardens",
	"players_sleeping_percentage",
	"send_command_feedback",
	"show_advancement_messages",
	"tnt_explosion_drop_decay",
	"command_blocks_work",
	"command_block_output",
	"drowning_damage",
	"advance_time",
	"keep_inventory",
	"max_block_modifications",
	"freeze_damage",
	"fire_spread_radius_around_player",
	"random_tick_speed",
	"spawn_wandering_traders",
	"fire_damage",
	"block_drops",
	"spawn_patrols",
	"player_movement_check",
	"global_sound_events",
	"max_entity_cramming",
	"max_command_forks",
	"max_snow_accumulation_height",
	"max_command_sequence_length",
	"mob_griefing",
}

// GameRuleIndex identifies a row in GameRuleTable.
type GameRuleIndex uint8

// Game rule indices follow registration order.
// vanilla: GameRules, GameRuleMap
const (
	RuleAdvanceTime GameRuleIndex = iota
	RuleAdvanceWeather
	RuleAllowEnteringNether
	RuleBlockDrops
	RuleBlockExplosionDropDecay
	RuleCommandBlocksWork
	RuleCommandBlockOutput
	RuleDrowningDamage
	RuleElytraMovementCheck
	RuleEnderPearlsVanishOnDeath
	RuleEntityDrops
	RuleFallDamage
	RuleFireDamage
	RuleFireSpread
	RuleForgiveDeadPlayers
	RuleFreezeDamage
	RuleGlobalSoundEvents
	RuleImmediateRespawn
	RuleKeepInventory
	RuleLavaSourceConversion
	RuleLimitedCrafting
	RuleLocatorBar
	RuleLogAdminCommands
	RuleMaxBlockMods
	RuleMaxCommandForks
	RuleMaxCommandSequence
	RuleMaxCramming
	RuleMaxMinecartSpeed
	RuleMaxSnowHeight
	RuleMobDrops
	RuleMobExplosionDropDecay
	RuleMobGriefing
	RuleNaturalRegen
	RulePlayerMovementCheck
	RulePlayersNetherPortalCreativeDelay
	RulePlayersNetherPortalDefaultDelay
	RulePlayersSleeping
	RuleProjectilesCanBreakBlocks
	RulePvp
	RuleRaids
	RuleRandomTickSpeed
	RuleReducedDebug
	RuleRespawnRadius
	RuleSendFeedback
	RuleShowAdvancementMsgs
	RuleShowDeathMsg
	RuleSpawnerBlocks
	RuleSpawnMobs
	RuleSpawnMonsters
	RuleSpawnPatrols
	RuleSpawnPhantoms
	RuleSpawnTraders
	RuleSpawnWardens
	RuleSpectatorsGenerateChunks
	RuleSpreadVines
	RuleTNTExplodes
	RuleTNTExplosionDropDecay
	RuleUniversalAnger
	RuleWaterSourceConversion
	ruleCount
	// RuleUnknown is the result of looking up an unregistered name.
	RuleUnknown GameRuleIndex = 255
)

// RuleIndex resolves a command or saved rule name, returning RuleUnknown if absent.
func RuleIndex(id string) GameRuleIndex {
	if i, ok := ruleIndices[id]; ok {
		return i
	}
	return RuleUnknown
}

var ruleIndices = func() map[string]GameRuleIndex {
	m := make(map[string]GameRuleIndex, len(GameRuleTable))
	for i, r := range GameRuleTable {
		m[r.ID] = GameRuleIndex(i)
	}
	return m
}()

// String returns the rule's command name.
func (id GameRuleIndex) String() string {
	if id >= ruleCount {
		return ""
	}
	return GameRuleTable[id].ID
}

// ruleValue is a rule's current boolean or integer.
type ruleValue struct {
	b bool
	i int32
}

type ruleValues [ruleCount]ruleValue

// ruleTable is every rule at its default, indexed by GameRuleIndex.
func ruleTable() *ruleValues {
	m := new(ruleValues)
	for i, r := range GameRuleTable {
		m[i] = ruleValue{b: r.DefBool, i: r.DefInt}
	}
	return m
}

// ruleByID finds the row for id, nil if none (an unknown literal, which a
// client built from this table's tree cannot send).
func ruleByID(id string) *GameRule {
	i := RuleIndex(id)
	if i == RuleUnknown {
		return nil
	}
	return &GameRuleTable[i]
}

// loadGameRules reads dir's game rule file, defaulting when absent; a world
// still keeping rules in level.dat reads them from there.
func loadGameRules(dir string, level *LevelData) *ruleValues {
	values := ruleTable()
	if level.keepsRulesInLevel() && rulesFromLevel(values, level.raw) {
		// Vanilla's upgrade extracts these over any game_rules.dat.
		return values
	}
	stored, err := readSavedData(dir, gameRulesFile)
	if err != nil {
		if !errors.Is(err, errNoSavedData) {
			slog.Warn("could not read game_rules.dat, using defaults", "error", err)
		}
		return values
	}
	rulesFromCompound(values, stored)
	return values
}

// rulesFromCompound reads rules from game_rules.dat's data compound, or a
// 1.21.11+ level.dat's game_rules.
func rulesFromCompound(values *ruleValues, stored nbt.Compound) {
	for i, r := range GameRuleTable {
		if r.FeatureGated {
			// Neither vanilla's file nor this server's carries the
			// feature-flagged rule, so the default stands.
			continue
		}
		key := "minecraft:" + r.ID
		if r.Kind == RuleBool {
			if v, ok := nbt.Get[nbt.Byte](stored, key); ok {
				values[i] = ruleValue{b: v != 0, i: values[i].i}
			}
		} else {
			if v, ok := nbt.Get[nbt.Int](stored, key); ok {
				values[i] = ruleValue{b: values[i].b, i: int32(v)}
			}
		}
	}
}

// saveGameRules writes the rule file, encoded under the time lock like every
// rule access; the write itself is outside it, so a tick never waits on a sync.
func (w *World) saveGameRules() error {
	w.timeMu.Lock()
	if w.rules == nil {
		w.timeMu.Unlock()
		return nil
	}
	data := rulesToCompound(w.rules)
	w.timeMu.Unlock()
	if w.ownRules {
		return writeSavedData(w.storage, gameRulesFile, data)
	}
	return writeSavedData(w.dir, gameRulesFile, data)
}

// rulesWorld is the world holding w's rules: itself for a plugin world with its
// own table, else the overworld every other dimension shares.
func (w *World) rulesWorld() *World {
	if w.ownRules {
		return w
	}
	return w.timeWorld()
}

// UseOwnRules gives a world without a level.dat its own rule table, read from
// its data/ folder or else copied from base on first creation, and saved there
// by Save. A vanilla dimension keeps sharing the overworld's table.
func (w *World) UseOwnRules(base *World) {
	var values *ruleValues
	if stored, err := readSavedData(w.storage, gameRulesFile); err == nil {
		values = ruleTable()
		rulesFromCompound(values, stored)
	} else {
		if !errors.Is(err, errNoSavedData) {
			slog.Warn("could not read game_rules.dat, copying the overworld's", "error", err)
		}
		base.timeMu.Lock()
		copied := *base.rules
		base.timeMu.Unlock()
		values = &copied
	}
	w.timeMu.Lock()
	w.rules, w.ownRules = values, true
	w.refreshFeedbackRules()
	w.refreshSpawnEnemies()
	w.timeMu.Unlock()
}

// writeGameRules writes dir's rule file as vanilla reads it: a gzipped unnamed
// root holding the data compound, each rule under its minecraft: name, boolean
// as byte, integer as int.
func writeGameRules(dir string, values *ruleValues) error {
	data := rulesToCompound(values)
	return writeSavedData(dir, gameRulesFile, data)
}

// rulesToCompound is the compound game_rules.dat keeps the rules in.
func rulesToCompound(values *ruleValues) nbt.Compound {
	data := nbt.Compound{}
	for i, r := range GameRuleTable {
		if r.FeatureGated {
			continue
		}
		value := values[i]
		key := "minecraft:" + r.ID
		if r.Kind == RuleBool {
			data[key] = nbt.Byte(boolByte(value.b))
		} else {
			data[key] = nbt.Int(value.i)
		}
	}
	return data
}

// BoolRule reads a boolean rule, false for an unknown index. Only the overworld
// holds a table (the server has one set), so other worlds read defaults; every
// reader asks the overworld.
func (w *World) BoolRule(id GameRuleIndex) bool {
	w = w.rulesWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	return w.ruleBool(id)
}

// ruleBool is BoolRule with the time lock already held, for ticks that hold it
// for the clock they advance.
func (w *World) ruleBool(id GameRuleIndex) bool {
	if id >= ruleCount {
		return false
	}
	if w.rules == nil {
		return GameRuleTable[id].DefBool
	}
	return w.rules[id].b
}

const (
	feedbackOffSend uint32 = 1 << iota
	feedbackOffCommandBlock
	feedbackOffLogAdmin
)

// refreshFeedbackRules recomputes feedbackOff from the table, with timeMu held
// or the world not yet shared.
func (w *World) refreshFeedbackRules() {
	var off uint32
	if !w.ruleBool(RuleSendFeedback) {
		off |= feedbackOffSend
	}
	if !w.ruleBool(RuleCommandBlockOutput) {
		off |= feedbackOffCommandBlock
	}
	if !w.ruleBool(RuleLogAdminCommands) {
		off |= feedbackOffLogAdmin
	}
	w.feedbackOff.Store(off)
}

// FeedbackRules reads send_command_feedback, command_block_output and
// log_admin_commands without the time lock.
func (w *World) FeedbackRules() (send, commandBlockOutput, logAdmin bool) {
	off := w.feedbackOff.Load()
	return off&feedbackOffSend == 0, off&feedbackOffCommandBlock == 0, off&feedbackOffLogAdmin == 0
}

// SpawnEnemies reports the latched natural and custom monster spawning gate.
func (w *World) SpawnEnemies() bool { return w.spawnEnemies.Load() }

// refreshSpawnEnemies runs with timeMu held or before the world is shared.
// vanilla: MinecraftServer.updateMobSpawningFlags, ServerLevel.isSpawningMonsters
func (w *World) refreshSpawnEnemies() {
	enemies := w.ruleOf(RuleSpawnMobs) && w.ruleOf(RuleSpawnMonsters)
	w.spawnEnemies.Store(enemies)
	for _, dim := range w.spawnWorlds {
		if !dim.ownRules {
			dim.spawnEnemies.Store(enemies)
		}
	}
}

// IntRule reads an integer rule, zero for an unknown index.
func (w *World) IntRule(id GameRuleIndex) int32 {
	if id >= ruleCount {
		return 0
	}
	w = w.rulesWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if w.rules == nil {
		return GameRuleTable[id].DefInt
	}
	return w.rules[id].i
}

// SetBoolRule sets a boolean rule for /gamerule without clamping (the parser
// already refused bad values); saved by the next save.
func (w *World) SetBoolRule(id GameRuleIndex, value bool) {
	w = w.rulesWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if w.rules == nil || id >= ruleCount {
		return
	}
	w.rules[id] = ruleValue{b: value, i: w.rules[id].i}
	w.refreshFeedbackRules()
	if id == RuleSpawnMonsters {
		w.refreshSpawnEnemies()
	}
}

// SetIntRule sets an integer rule for /gamerule, clamped to its row's range as
// the parser clamps.
func (w *World) SetIntRule(id GameRuleIndex, value int32) {
	w = w.rulesWorld()
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if w.rules == nil || id >= ruleCount {
		return
	}
	r := &GameRuleTable[id]
	value = max(r.Min, min(r.Max, value))
	w.rules[id] = ruleValue{b: w.rules[id].b, i: value}
}

// RuleByID returns the whole row for id, for callers needing more than bounds
// (the /gamerule tree builder needs the kind).
func RuleByID(id string) (GameRule, bool) {
	r := ruleByID(id)
	if r == nil {
		return GameRule{}, false
	}
	return *r, true
}

// RuleBounds reports whether id is a rule, with its kind and range, which
// /gamerule checks its argument against.
func RuleBounds(id string) (kind RuleKind, min, max int32, ok bool) {
	r := ruleByID(id)
	if r == nil {
		return 0, 0, 0, false
	}
	return r.Kind, r.Min, r.Max, true
}

// TNTExplodes reports the tnt_explodes rule.
func (w *World) TNTExplodes() bool { return w.BoolRule(RuleTNTExplodes) }
