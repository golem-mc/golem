package registry

// Typed IDs of the built-in entity type tags the server tests, stable across reloads.
var (
	EntityTypeTagIDAcceptsIronGolemGift      = mustEntityTypeTag("minecraft:accepts_iron_golem_gift")
	EntityTypeTagIDAxolotlAlwaysHostiles     = mustEntityTypeTag("minecraft:axolotl_always_hostiles")
	EntityTypeTagIDAxolotlHuntTargets        = mustEntityTypeTag("minecraft:axolotl_hunt_targets")
	EntityTypeTagIDCanBreatheUnderWater      = mustEntityTypeTag("minecraft:can_breathe_under_water")
	EntityTypeTagIDCandidateForIronGolemGift = mustEntityTypeTag("minecraft:candidate_for_iron_golem_gift")
	EntityTypeTagIDCannotBeAgeLocked         = mustEntityTypeTag("minecraft:cannot_be_age_locked")
	EntityTypeTagIDCannotBePushedOntoBoats   = mustEntityTypeTag("minecraft:cannot_be_pushed_onto_boats")
	EntityTypeTagIDDeflectsProjectiles       = mustEntityTypeTag("minecraft:deflects_projectiles")
	EntityTypeTagIDDismountsUnderwater       = mustEntityTypeTag("minecraft:dismounts_underwater")
	EntityTypeTagIDFallDamageImmune          = mustEntityTypeTag("minecraft:fall_damage_immune")
	EntityTypeTagIDFollowableFriendlyMobs    = mustEntityTypeTag("minecraft:followable_friendly_mobs")
	EntityTypeTagIDFreezeHurtsExtraTypes     = mustEntityTypeTag("minecraft:freeze_hurts_extra_types")
	EntityTypeTagIDFreezeImmuneEntityTypes   = mustEntityTypeTag("minecraft:freeze_immune_entity_types")
	EntityTypeTagIDFrogFood                  = mustEntityTypeTag("minecraft:frog_food")
	EntityTypeTagIDIgnoresPoisonAndRegen     = mustEntityTypeTag("minecraft:ignores_poison_and_regen")
	EntityTypeTagIDIllagerFriends            = mustEntityTypeTag("minecraft:illager_friends")
	EntityTypeTagIDImmuneToInfested          = mustEntityTypeTag("minecraft:immune_to_infested")
	EntityTypeTagIDImmuneToOozing            = mustEntityTypeTag("minecraft:immune_to_oozing")
	EntityTypeTagIDImpactProjectiles         = mustEntityTypeTag("minecraft:impact_projectiles")
	EntityTypeTagIDInvertedHealingAndHarm    = mustEntityTypeTag("minecraft:inverted_healing_and_harm")
	EntityTypeTagIDNautilusHostiles          = mustEntityTypeTag("minecraft:nautilus_hostiles")
	EntityTypeTagIDNoAngerFromWindCharge     = mustEntityTypeTag("minecraft:no_anger_from_wind_charge")
	EntityTypeTagIDNonControllingRider       = mustEntityTypeTag("minecraft:non_controlling_rider")
	EntityTypeTagIDNotAffectedByGeysers      = mustEntityTypeTag("minecraft:not_affected_by_geysers")
	EntityTypeTagIDNotScaryForPufferfish     = mustEntityTypeTag("minecraft:not_scary_for_pufferfish")
	EntityTypeTagIDPowderSnowWalkableMobs    = mustEntityTypeTag("minecraft:powder_snow_walkable_mobs")
	EntityTypeTagIDRaiders                   = mustEntityTypeTag("minecraft:raiders")
	EntityTypeTagIDRedirectableProjectile    = mustEntityTypeTag("minecraft:redirectable_projectile")
	EntityTypeTagIDUndead                    = mustEntityTypeTag("minecraft:undead")
	EntityTypeTagIDWitherFriends             = mustEntityTypeTag("minecraft:wither_friends")
)

// mustEntityTypeTag rejects a misspelled built-in tag at initialization.
func mustEntityTypeTag(tag string) EntityTypeTagID {
	id := EntityTypeTagOf(tag)
	if id == noEntityTypeTag {
		panic("registry: unknown entity type tag " + tag)
	}
	return id
}
