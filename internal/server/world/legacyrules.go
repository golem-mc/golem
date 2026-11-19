package world

import (
	"maps"
	"math"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// Before 26.1 game rules lived in level.dat, which vanilla's upgrade extracts
// into game_rules.dat: from 1.21.11 (data version 4658) as game_rules in
// game_rules.dat's form, before that as GameRules with string values under old
// camelCase names.
// vanilla: GameRuleRegistryFix, LevelDatToSavedDataFileFix
const gameRuleRegistryVersion = 4658

// gameRulesFileVersion is the data version 26.1 moved the rules to
// game_rules.dat at; older worlds keep them in level.dat.
const gameRulesFileVersion = 4772

// legacyRule is one rename GameRuleRegistryFix makes: the old name, the rule
// it became, whether a boolean's sense flipped, and an integer's clamp.
type legacyRule struct {
	old      string
	id       string
	inverted bool
	min, max int32
}

func lbool(old, id string) legacyRule { return legacyRule{old: old, id: id} }
func linv(old, id string) legacyRule  { return legacyRule{old: old, id: id, inverted: true} }
func lint(old, id string, min, max int32) legacyRule {
	return legacyRule{old: old, id: id, min: min, max: max}
}

// legacyRules in GameRuleRegistryFix's order; of two old names for one rule the
// later wins. doFireTick and allowFireTicksAwayFromPlayer, which together
// became fire_spread_radius_around_player, are handled apart.
var legacyRules = []legacyRule{
	lbool("allowEnteringNetherUsingPortals", "allow_entering_nether_using_portals"),
	lbool("announceAdvancements", "show_advancement_messages"),
	lbool("blockExplosionDropDecay", "block_explosion_drop_decay"),
	lbool("commandBlockOutput", "command_block_output"),
	lbool("enableCommandBlocks", "command_blocks_work"),
	lbool("commandBlocksEnabled", "command_blocks_work"),
	lint("commandModificationBlockLimit", "max_block_modifications", 1, math.MaxInt32),
	linv("disableElytraMovementCheck", "elytra_movement_check"),
	linv("disablePlayerMovementCheck", "player_movement_check"),
	linv("disableRaids", "raids"),
	lbool("doDaylightCycle", "advance_time"),
	lbool("doEntityDrops", "entity_drops"),
	lbool("doImmediateRespawn", "immediate_respawn"),
	lbool("doInsomnia", "spawn_phantoms"),
	lbool("doLimitedCrafting", "limited_crafting"),
	lbool("doMobLoot", "mob_drops"),
	lbool("doMobSpawning", "spawn_mobs"),
	lbool("doPatrolSpawning", "spawn_patrols"),
	lbool("doTileDrops", "block_drops"),
	lbool("doTraderSpawning", "spawn_wandering_traders"),
	lbool("doVinesSpread", "spread_vines"),
	lbool("doWardenSpawning", "spawn_wardens"),
	lbool("doWeatherCycle", "advance_weather"),
	lbool("drowningDamage", "drowning_damage"),
	lbool("enderPearlsVanishOnDeath", "ender_pearls_vanish_on_death"),
	lbool("fallDamage", "fall_damage"),
	lbool("fireDamage", "fire_damage"),
	lbool("forgiveDeadPlayers", "forgive_dead_players"),
	lbool("freezeDamage", "freeze_damage"),
	lbool("globalSoundEvents", "global_sound_events"),
	lbool("keepInventory", "keep_inventory"),
	lbool("lavaSourceConversion", "lava_source_conversion"),
	lbool("locatorBar", "locator_bar"),
	lbool("logAdminCommands", "log_admin_commands"),
	lint("maxCommandChainLength", "max_command_sequence_length", 0, math.MaxInt32),
	lint("maxCommandForkCount", "max_command_forks", 0, math.MaxInt32),
	lint("maxEntityCramming", "max_entity_cramming", 0, math.MaxInt32),
	lint("minecartMaxSpeed", "max_minecart_speed", math.MinInt32, math.MaxInt32),
	lbool("mobExplosionDropDecay", "mob_explosion_drop_decay"),
	lbool("mobGriefing", "mob_griefing"),
	lbool("naturalRegeneration", "natural_health_regeneration"),
	lint("playersNetherPortalCreativeDelay", "players_nether_portal_creative_delay", 0, math.MaxInt32),
	lint("playersNetherPortalDefaultDelay", "players_nether_portal_default_delay", 0, math.MaxInt32),
	lint("playersSleepingPercentage", "players_sleeping_percentage", 0, math.MaxInt32),
	lbool("projectilesCanBreakBlocks", "projectiles_can_break_blocks"),
	lbool("pvp", "pvp"),
	lint("randomTickSpeed", "random_tick_speed", 0, math.MaxInt32),
	lbool("reducedDebugInfo", "reduced_debug_info"),
	lbool("sendCommandFeedback", "send_command_feedback"),
	lbool("showDeathMessages", "show_death_messages"),
	lint("snowAccumulationHeight", "max_snow_accumulation_height", 0, 8),
	lbool("spawnMonsters", "spawn_monsters"),
	lint("spawnRadius", "respawn_radius", math.MinInt32, math.MaxInt32),
	lbool("spawnerBlocksEnabled", "spawner_blocks_work"),
	lbool("spectatorsGenerateChunks", "spectators_generate_chunks"),
	lbool("tntExplodes", "tnt_explodes"),
	lbool("tntExplosionDropDecay", "tnt_explosion_drop_decay"),
	lbool("universalAnger", "universal_anger"),
	lbool("waterSourceConversion", "water_source_conversion"),
}

// rulesFromLevel reads a pre-26.1 level.dat's rules into values, reporting
// whether level.dat holds rules at all (an empty GameRules counts, as vanilla's
// upgrade prefers it over game_rules.dat).
func rulesFromLevel(values *ruleValues, raw nbt.Compound) bool {
	if rules, ok := nbt.Get[nbt.Compound](raw, "game_rules"); ok {
		rulesFromCompound(values, rules)
		return true
	}
	legacy, ok := nbt.Get[nbt.Compound](raw, "GameRules")
	if !ok {
		return false
	}
	str := func(key string) (string, bool) {
		v, ok := nbt.Get[nbt.String](legacy, key)
		return string(v), ok
	}
	for _, l := range legacyRules {
		text, ok := str(l.old)
		r := ruleByID(l.id)
		if !ok || r == nil {
			continue
		}
		value := values[RuleIndex(l.id)]
		if r.Kind == RuleBool {
			// Java's Boolean.parseBoolean.
			value.b = strings.EqualFold(text, "true") != l.inverted
		} else {
			// Java's Integer.parseInt, which no whitespace passes.
			n, err := strconv.ParseInt(text, 10, 32)
			if err != nil {
				continue
			}
			value.i = max(l.min, min(l.max, int32(n)))
		}
		values[RuleIndex(l.id)] = value
	}
	doFireTick, haveTick := str("doFireTick")
	awayFromPlayer, haveAway := str("allowFireTicksAwayFromPlayer")
	if haveTick || haveAway {
		radius := int32(128)
		switch {
		case haveTick && !strings.EqualFold(doFireTick, "true"):
			radius = 0
		case strings.EqualFold(awayFromPlayer, "true"):
			radius = -1
		}
		values[RuleFireSpread] = ruleValue{i: radius}
	}
	return true
}

// rulesToLevel writes values into a pre-26.1 level.dat in its version's form,
// so vanilla's upgrade carries rules changed here. A rule with no old name is
// added under the first.
func rulesToLevel(values *ruleValues, raw nbt.Compound) {
	version, _ := nbt.Get[nbt.Int](raw, "DataVersion")
	if _, ok := nbt.Get[nbt.Compound](raw, "game_rules"); ok || version >= gameRuleRegistryVersion {
		raw["game_rules"] = rulesToCompound(values)
		return
	}
	legacy, _ := nbt.Get[nbt.Compound](raw, "GameRules")
	updated := maps.Clone(legacy)
	if updated == nil {
		updated = nbt.Compound{}
	}
	written := make(map[string]bool)
	for _, l := range legacyRules {
		if _, ok := legacy[l.old]; !ok {
			continue
		}
		updated[l.old] = legacyValue(values, l)
		written[l.id] = true
	}
	for _, l := range legacyRules {
		r := ruleByID(l.id)
		if written[l.id] || r == nil || r.FeatureGated {
			continue
		}
		updated[l.old] = legacyValue(values, l)
		written[l.id] = true
	}
	radius := values[RuleFireSpread].i
	updated["doFireTick"] = nbt.String(strconv.FormatBool(radius != 0))
	updated["allowFireTicksAwayFromPlayer"] = nbt.String(strconv.FormatBool(radius == -1))
	raw["GameRules"] = updated
}

func legacyValue(values *ruleValues, l legacyRule) nbt.String {
	value := values[RuleIndex(l.id)]
	if ruleByID(l.id).Kind == RuleBool {
		return nbt.String(strconv.FormatBool(value.b != l.inverted))
	}
	return nbt.String(strconv.FormatInt(int64(value.i), 10))
}
