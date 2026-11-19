package protocol

const (
	// Version is the protocol version this package speaks; a client
	// reporting another cannot talk to us.
	Version = 777
	// GameVersion is the Minecraft version Version corresponds to.
	GameVersion = "26.3"
)

// The F3 TPS chart's feed. The minecraft:debug_subscription registry is built
// into the client, so its IDs come from the reference server's registry
// report (tools/README.md); the sample type is RemoteDebugSampleType's ordinal.
const (
	DebugSubscriptionTickTime = 0 // minecraft:dedicated_server_tick_time
	// DebugSubscriptionCount is the size of the minecraft:debug_subscription
	// registry, whose built-in entries are the valid request IDs.
	// vanilla: DebugSubscriptions
	DebugSubscriptionCount = 16
	DebugSampleTickTime    = 0
)

// Actions of the Boss Event packet.
const (
	BossEventAdd        = 0
	BossEventRemove     = 1
	BossEventProgress   = 2
	BossEventName       = 3
	BossEventStyle      = 4
	BossEventProperties = 5
)

// The Boss Event packet's colour and overlay ids used by the wither's bar.
// vanilla: BossEvent.BossBarColor.PURPLE, BossBarOverlay.PROGRESS
const (
	BossBarColorPurple     = 5
	BossBarOverlayProgress = 0
)

// Game events sent in the Game Event packet.
const (
	// GameEventNoRespawnBlockAvailable makes the client show
	// block.minecraft.spawn.not_valid: the spawn point's bed is gone or
	// obstructed, or it has nothing to stand on.
	// vanilla: ClientboundGameEventPacket.NO_RESPAWN_BLOCK_AVAILABLE
	GameEventNoRespawnBlockAvailable = 0

	GameEventStartWaitingForChunks = 13

	// GameEventDemoEvent carries a demo hint or screen in its value.
	// vanilla: ClientboundGameEventPacket.DEMO_EVENT
	GameEventDemoEvent = 5

	// GameEventChangeGameMode is event 3, its float the new game mode:
	// 0 survival, 1 creative, 2 adventure, 3 spectator.
	GameEventChangeGameMode = 3

	// GameEventArrowHit is the ding sent to a shooter alone when their arrow
	// hits a player. vanilla: ClientboundGameEventPacket PLAY_ARROW_HIT_SOUND
	GameEventArrowHit = 6

	// The weather events; the two level changes carry 0 to 1 and are what
	// make the client draw rain and darken the light.
	GameEventBeginRaining = 1
	GameEventEndRaining   = 2
	GameEventRainLevel    = 7
	GameEventThunderLevel = 8

	// GameEventWinGame rolls the credits, which the client ends with Client
	// Command PERFORM_RESPAWN. The value is unread; the reference server
	// sends 0. vanilla: ClientboundGameEventPacket.WIN_GAME
	GameEventWinGame = 4

	// GameEventImmediateRespawn says whether the death screen skips itself
	// (1.0) or waits for the button (0.0); Login's respawn-screen field
	// says the same at join.
	GameEventImmediateRespawn = 11

	// GameEventGuardianElderEffect is event 10, the elder guardian's
	// apparition; its float is 1 for a sounding curse and 0 for a silent one.
	// vanilla: ClientboundGameEventPacket.GUARDIAN_ELDER_EFFECT
	GameEventGuardianElderEffect = 10

	// GameEventPufferFishSting is event 9, sent to a player a puffed pufferfish
	// hurt; the value is unread.
	// vanilla: ClientboundGameEventPacket.PUFFER_FISH_STING
	GameEventPufferFishSting = 9

	// GameEventLimitedCrafting: 1.0 keeps the crafting book to unlocked
	// recipes, 0.0 does not.
	GameEventLimitedCrafting = 12
)

// Operations of the Waypoint packet and the forms its trailing coordinate
// takes: the ordinals of the wire enums, a VarInt each.
// vanilla: ClientboundTrackedWaypointPacket.Operation, TrackedWaypoint.Type
const (
	WaypointTrack   = 0
	WaypointUntrack = 1
	WaypointUpdate  = 2

	WaypointTypeEmpty   = 0
	WaypointTypeVec3i   = 1
	WaypointTypeChunk   = 2
	WaypointTypeAzimuth = 3
)

// Statuses of the Entity Event packet, one-off animations on the entity.
// 24 to 28 are a player's op permission levels 0 to 4; 47 to 52 shatter the
// stack in a slot.
const (
	// EntityStatusKineticHit plays the user's kinetic weapon hit sound.
	// vanilla: EntityEvent.KINETIC_HIT
	EntityStatusKineticHit = 2
	// EntityStatusDeath is the death (the body stays), EntityStatusDeathPoof
	// the poof as it leaves at the twentieth tick. vanilla: LivingEntity.tickDeath
	EntityStatusDeath = 3
	// EntityStatusHoglinAttack is the hoglin's and zoglin's attack animation
	// and sound. vanilla: EntityEvent.START_ATTACKING
	EntityStatusHoglinAttack = 4
	// EntityStatusIronGolemAttack starts the ten-tick arm attack animation.
	// vanilla: IronGolem.doHurtTarget, handleEntityEvent
	EntityStatusIronGolemAttack = 4
	// vanilla: IronGolem.offerFlower
	EntityStatusIronGolemOfferFlower = 11
	EntityStatusIronGolemStopFlower  = 34
	EntityStatusDeathPoof            = 60
	// EntityStatusBoatSink and EntityStatusBoatLaunch tell a boat's client
	// the throw at the end of a spell on a bubble column's surface.
	// vanilla: AbstractBoat.handleEntityEvent
	EntityStatusBoatSink   = 71
	EntityStatusBoatLaunch = 72

	// EntityStatusArrowEffectPuff is the burst of twenty particles in a
	// tipped arrow's colour, sent as its effect fades.
	EntityStatusArrowEffectPuff = 0

	// EntityStatusLoveHearts is the heart burst of a body in love or just
	// bred. vanilla: Animal.setInLove, finalizeSpawnChildFromBreeding
	EntityStatusLoveHearts = 18

	// EntityStatusFireworkBurst is a rocket's bang, drawn from its own item.
	// vanilla: FireworkRocketEntity.explode, handleEntityEvent
	EntityStatusFireworkBurst = 17

	// EntityStatusOpLevelBase reports permission level 0, one level of
	// permission per step past it.
	EntityStatusOpLevelBase = 24

	// EntityStatusUseFinished completes a player's use of an item on their
	// client. vanilla: Player.handleEntityEvent status 9
	EntityStatusUseFinished = 9

	// EntityStatusFoxEat is the burst of item particles from the fox's mouth.
	// vanilla: Fox.handleEntityEvent
	EntityStatusFoxEat = 45

	// EntityStatusTeleportParticles is the portal burst a random teleport
	// (a chorus fruit) leaves at the entity.
	EntityStatusTeleportParticles = 46
	// EntityStatusWitchParticles is the burst of witch particles over the
	// witch's head. vanilla: Witch.handleEntityEvent
	EntityStatusWitchParticles = 15
	// EntityStatusVillagerHappy is the burst of happy-villager particles
	// around a villager that just traded. vanilla: Villager.handleEntityEvent
	EntityStatusVillagerHappy = 14
	// EntityStatusVillagerSplash is the sweat during an active raid.
	// vanilla: Villager.handleEntityEvent
	EntityStatusVillagerSplash = 42
	// EntityStatusZombieVillagerCure plays the client's cure sound.
	// vanilla: ZombieVillager.startConverting, handleEntityEvent
	EntityStatusZombieVillagerCure = 16
	// EntityStatusVillagerAngry is the angry-villager particles over a villager
	// a player hurt. vanilla: Villager.handleEntityEvent
	EntityStatusVillagerAngry = 13
	// EntityStatusVillagerHearts is the heart particles of courting villagers
	// and their newborn. vanilla: Villager.handleEntityEvent
	EntityStatusVillagerHearts = 12

	// EntityStatusFishingPull is a retrieved hook's pull on the body its
	// metadata names; a player adds it to their own movement.
	// vanilla: FishingHook.retrieve, FishingHook.handleEntityEvent
	EntityStatusFishingPull = 31

	// EntityStatusRabbitJump starts a rabbit's hop animation.
	// vanilla: Rabbit.handleEntityEvent
	EntityStatusRabbitJump = 1

	// EntityStatusEatGrass is a sheep's lowered head (EatBlockGoal).
	// vanilla: EntityEvent.EAT_GRASS
	EntityStatusEatGrass = 10

	// EntityStatusWolfShake starts a wolf's shake animation;
	// EntityStatusWolfStopShake cancels it.
	// vanilla: Wolf.handleEntityEvent status 8 and 56
	EntityStatusWolfShake     = 8
	EntityStatusWolfStopShake = 56

	// EntityStatusGuardianAttack plays a guardian's attack sound.
	// vanilla: EntityEvent.GUARDIAN_ATTACK_SOUND
	EntityStatusGuardianAttack = 21
	// EntityStatusEvokerFangsBite starts the fangs' rising animation and its
	// sound. vanilla: EvokerFangs.handleEntityEvent
	EntityStatusEvokerFangsBite = 4
	// EntityStatusRavagerAttack starts a ravager's swing, EntityStatusRavagerStun
	// its stun and EntityStatusRavagerRoar the roar's particles.
	// vanilla: Ravager.handleEntityEvent
	EntityStatusRavagerAttack = 4
	EntityStatusRavagerStun   = 39
	EntityStatusRavagerRoar   = 69
	// EntityStatusGoatLowerHead and EntityStatusGoatRaiseHead put a goat's
	// head down for the run-up and back up.
	// vanilla: Goat.handleEntityEvent
	EntityStatusGoatLowerHead = 58
	EntityStatusGoatRaiseHead = 59
	// EntityStatusWardenTendrils starts the warden's ten-tick tendril
	// animation; EntityStatusWardenSonicBoom starts its sonic-boom animation.
	// vanilla: Warden.handleEntityEvent
	EntityStatusWardenTendril   = 61
	EntityStatusWardenSonicBoom = 62

	// EntityStatusSquidTentacles restarts the client's tentacle clock at
	// the wrap of the pump cycle. vanilla: Squid.handleEntityEvent status 19
	EntityStatusSquidTentacles = 19

	// The reduced-debug-info pair the /gamerule reduced_debug_info change
	// broadcasts. vanilla: Player.handleEntityEvent statuses 22 and 23
	EntityStatusReducedDebugOn  = 22
	EntityStatusReducedDebugOff = 23

	// The equipment-break statuses, one per player slot: main hand, off
	// hand, armour head down.
	// vanilla: LivingEntity.entityEventForEquipmentBreak
	EntityStatusBreakMainHand = 47
	EntityStatusBreakOffHand  = 48
	EntityStatusBreakHead     = 49
	EntityStatusBreakChest    = 50
	EntityStatusBreakLegs     = 51
	EntityStatusBreakFeet     = 52
	EntityStatusBreakBody     = 65

	// EntityStatusTotem plays the totem animation and sound of a save from
	// death. vanilla: LivingEntity.checkTotemDeathProtection
	EntityStatusTotem = 35
	// EntityStatusArmorStandShake is a punched stand's wobble.
	// vanilla: ArmorStand.hurtServer
	EntityStatusArmorStandShake = 32
)

// Actions of the serverbound Client Command packet.
const (
	ClientCommandPerformRespawn = 0
	// ClientCommandRequestStats asks for the statistics changed since the
	// last answer, which the statistics screen sends on opening.
	ClientCommandRequestStats = 1
	// ClientCommandRequestGameRuleValues asks for every rule's value, which
	// the server answers for a gamemaster and above.
	ClientCommandRequestGameRuleValues = 2
)

// The last values of the Client Information enums (hidden chat, right hand).
const (
	ChatModeHidden = 2
	MainHandLeft   = 0
	MainHandRight  = 1
)

// ChatModeFull is the chat mode a connection starts in and the only one a
// player chat broadcast reaches. vanilla: ChatVisiblity.FULL
const ChatModeFull = 0

// Built-in Server Links labels, a VarInt where a custom link carries a text
// component. vanilla: ServerLinks.KnownLinkType
const (
	// ServerLinkBugReport is the bug-report-link of a vanilla
	// server.properties, shown on the crash screen too.
	ServerLinkBugReport = 0
)

// World events sent in the World Event packet. Event 2001's data is the
// block state ID, from which the client looks up the break sound itself.
const (
	WorldEventBlockBreak = 2001
	// WorldEventDestroyBlockParticles is the destroy particles without the
	// break sound; data is the block state. vanilla: LevelEvent.PARTICLES_DESTROY_BLOCK
	WorldEventDestroyBlockParticles = 2014
	// WorldEventDestroyProgress is a held dig's crack particles; data is the
	// dig's face. vanilla: ServerPlayerGameMode.tick
	WorldEventDestroyProgress = 2019
	// WorldEventDestroyProgressSound is WorldEventDestroyProgress with the hit
	// sound, every fourth tick.
	WorldEventDestroyProgressSound = 2020
	// WorldEventFireExtinguish is a broken fire's extinguish sound, played
	// to the breaker too. vanilla: BaseFireBlock.playerWillDestroy
	WorldEventFireExtinguish = 1009
	// WorldEventMaceSmash is the dust a mace's smash raises at its victim's
	// feet, the data the particle count. vanilla: MaceItem.knockback
	WorldEventMaceSmash = 2013
	// WorldEventAnvilUse is what an AnvilMenu's take plays where the anvil
	// stood. vanilla: AnvilMenu.take
	WorldEventAnvilUse = 1030
	// WorldEventSmithingUse is what a SmithingMenu's take plays where the
	// smithing table stood. vanilla: SmithingMenu.onTake
	WorldEventSmithingUse = 1044
	// WorldEventLecternPageTurn is the page-turn sound a lectern's page
	// change plays at the block. vanilla: LecternBlock.signalPageChange
	WorldEventLecternPageTurn = 1043
	// WorldEventGrindstoneUsed is what a grindstone's result take plays
	// where the grindstone stands. vanilla: GrindstoneMenu's result onTake
	WorldEventGrindstoneUsed = 1042
	// WorldEventZombieDrowned and WorldEventHuskDrowned are the underwater
	// conversions' sounds.
	WorldEventZombieDrowned = 1040
	WorldEventHuskDrowned   = 1041
	// WorldEventZombieVillagerCured is the finished conversion's sound.
	// vanilla: ZombieVillager.finishConversion
	WorldEventZombieVillagerCured = 1027
	// WorldEventZombieVillagerInfected is the sound of a villager a zombie killed.
	// vanilla: Zombie.killedEntity
	WorldEventZombieVillagerInfected = 1026
	// WorldEventSkeletonToStray is the freeze conversion's sound.
	// vanilla: LevelEvent.SOUND_SKELETON_TO_STRAY
	WorldEventSkeletonToStray = 1048
	// WorldEventAnvilBroken is what a falling anvil plays where it gave up
	// its place and became an item. vanilla: AnvilBlock.onBrokenAfterFall
	WorldEventAnvilBroken = 1029
	// WorldEventAnvilLand is what a falling anvil plays where it lands.
	// vanilla: AnvilBlock.onLand
	WorldEventAnvilLand = 1031
	// WorldEventPortalTravel is the portal-travel sound a crossing player
	// hears after arriving. vanilla: LevelEvent.SOUND_PORTAL_TRAVEL
	WorldEventPortalTravel = 1032
	// WorldEventDripstoneLand and WorldEventSulfurSpikeLand play where a
	// falling speleothem became an item.
	// vanilla: PointedDripstoneBlock.onBrokenAfterFall
	WorldEventDripstoneLand   = 1045
	WorldEventSulfurSpikeLand = 1052
	// WorldEventDispense is a dispenser's click, WorldEventDispenseFail
	// (1001) the empty click, WorldEventProjectileLaunch (1002) the launch;
	// WorldEventDispenseAnim's data is the 3D data value of the facing.
	WorldEventDispense         = 1000
	WorldEventDispenseFail     = 1001
	WorldEventProjectileLaunch = 1002
	WorldEventDispenseAnim     = 2000
	// WorldEventGhastWarn and WorldEventGhastShoot are a ghast's charge and
	// launch. vanilla: LevelEvent.SOUND_GHAST_WARNING, LevelEvent.SOUND_GHAST_FIREBALL
	WorldEventGhastWarn  = 1015
	WorldEventGhastShoot = 1016
	// WorldEventWitherBreak is the sound of a wither breaking blocks,
	// WorldEventWitherSpawn the global sound of its summoning ending and
	// WorldEventWitherShoot a head's launch.
	// vanilla: LevelEvent.SOUND_WITHER_BLOCK_BREAK, SOUND_WITHER_BOSS_SPAWN, SOUND_WITHER_BOSS_SHOOT
	WorldEventWitherBreak = 1022
	WorldEventWitherSpawn = 1023
	WorldEventWitherShoot = 1024
	// WorldEventBlazeShoot is the sound a blaze fireball launch plays.
	// vanilla: LevelEvent.SOUND_BLAZE_FIREBALL
	WorldEventBlazeShoot = 1018
	// WorldEventCrafterCraft (1049) and WorldEventCrafterFail (1050) are a
	// crafter's sounds; WorldEventCrafterAnim's data is its facing.
	WorldEventCrafterCraft = 1049
	WorldEventCrafterFail  = 1050
	WorldEventCrafterAnim  = 2010
	// WorldEventBeeGrowth's data is the count of the green particles.
	// vanilla: LevelEvent.PARTICLES_BEE_GROWTH
	WorldEventBeeGrowth = 2011
	// WorldEventBonemeal's data is the count of the sparkle clusters.
	WorldEventBonemeal = 1505
	// WorldEventCobwebWeave is the web-laying sound a dying body's weaving
	// plays at each web it stands. vanilla: WeavingMobEffect's 3018
	WorldEventCobwebWeave = 3018
	// WorldEventEndPortalFrameFill is an eye set into a frame.
	// vanilla: LevelEvent.END_PORTAL_FRAME_FILL
	WorldEventEndPortalFrameFill = 1503
	// WorldEventEndPortalSpawn plays at a completed ring's centre, to every
	// player per global_sound_events. vanilla: LevelEvent.SOUND_END_PORTAL_SPAWN
	WorldEventEndPortalSpawn = 1038
	// WorldEventWaxOn is the sound and sparkles a honeycomb leaves where it
	// waxed something. vanilla: LevelEvent.PARTICLES_AND_SOUND_WAX_ON
	WorldEventWaxOn = 3003
	// WorldEventPotionSplash is a thrown potion's shatter and burst, its
	// data the colour; instant potions use WorldEventPotionSplashInstant.
	// vanilla: LevelEvent 2002 SPLASH_POTION and its INSTANT_POTION 2007
	WorldEventPotionSplash        = 2002
	WorldEventPotionSplashInstant = 2007
	// WorldEventPotionBreakSound is the glass-break sound that follows a
	// non-silent 2002, WorldEventInstantPotionBreakSound the one after 2007;
	// the client draws 2002/2007 as particles only.
	// vanilla: AbstractThrownPotion.onHit, ThrownExperienceBottle.onHit
	WorldEventPotionBreakSound        = 1053
	WorldEventInstantPotionBreakSound = 1054
	// WorldEventJukeboxPlay's data is the minecraft:jukebox_song ID.
	// vanilla: LevelEvent.SOUND_PLAY_JUKEBOX_SONG
	WorldEventJukeboxPlay = 1010
	// WorldEventJukeboxStop stops whatever a jukebox is playing.
	// vanilla: LevelEvent.SOUND_STOP_JUKEBOX_SONG
	WorldEventJukeboxStop = 1011
	// WorldEventBrewing is the gurgle a brewing stand plays where it
	// finished a brew. vanilla: LevelEvent.SOUND_BREWING_STAND_BREW
	WorldEventBrewing = 1035
	// The teleport trails, each with a PackPosDiff from the old cell to the
	// new as data: a dragon egg's (radii 16, 8, 16), a shulker's (8, 8, 8),
	// a teleport_randomly consume effect's and an enderman's (clamped, 127).
	// vanilla: DragonEggBlock.teleport, Shulker.teleportSomewhere,
	// TeleportRandomlyConsumeEffect.apply, EnderMan.teleport
	WorldEventDragonEggTeleport = 2015
	WorldEventShulkerTeleport   = 2016
	WorldEventConsumeTeleport   = 2017
	WorldEventEndermanTeleport  = 2018
)

// Sound categories, the Sound Effect packet's SoundSource ordinals, which
// /playsound names by their lower-case literal.
const (
	SoundCategoryMaster  = 0
	SoundCategoryMusic   = 1
	SoundCategoryRecord  = 2
	SoundCategoryWeather = 3
	SoundCategoryBlock   = 4
	SoundCategoryHostile = 5
	SoundCategoryNeutral = 6
	SoundCategoryPlayer  = 7
	SoundCategoryAmbient = 8
	SoundCategoryVoice   = 9
	SoundCategoryUI      = 10
)

// Attributes by minecraft:attribute ID, built into the client: from the
// vanilla data report, moving between versions.
const (
	// AttributeMaxHealth, and AttributeMovementSpeed, which a mob is sent
	// with so the client's walk animation matches.
	AttributeMaxHealth     = 23
	AttributeMovementSpeed = 26
	// AttributeJumpStrength is minecraft:jump_strength, the lift the client's
	// own jump of a ridden horse is computed from.
	AttributeJumpStrength = 19
	// AttributeArmor is minecraft:armor.
	AttributeArmor = 1
)

// SoundEventWindChargeBurst is minecraft:entity.wind_charge.wind_burst,
// numbered as SoundEventGenericExplode is (registry ID 1776 plus one).
const SoundEventWindChargeBurst = 1777

// SoundEventBreezeWindBurst is minecraft:entity.breeze.wind_burst, numbered
// as SoundEventGenericExplode is.
const SoundEventBreezeWindBurst = 211

// SoundEventGenericExplode is minecraft:entity.generic.explode in ID-or-X
// holder form: registry ID 703 (vanilla data report, moves between
// versions) plus one.
const SoundEventGenericExplode = 704

// Animations of the Entity Animation packet, played once; lasting state
// such as sneaking travels as entity metadata instead.
const (
	AnimationLeaveBed         = 0
	AnimationCriticalHit      = 1
	AnimationMagicCriticalHit = 2
)

// Hands, as the client names them when it swings one.
const (
	HandMain = iota
	HandOff
)

// IDOrZero maps a wire id onto an enum of n values: an id outside it is 0.
// vanilla: ByIdMap.OutOfBoundsStrategy.ZERO
func IDOrZero(id, n int32) int32 {
	if id < 0 || id >= n {
		return 0
	}
	return id
}

// IDWrap maps a wire id onto an enum of n values modulo n.
// vanilla: ByIdMap.OutOfBoundsStrategy.WRAP
func IDWrap(id, n int32) int32 {
	return (id%n + n) % n
}

// Actions of the serverbound Player Command packet; sneaking is reported in
// Player Input. Leaving the bed is the wake button.
const (
	PlayerCommandLeaveBed       = 0
	PlayerCommandStartSprinting = 1
	PlayerCommandStopSprinting  = 2
	// PlayerCommandStartFlyingElytra is the last of the vanilla enum.
	PlayerCommandStartFlyingElytra = 6
)

// Statuses of the serverbound Player Action packet. Creative breaks on
// StartedDigging, survival on FinishedDigging, except instant blocks, which
// break on StartedDigging: the client sends no FinishedDigging for a block
// it already broke.
const (
	PlayerActionStartedDigging = 0

	// PlayerActionChangeDestroyDirection re-aims a dig at another face.
	PlayerActionChangeDestroyDirection = 1

	PlayerActionCancelledDigging = 2
	PlayerActionFinishedDigging  = 3
	PlayerActionDropItemStack    = 4
	PlayerActionDropItem         = 5

	// PlayerActionReleaseUseItem is the client stopping a use short of done.
	PlayerActionReleaseUseItem = 6

	PlayerActionSwapItemInHand = 7

	// PlayerActionStab is the last enum value; it reads, and is ignored.
	PlayerActionStab = 8
)

// Faces of a block as Player Action and Use Item On number them: the offset
// from the clicked block to where a placed block goes.
const (
	FaceBottom = 0 // -Y
	FaceTop    = 1 // +Y
	FaceNorth  = 2 // -Z
	FaceSouth  = 3 // +Z
	FaceWest   = 4 // -X
	FaceEast   = 5 // +X
)

// FaceOffset is how many blocks each face is from the block it is on, so
// placing against a face adds its entry to the position the player clicked on.
var FaceOffset = [6][3]int32{
	FaceBottom: {0, -1, 0},
	FaceTop:    {0, 1, 0},
	FaceNorth:  {0, 0, -1},
	FaceSouth:  {0, 0, 1},
	FaceWest:   {-1, 0, 0},
	FaceEast:   {1, 0, 0},
}

// Flags of the serverbound Player Input packet, which is the state of the keys
// the player is holding down.
const (
	InputForward  = 0x01
	InputBackward = 0x02
	InputLeft     = 0x04
	InputRight    = 0x08
	InputJump     = 0x10
	InputSneak    = 0x20
	InputSprint   = 0x40
)

// Entity metadata field types, which each entry carries along with its index.
const (
	MetadataByte    = 0
	MetadataVarInt  = 1
	MetadataVarLong = 2
	MetadataFloat   = 3
	// MetadataString is a length-prefixed UTF-8 string; MetadataText a text
	// component in network NBT.
	MetadataString  = 4
	MetadataText    = 5
	MetadataSlot    = 7
	MetadataBoolean = 8
	MetadataPose    = 20
	// MetadataHumanoidArm is a varint enum, left 0 and right 1.
	MetadataHumanoidArm = 42
	// MetadataResolvableProfile is minecraft:profile's wire form.
	MetadataResolvableProfile = 41
	// MetadataArmadilloState is ArmadilloState as a VarInt enum.
	MetadataArmadilloState = 36
	// MetadataCopperGolemState and MetadataWeatheringCopperState are
	// CopperGolemState and WeatheringCopper.WeatherState as VarInt enums.
	MetadataCopperGolemState      = 37
	MetadataWeatheringCopperState = 38
	// MetadataSnifferState is Sniffer.State as a VarInt enum.
	MetadataSnifferState = 35
	// MetadataOptionalVarInt is an optional entity ID: 0 for absent, 1 +
	// the ID otherwise.
	MetadataOptionalVarInt = 19
	// MetadataOptionalLivingEntityReference is a boolean then, if set, the
	// UUID of the entity: a tamed mob's owner.
	MetadataOptionalLivingEntityReference = 13
	// MetadataBlockPos is a block position as one long; the falling
	// block's spawn-position field.
	MetadataBlockPos = 10
	// MetadataOptionalPosition is a boolean then a Position; the bed a
	// sleeping body lies in.
	MetadataOptionalPosition = 11
	// MetadataOptionalBlockState is a block state ID as a VarInt, 0 for
	// absent; the enderman's carried block.
	MetadataOptionalBlockState = 15
	// MetadataBlockState is a block state ID as a VarInt; the primed
	// TNT's block-state field.
	MetadataBlockState = 14
	// MetadataParticles is a VarInt-counted list of particles, each its
	// type ID and that type's data; a living body's effect particles.
	MetadataParticles = 17
	// MetadataParticle is one particle: its type ID and that type's data,
	// the un-counted form; the area effect cloud's rain.
	MetadataParticle = 16
	// MetadataOptionalText is a boolean then, if set, a text component in
	// network NBT; the custom name.
	MetadataOptionalText = 6
	// MetadataDirection is a Direction: down 0, up 1, north 2, south 3,
	// west 4, east 5.
	MetadataDirection = 12
	// The farm animals' variant types, each an ID in its own registry.
	MetadataCatVariant          = 21
	MetadataCatSoundVariant     = 22
	MetadataCowVariant          = 23
	MetadataCowSoundVariant     = 24
	MetadataPigVariant          = 28
	MetadataPigSoundVariant     = 29
	MetadataChickenVariant      = 30
	MetadataChickenSoundVariant = 31
	MetadataWolfVariant         = 25
	MetadataWolfSoundVariant    = 26
	// vanilla: EntityDataSerializers.FROG_VARIANT
	MetadataFrogVariant = 27
	// MetadataPaintingVariant is an ID-or-inline holder: a registry ID plus
	// one, or 0 followed by an inline definition.
	MetadataPaintingVariant = 34
	// MetadataVillagerData is three VarInts: the villager type, the
	// profession and the level.
	MetadataVillagerData = 18
	// MetadataRotations is three floats — the x, y and z of a rotation —
	// the armor stand's six pose fields.
	MetadataRotations = 9
)

// Data component types by minecraft:data_component_type ID, built into the
// client: from the vanilla data report, moving between versions
// (tools/gen itemprops checks the first).
const (
	// DataComponentCustomData is minecraft:custom_data, the bare network NBT
	// compound a pack or a command keeps on a stack.
	DataComponentCustomData = 0
	// DataComponentDamage is minecraft:damage, the wear a tool or a piece of
	// armour has taken.
	DataComponentDamage = 3
	// DataComponentUnbreakable is minecraft:unbreakable; its value is the
	// unit, no data follows the type on the wire.
	DataComponentUnbreakable = 4
	// DataComponentCustomName is minecraft:custom_name, travelling as a
	// text component.
	DataComponentCustomName = 6
	// DataComponentEnchantments is minecraft:enchantments: pairs of
	// registry IDs and levels.
	DataComponentEnchantments = 13
	// DataComponentRepairCost is minecraft:repair_cost, what the next
	// repair's level cost grows from.
	DataComponentRepairCost = 19
	// DataComponentEnchantmentGlintOverride's presence is part of the
	// value: a default-glinting item carries it to hide the glint.
	DataComponentEnchantmentGlintOverride = 21
	// DataComponentStoredEnchantments is minecraft:stored_enchantments, an
	// enchanted book's stores; it travels as the enchantments do.
	DataComponentStoredEnchantments = 45
	// DataComponentContainer is minecraft:container, the slots a shulker
	// box holds inside its own stack, so a filled box stays one stack.
	DataComponentContainer = 77
)

// DataComponentTypeCount is the size of minecraft:data_component_type
// (vanilla data report): the bound the client's patch codec checks a type
// against, so a type past it names nothing.
const DataComponentTypeCount = 122

// The unmodelled data components held as raw bytes. Their wire shapes are
// the client's stream codecs, so a reader can check the relaying client
// would accept them; any other unmodelled component is refused.
const (
	DataComponentBlockTransformer         = 43  // minecraft:block_transformer
	DataComponentDyedColor                = 47  // minecraft:dyed_color
	DataComponentSulfurCubeContent        = 80  // minecraft:sulfur_cube_content
	DataComponentBucketEntityData         = 61  // minecraft:bucket_entity_data
	DataComponentSalmonSize               = 93  // minecraft:salmon/size
	DataComponentTropicalFishPattern      = 95  // minecraft:tropical_fish/pattern
	DataComponentTropicalFishBaseColor    = 96  // minecraft:tropical_fish/base_color
	DataComponentTropicalFishPatternColor = 97  // minecraft:tropical_fish/pattern_color
	DataComponentAxolotlVariant           = 111 // minecraft:axolotl/variant
	DataComponentVillagerVariant          = 88  // minecraft:villager/variant
	DataComponentWolfVariant              = 89  // minecraft:wolf/variant
	DataComponentWolfSoundVariant         = 90  // minecraft:wolf/sound_variant
	DataComponentWolfCollar               = 91  // minecraft:wolf/collar
	DataComponentFoxVariant               = 92  // minecraft:fox/variant
	DataComponentParrotVariant            = 94  // minecraft:parrot/variant
	DataComponentMooshroomVariant         = 98  // minecraft:mooshroom/variant
	DataComponentPigVariant               = 100 // minecraft:pig/variant
	DataComponentPigSoundVariant          = 101 // minecraft:pig/sound_variant
	DataComponentCowVariant               = 102 // minecraft:cow/variant
	DataComponentCowSoundVariant          = 103 // minecraft:cow/sound_variant
	DataComponentChickenVariant           = 104 // minecraft:chicken/variant
	DataComponentChickenSoundVariant      = 105 // minecraft:chicken/sound_variant
	DataComponentZombieNautilusVariant    = 106 // minecraft:zombie_nautilus/variant
	DataComponentFrogVariant              = 107 // minecraft:frog/variant
	DataComponentHorseVariant             = 108 // minecraft:horse/variant
	DataComponentLlamaVariant             = 110 // minecraft:llama/variant
	DataComponentCatVariant               = 112 // minecraft:cat/variant
	DataComponentCatSoundVariant          = 113 // minecraft:cat/sound_variant
	DataComponentCatCollar                = 114 // minecraft:cat/collar
	DataComponentSheepColor               = 115 // minecraft:sheep/color
	DataComponentShulkerColor             = 116 // minecraft:shulker/color
	DataComponentCushionColor             = 121 // minecraft:cushion/color
	DataComponentBundleContents           = 52  // minecraft:bundle_contents
	DataComponentChargedProjectiles       = 51  // minecraft:charged_projectiles
	DataComponentPotionContents           = 53  // minecraft:potion_contents
	DataComponentPotionDurationScale      = 54  // minecraft:potion_duration_scale
	DataComponentSuspiciousStew           = 55  // minecraft:suspicious_stew_effects
	DataComponentOminousBottleAmplifier   = 65  // minecraft:ominous_bottle_amplifier
	DataComponentJukeboxPlayable          = 66  // minecraft:jukebox_playable
	DataComponentFireworkExplosion        = 70  // minecraft:firework_explosion
	DataComponentFireworks                = 71  // minecraft:fireworks
	DataComponentTrim                     = 58  // minecraft:trim
	DataComponentInstrument               = 63  // minecraft:instrument
	DataComponentWritableBookContent      = 56  // minecraft:writable_book_content
	DataComponentWrittenBookContent       = 57  // minecraft:written_book_content
	DataComponentProfile                  = 72  // minecraft:profile
	DataComponentCustomModelData          = 17  // minecraft:custom_model_data
	DataComponentBannerPatterns           = 74  // minecraft:banner_patterns
	DataComponentItemName                 = 9   // minecraft:item_name
	DataComponentLore                     = 11  // minecraft:lore
	DataComponentAttributeModifiers       = 16  // minecraft:attribute_modifiers
	DataComponentRarity                   = 12  // minecraft:rarity
	DataComponentItemModel                = 10  // minecraft:item_model
	DataComponentTooltipStyle             = 35  // minecraft:tooltip_style
	DataComponentCompostable              = 84  // minecraft:compostable
	DataComponentUseEffects               = 5   // minecraft:use_effects
	DataComponentPiercingWeapon           = 38  // minecraft:piercing_weapon
	DataComponentKineticWeapon            = 39  // minecraft:kinetic_weapon
	DataComponentAttackAnimation          = 40  // minecraft:attack_animation
	DataComponentInteractAnimation        = 41  // minecraft:interact_animation
	DataComponentVillagerFood             = 44  // minecraft:villager_food
	DataComponentCookingFuel              = 85  // minecraft:cooking_fuel
	DataComponentBrewingFuel              = 86  // minecraft:brewing_fuel
	DataComponentMobVisibility            = 87  // minecraft:mob_visibility
	DataComponentMaxStackSize             = 1   // minecraft:max_stack_size
	DataComponentMaxDamage                = 2   // minecraft:max_damage
	DataComponentMinimumAttackCharge      = 7   // minecraft:minimum_attack_charge
	DataComponentDamageType               = 8   // minecraft:damage_type
	DataComponentEnchantable              = 31  // minecraft:enchantable
	DataComponentRepairable               = 33  // minecraft:repairable
	DataComponentDye                      = 46  // minecraft:dye
	DataComponentProvidesTrimMaterial     = 64  // minecraft:provides_trim_material
	DataComponentProvidesBannerPatterns   = 67  // minecraft:provides_banner_patterns
	DataComponentProvidesPotteryPattern   = 117 // minecraft:provides_pottery_pattern
	DataComponentBreakSound               = 83  // minecraft:break_sound
	DataComponentTooltipDisplay           = 18  // minecraft:tooltip_display
	DataComponentBaseColor                = 75  // minecraft:base_color
	DataComponentPaintingVariant          = 109 // minecraft:painting/variant
	DataComponentRabbitVariant            = 99  // minecraft:rabbit/variant
	// DataComponentLodestoneTracker is where a compass points and whether
	// the game keeps it pointed.
	DataComponentLodestoneTracker = 69
	// DataComponentBlockState is minecraft:block_state, the state
	// properties a block item applies to the block it places.
	DataComponentBlockState = 78
	// DataComponentEntityData is minecraft:entity_data, an entity compound
	// a spawned entity reads its fields from.
	DataComponentEntityData = 60
	// DataComponentMapID names a filled map's data/minecraft/maps/<id>.dat.
	DataComponentMapID = 48

	// DataComponentMapDecorations carries target markers as network NBT.
	DataComponentMapDecorations = 49
	// DataComponentBlockEntityData is minecraft:block_entity_data, a block
	// entity type and the compound a placed block reads its data from.
	DataComponentBlockEntityData = 62
	// DataComponentNoteBlockSound is minecraft:note_block_sound, the sound
	// a note block or a player head plays, a resource location.
	DataComponentNoteBlockSound = 73
	// DataComponentPotDecorations is minecraft:pot_decorations, the four
	// sherd items of a decorated pot.
	DataComponentPotDecorations = 76
	// DataComponentBees is minecraft:bees, the occupants of a beehive.
	DataComponentBees = 79
	// DataComponentSignTextFront and DataComponentSignTextBack are a sign
	// item's two sides, which its placement lays on the sign.
	DataComponentSignTextFront = 118
	DataComponentSignTextBack  = 119
	// DataComponentWaxed is minecraft:waxed, a unit marking a waxed sign.
	DataComponentWaxed = 120
	// DataComponentMapPostProcessing is a cartography table's zoom or lock
	// mark: transient, never saved, applied when the result is taken.
	DataComponentMapPostProcessing = 50
	// DataComponentCanPlaceOn and DataComponentCanBreak are the block
	// predicates an adventure player may build against and dig.
	DataComponentCanPlaceOn = 14
	DataComponentCanBreak   = 15
	// DataComponentEquippable is minecraft:equippable, held as raw bytes and
	// read by ItemStack.EquippableOf.
	DataComponentEquippable = 32
	// DataComponentFood and DataComponentConsumable are minecraft:food and
	// minecraft:consumable, held as raw bytes and read by ItemStack.FoodOf and
	// ItemStack.ConsumableOf.
	DataComponentFood       = 23
	DataComponentConsumable = 24
	// DataComponentUseRemainder is minecraft:use_remainder, an item template.
	DataComponentUseRemainder = 25
	// DataComponentUseCooldown is minecraft:use_cooldown, held as raw bytes
	// and read by ItemStack.UseCooldownOf.
	DataComponentUseCooldown = 26
	// DataComponentTool is minecraft:tool, held as raw bytes and read by
	// ItemStack.ToolOf.
	DataComponentTool = 28
	// DataComponentWeapon is minecraft:weapon, held as raw bytes and read by
	// ItemStack.WeaponOf.
	DataComponentWeapon = 29
	// DataComponentBlocksAttacks is minecraft:blocks_attacks, held as raw
	// bytes and read by ItemStack.BlocksAttacksOf.
	DataComponentBlocksAttacks = 37
	// DataComponentDamageResistant, DataComponentGlider and
	// DataComponentDeathProtection are held as raw bytes and read by
	// ItemStack.DamageResistantOf, HasGlider and DeathProtectionOf.
	DataComponentDamageResistant = 27
	DataComponentGlider          = 34
	DataComponentDeathProtection = 36
	// DataComponentAttackRange is minecraft:attack_range; only its removal
	// is modelled (ItemStack.Removes).
	DataComponentAttackRange = 30
)

// Registry sizes the raw components' holders are checked against, since
// the client refuses an out-of-range ID with the whole stack. Built-in
// registries' sizes are from the vanilla data report (tools/README.md);
// banner pattern and painting variant, sent at join, from tools/probe's
// registry dump.
const (
	RegistrySizeItem            = 1658
	RegistrySizePotion          = 46
	RegistrySizeMobEffect       = 40
	RegistrySizeBannerPattern   = 43
	RegistrySizePaintingVariant = 51
	// RegistrySizeInstrument is the minecraft:instrument registry this
	// server sends at join: the eight goat horns.
	RegistrySizeInstrument = 8
	// RegistrySizeBlockTransformer is the built-in minecraft:block_transformer count.
	RegistrySizeBlockTransformer = 3
	// RegistrySizeJukeboxSong is the built-in minecraft:jukebox_song count.
	RegistrySizeJukeboxSong = 22
	// RegistrySizeTrimMaterial and RegistrySizeTrimPattern bound
	// minecraft:trim's references; both are sent at join, so the sizes are
	// from tools/probe's registry dump.
	RegistrySizeTrimMaterial = 11
	RegistrySizeTrimPattern  = 18
	RegistrySizeEntityType   = 161
	// RegistrySizeSoundEvent bounds a sound holder's registry ID.
	RegistrySizeSoundEvent = 1991
	// RegistrySizeBlockEntityType bounds a block_entity_data's type ID, the
	// minecraft:block_entity_type registry built into the client.
	RegistrySizeBlockEntityType = 49
	// RegistrySizeBlock bounds can_place_on/can_break's inline block IDs.
	RegistrySizeBlock = 1286
)

// The glint override has three values, not two: presence is part of it,
// and an enchanted book showing its glint carries no component at all.
const (
	GlintUnset  int8 = 0
	GlintShown  int8 = 1
	GlintHidden int8 = 2
)

// Entity metadata indices. The hand states make a client draw a player
// eating, drinking or blocking (0 for none).
const (
	MetadataIndexEntityFlags = 0
	// MetadataIndexAir is the air-supply VarInt the client draws the
	// bubbles' countdown from.
	MetadataIndexAir = 1
	// MetadataIndexCustomName and MetadataIndexCustomNameVisible are
	// Entity's DATA_CUSTOM_NAME and DATA_CUSTOM_NAME_VISIBLE.
	MetadataIndexCustomName        = 2
	MetadataIndexCustomNameVisible = 3
	// MetadataIndexSilent is Entity's DATA_SILENT, the flag that mutes the
	// sounds the client plays for a body itself. vanilla: Entity.DATA_SILENT
	MetadataIndexSilent = 4
	// MetadataIndexNoGravity stops the client predicting a fall for a body
	// the server holds still. vanilla: Entity.DATA_NO_GRAVITY
	MetadataIndexNoGravity = 5
	MetadataIndexPose      = 6
	// MetadataIndexTicksFrozen is Entity's DATA_TICKS_FROZEN, the powder
	// snow a body is freezing in, counted one a tick inside it.
	// vanilla: Entity.DATA_TICKS_FROZEN
	MetadataIndexTicksFrozen = 7
	// MetadataIndexSleepingPos is the bed a sleeping body lies in. An
	// empty entry — not the absence of one — is what takes the sleeper
	// back out: metadata is absolute.
	MetadataIndexSleepingPos = 14
	// MetadataIndexItemSlot is the Item field of the minecraft:item
	// entity, which the thrown projectiles carry too — the potion among
	// them, its stack the item they are a stack of.
	MetadataIndexItemSlot = 8
	// MetadataIndexHandState is the hand-state byte living entities carry.
	MetadataIndexHandState = 8
	// MetadataIndexLivingHealth is the health float every living entity
	// carries.
	MetadataIndexLivingHealth = 9
	// MetadataIndexArrowCount is the VarInt of arrows a living entity wears.
	MetadataIndexArrowCount = 12
	// MetadataIndexStingerCount is the VarInt of bee stingers a living entity wears.
	MetadataIndexStingerCount = 13
	// MetadataIndexCloudRadius, MetadataIndexCloudWaiting and
	// MetadataIndexCloudParticle are an area effect cloud's radius, the
	// flag delaying its first application, and its particle.
	MetadataIndexCloudRadius   = 8
	MetadataIndexCloudWaiting  = 9
	MetadataIndexCloudParticle = 10
	// MetadataIndexEffectParticles and MetadataIndexEffectAmbient are a
	// living body's status effect particles and whether they are all
	// ambient (beacon) ones.
	MetadataIndexEffectParticles = 10
	MetadataIndexEffectAmbient   = 11
	// MetadataIndexAbsorption is a player's extra hearts.
	MetadataIndexAbsorption = 17
	// MetadataIndexScore is a player's score, a VarInt shown on the death
	// screen.
	MetadataIndexScore = 18
	// MetadataIndexShoulderLeft and MetadataIndexShoulderRight are the variant
	// of the parrot on each shoulder, an optional unsigned int.
	MetadataIndexShoulderLeft  = 19
	MetadataIndexShoulderRight = 20
	// MetadataIndexMainHand is a player's main arm (MainHandLeft or
	// MainHandRight); vanilla's default is right.
	MetadataIndexMainHand = 15
	// MetadataIndexSkinParts is the byte of optional skin layers a player's
	// own metadata carries alone.
	MetadataIndexSkinParts = 16
	// MetadataIndexMobFlags is the bit-mask byte every mob carries; the
	// aggressive bit makes a client draw a zombie with its arms raised.
	MetadataIndexMobFlags = 15
	// MetadataIndexBaby is the baby flag every ageable mob carries.
	MetadataIndexBaby = 16
	// MetadataIndexPiglinImmune is AbstractPiglin's zombification immunity.
	MetadataIndexPiglinImmune = 16
	// MetadataIndexPiglinBaby is Piglin.DATA_BABY_ID.
	MetadataIndexPiglinBaby = 17
	// MetadataIndexPiglinCharging is Piglin.DATA_IS_CHARGING_CROSSBOW.
	MetadataIndexPiglinCharging = 18
	// MetadataIndexPiglinDancing is Piglin.DATA_IS_DANCING.
	MetadataIndexPiglinDancing = 19
	// MetadataIndexAgeLocked is AgeableMob's AGE_LOCKED.
	MetadataIndexAgeLocked = 17

	// vanilla: Dolphin.GOT_FISH, MOISTNESS_LEVEL after AgeableMob.
	MetadataIndexDolphinGotFish   = 18
	MetadataIndexDolphinMoistness = 19
	// MetadataIndexBeeFlags is Bee.DATA_FLAGS_ID: roll 0x02, has stung 0x04,
	// has nectar 0x08.
	MetadataIndexBeeFlags = 18
	// MetadataIndexBeeAngerEnd is Bee.DATA_ANGER_END_TIME, -1 for none.
	MetadataIndexBeeAngerEnd = 19
	// MetadataIndexTurtleHasEgg and MetadataIndexTurtleLayingEgg are the
	// turtle's two booleans after AgeableMob's AGE_LOCKED.
	// vanilla: Turtle.HAS_EGG, Turtle.LAYING_EGG
	MetadataIndexTurtleHasEgg    = 18
	MetadataIndexTurtleLayingEgg = 19
	// MetadataIndexHappyGhastStaysStill is HappyGhast.STAYS_STILL, after its
	// IS_LEASH_HOLDER at 18.
	MetadataIndexHappyGhastStaysStill = 19
	// MetadataIndexHookedEntity is the FishingHook's DATA_HOOKED_ENTITY,
	// the body the bobber is hooked in: that body's entity ID plus one,
	// zero for none.
	MetadataIndexHookedEntity = 8
	// MetadataIndexBite is the FishingHook's DATA_BITING, "Is catchable"
	// on the wire, the bite the client pops the bobber down for.
	MetadataIndexBite = 9
	// The variant and sound variant of a cow or chicken (18, 19) and of a
	// pig (19, 20, after its boost time).
	MetadataIndexCowVariant          = 18
	MetadataIndexCowSoundVariant     = 19
	MetadataIndexChickenVariant      = 18
	MetadataIndexChickenSoundVariant = 19
	MetadataIndexPigVariant          = 19
	MetadataIndexPigSoundVariant     = 20
	// The wolf's collar colour (a dye ID VarInt, red by default), variant and
	// sound variant, after TamableAnimal's two and the begging flag.
	// vanilla: Wolf.DATA_INTERESTED_ID, DATA_COLLAR_COLOR, DATA_VARIANT_ID, DATA_SOUND_VARIANT_ID
	MetadataIndexWolfBegging      = 20
	MetadataIndexWolfCollar       = 21
	MetadataIndexWolfAngerEnd     = 22
	MetadataIndexWolfVariant      = 23
	MetadataIndexWolfSoundVariant = 24
	// MetadataIndexSheepColour is the sheep's own byte: the wool colour in
	// its low nibble, the sheared flag in bit 0x10.
	MetadataIndexSheepColour = 18
	// MetadataIndexHorseFlags is AbstractHorse's flag byte (HorseFlagTame,
	// HorseFlagBred, HorseFlagEating, HorseFlagStanding, HorseFlagMouthOpen)
	// and MetadataIndexHorseVariant the horse's colour in the low byte and
	// markings in the next.
	// vanilla: AbstractHorse.DATA_ID_FLAGS, Horse.DATA_ID_TYPE_VARIANT
	MetadataIndexHorseFlags   = 18
	MetadataIndexHorseVariant = 19
	// MetadataIndexChestedHorseChest is the donkey's and mule's chest flag.
	// vanilla: AbstractChestedHorse.DATA_ID_CHEST
	MetadataIndexChestedHorseChest = 19
	// MetadataIndexLlamaStrength and MetadataIndexLlamaVariant are a llama's
	// strength (1 to 5) and colour (0 to 3).
	// vanilla: Llama.DATA_STRENGTH_ID, Llama.DATA_VARIANT_ID
	MetadataIndexLlamaStrength = 20
	MetadataIndexLlamaVariant  = 21
	// MetadataIndexTamableFlags is TamableAnimal's byte (TamableSitting,
	// TamableTame) and MetadataIndexTamableOwner its owner reference.
	// vanilla: TamableAnimal.DATA_FLAGS_ID, DATA_OWNERUUID_ID
	MetadataIndexTamableFlags = 18
	MetadataIndexTamableOwner = 19
	// MetadataIndexNautilusDash is AbstractNautilus.DASH after TamableAnimal.
	MetadataIndexNautilusDash = 20
	// MetadataIndexZombieNautilusVariant follows AbstractNautilus.DASH.
	MetadataIndexZombieNautilusVariant = 21
	// MetadataZombieNautilusVariant is EntityDataSerializers.ZOMBIE_NAUTILUS_VARIANT.
	MetadataZombieNautilusVariant = 32
	// MetadataIndexParrotVariant is Parrot.DATA_VARIANT_ID after TamableAnimal.
	MetadataIndexParrotVariant = 20
	// The cat's variant and sound variant, and the ocelot's trusting flag.
	// vanilla: Cat.DATA_VARIANT_ID, IS_LYING, RELAX_STATE_ONE,
	// DATA_COLLAR_COLOR, DATA_SOUND_VARIANT_ID; Ocelot.DATA_TRUSTING
	MetadataIndexCatVariant      = 20
	MetadataIndexCatLying        = 21
	MetadataIndexCatRelaxing     = 22
	MetadataIndexCatCollar       = 23
	MetadataIndexCatSoundVariant = 24
	MetadataIndexOcelotTrusting  = 18
	// MetadataIndexMooshroomVariant is the mooshroom's variant VarInt:
	// its 0 the red, its 1 the brown, in the index the sheep shares.
	// vanilla: MushroomCow.DATA_TYPE
	MetadataIndexMooshroomVariant = 18
	// MetadataIndexRabbitType is Rabbit.DATA_TYPE_ID, a VarInt.
	MetadataIndexRabbitType = 18
	// MetadataIndexPolarBearStanding is PolarBear.DATA_STANDING_ID, a boolean.
	MetadataIndexPolarBearStanding = 18
	// MetadataIndexCamelDash and MetadataIndexCamelLastPoseChangeTick are
	// Camel.DASH (a boolean) and LAST_POSE_CHANGE_TICK (a long) after
	// AbstractHorse's flags.
	MetadataIndexCamelDash               = 19
	MetadataIndexCamelLastPoseChangeTick = 20
	// MetadataIndexAllayDancing and MetadataIndexAllayCanDuplicate are
	// Allay.DATA_DANCING and DATA_CAN_DUPLICATE, booleans after Mob's flags.
	MetadataIndexAllayDancing      = 16
	MetadataIndexAllayCanDuplicate = 17
	// MetadataIndexArmadilloState is Armadillo.ARMADILLO_STATE.
	MetadataIndexArmadilloState = 18
	// MetadataIndexCopperGolemWeather and MetadataIndexCopperGolemState are
	// CopperGolem.DATA_WEATHER_STATE and COPPER_GOLEM_STATE.
	MetadataIndexCopperGolemWeather = 16
	MetadataIndexCopperGolemState   = 17
	// MetadataIndexSnifferState and MetadataIndexSnifferDropSeedAt are
	// Sniffer.DATA_STATE and DATA_DROP_SEED_AT_TICK.
	MetadataIndexSnifferState      = 18
	MetadataIndexSnifferDropSeedAt = 19
	// MetadataIndexPandaUnhappyCounter and the five after it are Panda's
	// UNHAPPY_COUNTER, SNEEZE_COUNTER, EAT_COUNTER (VarInts), MAIN_GENE_ID,
	// HIDDEN_GENE_ID and DATA_ID_FLAGS (Bytes).
	MetadataIndexPandaUnhappyCounter = 18
	MetadataIndexPandaSneezeCounter  = 19
	MetadataIndexPandaEatCounter     = 20
	MetadataIndexPandaMainGene       = 21
	MetadataIndexPandaHiddenGene     = 22
	MetadataIndexPandaFlags          = 23
	// MetadataIndexFoxType and MetadataIndexFoxFlags are Fox.DATA_TYPE_ID (a
	// VarInt) and Fox.DATA_FLAGS_ID (a byte of the Fox* bits).
	MetadataIndexFoxType  = 18
	MetadataIndexFoxFlags = 19
	// vanilla: Axolotl.DATA_VARIANT, DATA_PLAYING_DEAD, FROM_BUCKET
	MetadataIndexAxolotlVariant     = 18
	MetadataIndexAxolotlPlayingDead = 19
	MetadataIndexAxolotlFromBucket  = 20
	// AbstractFish.FROM_BUCKET and Salmon.DATA_TYPE, a boolean and a VarInt.
	MetadataIndexFishFromBucket = 16
	// vanilla: Tadpole.AGE_LOCKED, Frog.DATA_VARIANT_ID
	MetadataIndexTadpoleAgeLocked = 17
	MetadataIndexFrogVariant      = 18
	// vanilla: Frog.DATA_TONGUE_TARGET_ID, OPTIONAL_UNSIGNED_INT
	MetadataIndexFrogTongueTarget    = 19
	MetadataIndexSalmonSize          = 17
	MetadataIndexTropicalFishVariant = 17
	// vanilla: Pufferfish.PUFF_STATE, a VarInt
	MetadataIndexPufferfishPuff = 17
	// MetadataIndexGuardianMoving and MetadataIndexGuardianTarget are
	// Guardian.DATA_ID_MOVING and DATA_ID_ATTACK_TARGET, a boolean and a
	// VarInt.
	// MetadataIndexWardenAnger is Warden.CLIENT_ANGER_LEVEL, a VarInt.
	MetadataIndexWardenAnger    = 16
	MetadataIndexGuardianMoving = 16
	// MetadataIndexBlazeFlags is Blaze.DATA_FLAGS_ID, a byte whose bit 0 is
	// the charged state.
	MetadataIndexBlazeFlags = 16
	// MetadataIndexWitherTargetA to C and MetadataIndexWitherInvul are
	// WitherBoss.DATA_TARGET_A to C and DATA_ID_INV, VarInts.
	MetadataIndexWitherTargetA = 16
	MetadataIndexWitherTargetB = 17
	MetadataIndexWitherTargetC = 18
	MetadataIndexWitherInvul   = 19
	// MetadataIndexGhastCharging is Ghast.DATA_IS_CHARGING, a boolean.
	MetadataIndexGhastCharging = 16
	// MetadataIndexDragonPhase is EnderDragon.DATA_PHASE, an integer.
	MetadataIndexDragonPhase    = 16
	MetadataIndexGuardianTarget = 17
	// MetadataIndexCreakingCanMove, MetadataIndexCreakingActive,
	// MetadataIndexCreakingTearingDown and MetadataIndexCreakingHome are
	// Creaking.CAN_MOVE, IS_ACTIVE, IS_TEARING_DOWN (booleans) and HOME_POS
	// (an optional position).
	MetadataIndexCreakingCanMove     = 16
	MetadataIndexCreakingActive      = 17
	MetadataIndexCreakingTearingDown = 18
	MetadataIndexCreakingHome        = 19
	// MetadataIndexShulkerFace, MetadataIndexShulkerPeek and
	// MetadataIndexShulkerColor are Shulker.DATA_ATTACH_FACE_ID,
	// DATA_PEEK_ID and DATA_COLOR_ID, a direction and two bytes (16 undyed).
	MetadataIndexShulkerFace  = 16
	MetadataIndexShulkerPeek  = 17
	MetadataIndexShulkerColor = 18
	// MetadataIndexStriderBoost is Strider.DATA_BOOST_TIME, a VarInt.
	MetadataIndexStriderBoost = 18
	// MetadataIndexPigBoost is Pig.DATA_BOOST_TIME, a VarInt.
	MetadataIndexPigBoost = MetadataIndexStriderBoost
	// MetadataIndexStriderCold is Strider.DATA_SUFFOCATING, a boolean.
	MetadataIndexStriderCold = 19
	// MetadataIndexGoatScreaming, MetadataIndexGoatLeftHorn and
	// MetadataIndexGoatRightHorn are Goat's three booleans.
	// vanilla: Goat.DATA_IS_SCREAMING_GOAT, DATA_HAS_LEFT_HORN, DATA_HAS_RIGHT_HORN
	MetadataIndexGoatScreaming = 18
	MetadataIndexGoatLeftHorn  = 19
	MetadataIndexGoatRightHorn = 20
	// MetadataIndexSpiderClimbing is the spider's own byte; bit 0x01 is
	// what the client draws a wall-climbing spider with. vanilla: Spider.tick
	MetadataIndexSpiderClimbing = 16
	// MetadataIndexCreeperSwell is the creeper's swell direction, 1 while
	// the fuse runs and -1 idle. vanilla: DATA_SWELL_DIR
	MetadataIndexCreeperSwell = 16
	// MetadataIndexCreeperPowered is the creeper's "is charged" boolean,
	// the flag its own synched data carries. vanilla: DATA_IS_POWERED
	MetadataIndexCreeperPowered = 17
	// MetadataIndexCreeperIgnited is the creeper's ignited boolean, set
	// when a creeper_igniters item lights the fuse.
	// vanilla: Creeper.DATA_IS_IGNITED
	MetadataIndexCreeperIgnited = 18
	// MetadataIndexEndermanScreaming and MetadataIndexEndermanStaring are the
	// enderman's creepy and stared-at booleans, after its carried block at
	// index 16. vanilla: EnderMan.DATA_CREEPY, EnderMan.DATA_STARED_AT
	MetadataIndexEndermanCarried   = 16
	MetadataIndexEndermanScreaming = 17
	MetadataIndexEndermanStaring   = 18
	// MetadataIndexRaiderCelebrating is the raider's celebrating boolean.
	// vanilla: Raider.IS_CELEBRATING
	MetadataIndexRaiderCelebrating = 16
	// MetadataIndexWitchDrinking is the witch's using-item boolean, after
	// the raider's celebrating flag at index 16.
	// vanilla: Witch.DATA_USING_ITEM
	MetadataIndexWitchDrinking = 17
	// MetadataIndexPillagerCharging is the pillager's charging-crossbow
	// boolean, after the raider's celebrating flag at index 16.
	// vanilla: Pillager.IS_CHARGING_CROSSBOW
	MetadataIndexPillagerCharging = 17
	// MetadataIndexVexFlags is the vex's flag byte, bit 0x01 charging.
	// vanilla: Vex.DATA_FLAGS_ID
	MetadataIndexVexFlags = 16
	// MetadataIndexSpellcasting is a spellcaster illager's spell byte, after
	// the raider's celebrating flag at index 16.
	// vanilla: SpellcasterIllager.DATA_SPELL_CASTING_ID
	MetadataIndexSpellcasting = 17
	// MetadataIndexBoggedSheared is the bogged's sheared boolean.
	// vanilla: Bogged.DATA_SHEARED
	MetadataIndexBoggedSheared = 16
	// MetadataIndexGolemFlags is the golems' flag byte: the iron golem's
	// player-created 0x01 and the snow golem's pumpkin 0x10, set by default.
	// vanilla: IronGolem.DATA_FLAGS_ID, SnowGolem.DATA_PUMPKIN_ID
	MetadataIndexGolemFlags = 16
	// MetadataIndexPhantomSize is the phantom's size int.
	// vanilla: Phantom.ID_SIZE
	MetadataIndexPhantomSize = 16
	// MetadataIndexCubeSize is a slime's, magma cube's or sulfur cube's size.
	// vanilla: AbstractCubeMob.ID_SIZE
	MetadataIndexCubeSize = 18
	// MetadataIndexSulfurCubeMaxFuse is SulfurCube.MAX_FUSE, the first field
	// after AbstractCubeMob.ID_SIZE.
	MetadataIndexSulfurCubeMaxFuse = 19
	// MetadataIndexSulfurCubeFromBucket follows AbstractCubeMob.ID_SIZE and SulfurCube.MAX_FUSE.
	MetadataIndexSulfurCubeFromBucket = 20
	// MetadataIndexZombieDrowning is the zombie's becoming-a-drowned
	// boolean. vanilla: Zombie.DATA_DROWNED_CONVERSION_ID
	MetadataIndexZombieDrowning = 18
	// MetadataIndexSkeletonStraying is the skeleton's freeze-converting
	// boolean. vanilla: Skeleton.DATA_STRAY_CONVERSION_ID
	MetadataIndexSkeletonStraying = 16
	// MetadataIndexGlowSquidDark is the glow squid's dark ticks remaining,
	// the count a struck body's glow dims down by.
	// vanilla: GlowSquid.DATA_DARK_TICKS_REMAINING
	MetadataIndexGlowSquidDark = 18
	// MetadataIndexZombieVillagerCuring is the zombie villager's curing flag.
	// vanilla: ZombieVillager.DATA_CONVERTING_ID, after Zombie's three fields
	MetadataIndexZombieVillagerCuring = 19
	// MetadataIndexVillagerData and MetadataIndexVillagerDataFinalized are
	// the zombie villager's VillagerData and its finalized boolean.
	MetadataIndexVillagerData          = 20
	MetadataIndexVillagerDataFinalized = 21
	// MetadataIndexVillagerVillagerData and MetadataIndexVillagerFinalized
	// are the villager's own, after AbstractVillager's unhappy counter.
	// vanilla: Villager.DATA_VILLAGER_DATA, DATA_VILLAGER_DATA_FINALIZED
	MetadataIndexVillagerUnhappy      = 18
	MetadataIndexVillagerVillagerData = 19
	MetadataIndexVillagerFinalized    = 20
	// The abstract arrow's own fields, at the indices
	// AbstractArrow.defineSynchedData registers; a plain arrow carries
	// none of them away from their defaults.
	MetadataIndexArrowFlags    = 8
	MetadataIndexArrowPierce   = 9
	MetadataIndexArrowInGround = 10
	// MetadataIndexArrowColor is the Arrow's effect colour VarInt, -1 for
	// none.
	MetadataIndexArrowColor = 11
	// The trident's fields past the abstract arrow's: loyalty and glint.
	// vanilla: ThrownTrident.ID_LOYALTY, ID_FOIL
	MetadataIndexTridentLoyalty = 11
	MetadataIndexTridentFoil    = 12
	// MetadataIndexOrbValue is the experience orb's worth; a fresh orb is
	// worth nothing at the default, so the field rides every orb's
	// metadata. vanilla: ExperienceOrb.defineSynchedData
	MetadataIndexOrbValue = 8
	// The primed TNT's fuse and block state, at the indices PrimedTnt's
	// defineSynchedData registers; both ride the metadata only away from
	// their defaults (80 ticks, the TNT block).
	MetadataIndexTNTFuse       = 8
	MetadataIndexTNTBlockState = 9
	// The firework rocket's fields past the item stack: the optional body a
	// boost rocket is attached to, and the crossbow's shot-at-angle flag
	// (always false here). vanilla: FireworkRocketEntity.defineSynchedData
	MetadataIndexFireworkAttachedTo  = 9
	MetadataIndexFireworkShotAtAngle = 10
	// MetadataIndexFallingBlockStartPos is the block position the fall
	// began at, default (0, 0, 0).
	MetadataIndexFallingBlockStartPos = 8
	// The hanging entity's own fields, at the indices
	// HangingEntity.defineSynchedData registers: the direction a
	// painting faces, and beneath it the variant it is drawn as.
	MetadataIndexHangingDirection = 8
	MetadataIndexPaintingVariant  = 9
	// The item frame's fields, beneath the hanging direction it shares
	// with the paintings: the item it holds at the index ItemFrame.
	// defineSynchedData adds, and the item's rotation below it.
	MetadataIndexFrameItem     = 9
	MetadataIndexFrameRotation = 10
	// The vehicle shake a hit boat carries: rocking ticks left, rocking
	// direction, and damage, the rocking's multiplier. A fresh body is 0,
	// 1, 0. vanilla: VehicleEntity.defineSynchedData
	MetadataIndexVehicleShake          = 8
	MetadataIndexVehicleShakeDirection = 9
	MetadataIndexVehicleShakeDamage    = 10
	// The minecart's display block, at the indices AbstractMinecart's
	// defineSynchedData registers: the optional custom block state, then
	// the offset it is drawn at.
	MetadataIndexMinecartDisplayBlock  = 11
	MetadataIndexMinecartDisplayOffset = 12
	// MetadataIndexMinecartFurnaceFuel is the furnace minecart's lit flag.
	MetadataIndexMinecartFurnaceFuel = 13
	// The command block minecart's command and its last output, beneath the
	// minecart's fields. vanilla: MinecartCommandBlock.defineSynchedData
	MetadataIndexMinecartCommand    = 13
	MetadataIndexMinecartLastOutput = 14
	// BoatHurtDirDefault is the shake direction a fresh body starts at.
	BoatHurtDirDefault = 1
	// The boat's paddles, at the indices Boat's defineSynchedData
	// registers: the driver's Paddle Boat report is what they carry. The
	// splash timer is the ticks left on a bubble column's surface.
	MetadataIndexBoatLeftPaddle  = 11
	MetadataIndexBoatRightPaddle = 12
	MetadataIndexBoatBubbleTime  = 13
	// The end crystal's own fields: the optional beam target block, then the
	// show-bottom boolean.
	MetadataIndexEndCrystalBeam       = 8
	MetadataIndexEndCrystalShowBottom = 9
	// The armor stand's own fields, at the indices ArmorStand's
	// defineSynchedData registers: the flags byte, then the six body-part
	// rotations, each three floats.
	MetadataIndexArmorStandFlags    = 15
	MetadataIndexArmorStandHead     = 16
	MetadataIndexArmorStandBody     = 17
	MetadataIndexArmorStandLeftArm  = 18
	MetadataIndexArmorStandRightArm = 19
	MetadataIndexArmorStandLeftLeg  = 20
	MetadataIndexArmorStandRightLeg = 21
	// The mannequin's fields: Avatar's main hand and model layers, then its
	// own profile, immovable flag and optional label.
	MetadataIndexAvatarMainHand     = 15
	MetadataIndexAvatarLayers       = 16
	MetadataIndexMannequinProfile   = 17
	MetadataIndexMannequinImmovable = 18
	MetadataIndexMannequinLabel     = 19
	// MetadataEnd terminates a metadata array in place of an index.
	MetadataEnd = 0xFF
)

// Flags of the armor stand's byte at MetadataIndexArmorStandFlags, the
// small, arms, base-plate and marker state of the body.
const (
	ArmorStandFlagSmall       = 0x01
	ArmorStandFlagArms        = 0x04
	ArmorStandFlagNoBasePlate = 0x08
	ArmorStandFlagMarker      = 0x10
)

// EntityFlagArrowCrit is bit 0x01 of the abstract arrow's metadata byte,
// the critical shot the bow's full draw sets; EntityFlagArrowNoClip is
// bit 0x02, the noclip a loyalty trident returns by.
const (
	EntityFlagArrowCrit   = 0x01
	EntityFlagArrowNoClip = 0x02
)

// EntityFlagClimbing is bit 0x01 of the spider's metadata byte, the
// climbing state; no other bit of the byte is used in this version.
const EntityFlagClimbing = 0x01

// Flags of the hand-state field at MetadataIndexHandState, the whole of
// what other clients can see of a held item being used; a player eating
// holds the use on their main hand.
const (
	EntityFlagHandActive    = 0x01
	EntityFlagActiveHandOff = 0x02
	EntityFlagSpinAttack    = 0x04
)

// Flags of the entity metadata field at MetadataIndexEntityFlags.
// EntityFlagSneaking only hides the name tag; the pose is what makes a
// player visibly crouch, and the vanilla server sets both.
const (
	EntityFlagOnFire    = 0x01
	EntityFlagSneaking  = 0x02
	EntityFlagSprinting = 0x08
	EntityFlagSwimming  = 0x10
	EntityFlagInvisible = 0x20
	EntityFlagGlowing   = 0x40
	EntityFlagFlying    = 0x80
)

// EntityEventDrown is the entity event vanilla broadcasts the tick a body's
// breath drowns it, ahead of the blow itself.
const EntityEventDrown = 67

// EntityEventHoneySlide is the entity event a honey block's landing
// broadcasts beside the slide sound. vanilla: HoneyBlock.fallOn
const EntityEventHoneySlide = 54

// EntityEventHoneySlideParticles is the entity event a body sliding down a
// honey block's side may broadcast each tick. vanilla: HoneyBlock.maybeDoSlideEffects
const EntityEventHoneySlideParticles = 53

// Flags of the Mob bit-mask byte at MetadataIndexMobFlags; the aggressive
// bit is the one a hunting monster sets.
const (
	EntityFlagNoAI       = 0x01
	EntityFlagLeftHanded = 0x02
	EntityFlagAggressive = 0x04
)

// Poses, the shape a player is drawn in. Sleeping beats the walking poses
// (vanilla sets it once per sleep, whatever sneak does) and swimming beats
// sneaking, since sneak is held in water too.
const (
	PoseStanding = 0

	// The elytra glide and riptide poses no input of this server's own
	// reaches, kept so the numbering has no gaps on the wire.
	PoseFallFlying = 1

	PoseSleeping   = 2
	PoseSwimming   = 3
	PoseSpinAttack = 4
	PoseSneaking   = 5

	// PoseLongJumping is the goat's leap.
	PoseLongJumping = 6
	// vanilla: Pose.CROAKING, USING_TONGUE, SITTING
	PoseCroaking    = 8
	PoseUsingTongue = 9
	PoseSitting     = 10

	// PoseDying is set on a body that has died.
	PoseDying = 7

	// vanilla: Pose
	// vanilla: Pose.SNIFFING
	PoseRoaring  = 11
	PoseSniffing = 12
	PoseEmerging = 13
	PoseDigging  = 14
	PoseSliding  = 15
	PoseShooting = 16
	PoseInhaling = 17
)

// Equipment slots of the Set Equipment packet. Not the inventory window's
// slots: the main hand is the selected hotbar slot and the armour slots run
// feet-to-head.
const (
	EquipMainHand = 0
	EquipOffHand  = 1
	EquipFeet     = 2
	EquipLegs     = 3
	EquipChest    = 4
	EquipHead     = 5
)

// Game modes.
const (
	GameModeSurvival = iota
	GameModeCreative
	GameModeAdventure
	GameModeSpectator
)

// Window types, as numbered in the minecraft:menu registry, built into the
// client: captured from the vanilla server's data report.
const (
	MenuGeneric9x1   = 0  // a plugin menu's one row; two rows is the next number
	MenuGeneric9x3   = 2  // chests and barrels
	MenuGeneric9x6   = 5  // the double chest
	MenuCrafter      = 7  // the crafter: its 3x3, then the player's things, then the result
	MenuAnvil        = 8  // the anvil: two inputs, then the combined result
	MenuBeacon       = 9  // the beacon: the payment slot, then the player's things
	MenuBlastFurnace = 10 // the blast furnace, furnace-shaped
	MenuBrewingStand = 11
	MenuCrafting     = 12 // the crafting table
	MenuEnchantment  = 13 // the enchanting table
	MenuFurnace      = 14
	MenuGrindstone   = 15 // the grindstone: two inputs, then the combined result
	MenuHopper       = 16
	MenuLectern      = 17 // the lectern: the book alone, no player inventory
	MenuLoom         = 18 // the loom: banner, dye and pattern item, then the result
	MenuMerchant     = 19 // a villager's trades: two payments, the result, then the player's things
	MenuGeneric3x3   = 6  // the dispenser and the dropper
	MenuShulkerBox   = 20
	MenuSmithing     = 21 // the smithing table: template, base and addition, then the result
	MenuSmoker       = 22 // the smoker, furnace-shaped
	MenuCartography  = 23 // the cartography table: map and addition, then the result
	MenuStonecutter  = 24
)

// Block entity types by minecraft:block_entity_type ID, which the client
// keeps to itself; Block Entity Data and Chunk Data name them by these.
const (
	BlockEntityFurnace      = 0
	BlockEntityChest        = 1
	BlockEntityTrappedChest = 2
	BlockEntityEnderChest   = 3
	BlockEntityJukebox      = 4
	BlockEntityDispenser    = 5
	BlockEntityDropper      = 6
	// BlockEntitySign and BlockEntityHangingSign are the standing and wall
	// signs' own and the hanging signs': two entries of one registry, one
	// block entity with either name.
	BlockEntitySign        = 7
	BlockEntityHangingSign = 8
	// BlockEntitySpawner is minecraft:mob_spawner, the monster spawner.
	BlockEntitySpawner          = 9
	BlockEntityCreakingHeart    = 10
	BlockEntityPiston           = 11
	BlockEntityBrewingStand     = 12
	BlockEntityEnchantingTable  = 13
	BlockEntityEndPortal        = 14
	BlockEntityBeacon           = 15
	BlockEntitySkull            = 16
	BlockEntityDaylightDetector = 17
	BlockEntityHopper           = 18
	BlockEntityComparator       = 19
	BlockEntityBanner           = 20
	BlockEntityStructureBlock   = 21
	BlockEntityEndGateway       = 22
	BlockEntityCommandBlock     = 23
	BlockEntityShulkerBox       = 24
	BlockEntityConduit          = 25
	BlockEntityBarrel           = 26
	BlockEntitySmoker           = 27
	BlockEntityBlastFurnace     = 28
	BlockEntityLectern          = 29
	// BlockEntityBell is minecraft:bell, whose entity exists for the block
	// event that swings it to land in.
	BlockEntityBell                  = 30
	BlockEntityJigsaw                = 31
	BlockEntityCampfire              = 32
	BlockEntityBeehive               = 33
	BlockEntitySculkSensor           = 34
	BlockEntityCalibratedSculkSensor = 35
	BlockEntitySculkCatalyst         = 36
	BlockEntitySculkShrieker         = 37
	BlockEntityChiseledBookshelf     = 38
	BlockEntityShelf                 = 39
	BlockEntityBrushableBlock        = 40
	BlockEntityDecoratedPot          = 41
	BlockEntityCrafter               = 42
	// BlockEntityTrialSpawner is minecraft:trial_spawner, the trial
	// chamber's own.
	BlockEntityTrialSpawner      = 43
	BlockEntityVault             = 44
	BlockEntityTestBlock         = 45
	BlockEntityTestInstanceBlock = 46
	BlockEntityCopperGolemStatue = 47
	BlockEntityPotentSulfur      = 48
)

// Block registry IDs, as the Block Action packet carries them: an ID from
// the minecraft:block registry rather than a block state, which the vanilla
// client ignores in favour of its own world.
const (
	BlockChest        = 245
	BlockTrappedChest = 523
	BlockFurnace      = 253
	BlockEnderChest   = 451
	BlockDispenser    = 114
	BlockDropper      = 536
	BlockHopper       = 530
	BlockBarrel       = 929
	BlockBell         = 938
	BlockNoteBlock    = 118
)

// BlockShulkerBoxIDs are the shulker boxes' block IDs for Block Action:
// registered in a row, undyed first, then in dye registry order.
var BlockShulkerBoxIDs = map[string]int32{
	"shulker_box":            735,
	"white_shulker_box":      736,
	"orange_shulker_box":     737,
	"magenta_shulker_box":    738,
	"light_blue_shulker_box": 739,
	"yellow_shulker_box":     740,
	"lime_shulker_box":       741,
	"pink_shulker_box":       742,
	"gray_shulker_box":       743,
	"light_gray_shulker_box": 744,
	"cyan_shulker_box":       745,
	"purple_shulker_box":     746,
	"blue_shulker_box":       747,
	"brown_shulker_box":      748,
	"green_shulker_box":      749,
	"red_shulker_box":        750,
	"black_shulker_box":      751,
}

// Block registry IDs of the copper chests, which the Block Action packet
// carries the same way, keyed by block name; the eight sit in a row in
// oxidation order, unwaxed then waxed, so the IDs follow their state ranges.
var BlockCopperChestIDs = map[string]int32{
	"copper_chest":                 1198,
	"exposed_copper_chest":         1199,
	"weathered_copper_chest":       1200,
	"oxidized_copper_chest":        1201,
	"waxed_copper_chest":           1202,
	"waxed_exposed_copper_chest":   1203,
	"waxed_weathered_copper_chest": 1204,
	"waxed_oxidized_copper_chest":  1205,
}

// Block IDs of the piston family, comparator and observer for Block Action;
// a piston's extend and retract are actions 0 and 1, with the push facing
// as parameter.
const (
	BlockPiston       = 149
	BlockStickyPiston = 138
	BlockPistonHead   = 150
	BlockMovingPiston = 199
	BlockComparator   = 526
	BlockObserver     = 734
)

// The Block Actions a piston runs; the parameter of both is the facing the
// piston pushes along. vanilla: get3DDataValue
const (
	PistonActionExtend  = 0
	PistonActionRetract = 1
)

// ChestActionLid is the lid Block Action, its parameter the viewer count:
// open above zero, closed at zero. Barrels, ender chests and shulker boxes
// use it on their own block IDs.
const ChestActionLid = 1

// Properties of the Set Container Property packet for the furnace window,
// which are what draw the furnace's fire icon and progress arrow.
const (
	FurnacePropertyFuelLeft      = 0 // counting down to 0
	FurnacePropertyFuelTotal     = 1
	FurnacePropertyProgress      = 2 // counting up to the total
	FurnacePropertyProgressTotal = 3
)

// Properties of the Set Container Property packet for the brewing stand
// window: the ticks left of the brew and the fuel power left, then the full
// mark of each. vanilla: BrewingStandBlockEntity.DATA_*
const (
	BrewingPropertyBrewTime  = 0
	BrewingPropertyFuel      = 1
	BrewingPropertyBrewTotal = 2
	BrewingPropertyFuelTotal = 3
)

// Properties of the Set Container Property packet for the crafter window:
// the nine grid slots' disabled states, 1 a disabled slot, then whether a
// redstone pulse has it crafting.
const (
	CrafterPropertySlots     = 0 // 0-8, one per grid cell
	CrafterPropertyTriggered = 9
)

// Set Container Property indices for the enchanting table: the three level
// costs, the seed, and each slot's enchantment and level (-1 hidden). The
// wire has only the seed's low 16 bits, which the client draws its glyphs
// from.
const (
	EnchantPropertyCost0    = 0
	EnchantPropertyCost1    = 1
	EnchantPropertyCost2    = 2
	EnchantPropertySeed     = 3
	EnchantPropertyEnchant0 = 4
	EnchantPropertyEnchant1 = 5
	EnchantPropertyEnchant2 = 6
	EnchantPropertyLevel0   = 7
	EnchantPropertyLevel1   = 8
	EnchantPropertyLevel2   = 9
	EnchantPropertyCount    = 10
)

// Set Container Property indices for the beacon: the pyramid level and the
// primary and secondary effects as registry ID plus one, 0 for none.
// vanilla: BeaconMenu.encodeEffect
const (
	BeaconPropertyLevels    = 0
	BeaconPropertyPrimary   = 1
	BeaconPropertySecondary = 2
	BeaconPropertyCount     = 3
)

// Click modes of Click Container: 0 click, 1 shift, 2 number key, 3 middle,
// 4 drop key, 5 drag, 6 double-click collect. A drag is a burst of
// ClickModeDrag packets whose Button is the stage (0 start, 1 add slot, 2
// end) plus 4 for the right button or 8 for the middle.
const (
	ClickModePickup = iota
	ClickModeQuickMove
	ClickModeSwap
	ClickModeClone
	ClickModeThrow
	ClickModeDrag
	ClickModeCollect
)

// ClickClaimLimit is the most changed slots a Click Container may claim, the
// bound vanilla's codec reads the packet's changed-slots array with.
const ClickClaimLimit = 128

// HashedPatchLimit is the most components a hashed slot's added or removed
// list may carry, each bound vanilla's HashedPatchMap.STREAM_CODEC reads with.
const HashedPatchLimit = 256

// Player ability flags.
const (
	AbilityInvulnerable = 0x01
	AbilityFlying       = 0x02
	AbilityAllowFlying  = 0x04
	AbilityCreative     = 0x08
)

// The bits of a horse's flag byte that this server sets.
const (
	HorseFlagTame      = 0x02
	HorseFlagBred      = 0x08
	HorseFlagEating    = 0x10
	HorseFlagStanding  = 0x20
	HorseFlagMouthOpen = 0x40
)

// The bits of the fox's flag byte.
// vanilla: Fox.getFlag
const (
	FoxSitting   = 0x01
	FoxCrouching = 0x04
	FoxInterest  = 0x08
	FoxPouncing  = 0x10
	FoxSleeping  = 0x20
	FoxFaceplant = 0x40
	FoxDefending = 0x80
)

// The bits of a tameable mob's flag byte, and the entity events its taming
// broadcasts: status 6 the smoke of a failed roll, 7 the hearts of a
// success. vanilla: TamableAnimal.handleEntityEvent
const (
	TamableSitting = 0x01
	TamableTame    = 0x04

	EntityEventTameFailed = 6
	EntityEventTamed      = 7
)

// The entity events an ocelot's trust roll broadcasts: the smoke of a
// failed roll and the hearts of a success.
// vanilla: Ocelot.handleEntityEvent
const (
	EntityEventTrustFailed = 40
	EntityEventTrusted     = 41
)

// Entity events of a horse: the failed and the successful taming's smoke and
// hearts. vanilla: AbstractHorse.handleEntityEvent
const (
	EntityEventHorseTameFailed = 6
	EntityEventHorseTamed      = 7
)

// The actions of the Player Command packet that start and stop a ridden
// horse's jump, and open the ridden mount's screen (the inventory key).
const (
	PlayerCommandStartRidingJump = 3
	PlayerCommandStopRidingJump  = 4
	PlayerCommandOpenInventory   = 5
)

// RegistryLen reports how many entries the synchronized registry of the
// given name holds, data packs' additions included; the registry package
// installs it. Nil, or false, leaves the built-in size.
var RegistryLen func(name string) (int, bool)

// registrySize is the bound for an ID of a synchronized registry: the
// registry's current length, else its built-in size.
func registrySize(name string, builtin int32) int32 {
	if RegistryLen != nil {
		if n, ok := RegistryLen(name); ok {
			return int32(n)
		}
	}
	return builtin
}

// holderVariantRegistry is the registry a component naming one entry by a
// bare VarInt ID indexes, and its built-in size (zero for a component that is
// not one).
// vanilla: ByteBufCodecs.holderRegistry
func holderVariantRegistry(component int32) (string, int32) {
	switch component {
	case DataComponentVillagerVariant:
		return "minecraft:villager_type", 7
	case DataComponentWolfVariant:
		return "minecraft:wolf_variant", 9
	case DataComponentWolfSoundVariant:
		return "minecraft:wolf_sound_variant", 7
	case DataComponentPigVariant:
		return "minecraft:pig_variant", 3
	case DataComponentPigSoundVariant:
		return "minecraft:pig_sound_variant", 3
	case DataComponentCowVariant:
		return "minecraft:cow_variant", 3
	case DataComponentCowSoundVariant:
		return "minecraft:cow_sound_variant", 2
	case DataComponentChickenVariant:
		return "minecraft:chicken_variant", 3
	case DataComponentChickenSoundVariant:
		return "minecraft:chicken_sound_variant", 2
	case DataComponentZombieNautilusVariant:
		return "minecraft:zombie_nautilus_variant", 2
	case DataComponentFrogVariant:
		return "minecraft:frog_variant", 3
	case DataComponentCatVariant:
		return "minecraft:cat_variant", 11
	case DataComponentCatSoundVariant:
		return "minecraft:cat_sound_variant", 2
	}
	return "", 0
}

func sizeBannerPattern() int32 {
	return registrySize("minecraft:banner_pattern", RegistrySizeBannerPattern)
}
func sizePaintingVariant() int32 {
	return registrySize("minecraft:painting_variant", RegistrySizePaintingVariant)
}
func sizeInstrument() int32  { return registrySize("minecraft:instrument", RegistrySizeInstrument) }
func sizeJukeboxSong() int32 { return registrySize("minecraft:jukebox_song", RegistrySizeJukeboxSong) }
func sizeTrimMaterial() int32 {
	return registrySize("minecraft:trim_material", RegistrySizeTrimMaterial)
}
func sizeTrimPattern() int32 { return registrySize("minecraft:trim_pattern", RegistrySizeTrimPattern) }
