package entity

import (
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
)

// A mob collection is the list a world's tick walks, dropping the dead once
// they have reported. UUIDs, dimensions and what each client was told stay
// in the server.

// MobEventKind is something a mob's tick produced that the server may tell
// the world.
type MobEventKind uint8

const (
	// MobLaidEgg: a chicken's time came; the egg is at its feet.
	MobLaidEgg MobEventKind = iota
	// MobChestOpened and MobChestClosed: a copper golem began or ceased to
	// hold the chest at X, Y, Z open, which the server counts as an opener.
	MobChestOpened
	MobChestClosed
	// MobChestChanged: a copper golem took from or put into the chest at
	// X, Y, Z; its viewers are sent the slots.
	MobChestChanged
	// MobTurnedToStatue: an oxidized copper golem became the statue block
	// BlockState at X, Y, Z; the server places it and removes the body.
	MobTurnedToStatue
	// MobShedScute drops an armadillo shedding gift.
	MobShedScute
	// MobDroppedItem drops Stack at X, Y, Z, as Entity.spawnAtLocation does.
	MobDroppedItem
	// MobPandaAte posts the eat game event at the mob.
	MobPandaAte
	// MobSnifferDug is a sniffer's dig reaching its seed, at the head block X, Y, Z.
	MobSnifferDug
	// MobDragonShot carries a dragon fireball's origin and direction.
	MobDragonShot
	// MobDragonFlame places the perched dragon's cloud at X, Y, Z.
	MobDragonFlame
	// MobDragonFlameEnded discards the perched dragon's cloud.
	MobDragonFlameEnded
	// MobDied: the mob's last event; the server rolls the drops (RollDrops).
	MobDied
	// MobDying starts the corpse countdown without LivingEntity.die's kill effects.
	MobDying
	// MobKillingBlow awards death credit without removing the dragon or dropping loot.
	// vanilla: LivingEntity.die, EnderDragon.handleKillingBlow
	MobKillingBlow
	// MobAttackedPlayer: a monster's blow landed on Player. Damage is the raw
	// attack damage; armour, difficulty and death are the server's.
	MobAttackedPlayer
	// MobAxolotlSupported: an axolotl stopped attacking a target Player killed.
	MobAxolotlSupported
	// MobDolphinGrace grants Player 100 ticks of Dolphin's Grace.
	MobDolphinGrace
	// MobBlinded gives Player 400 ticks of Blindness.
	// vanilla: Illusioner.IllusionerBlindnessSpellGoal.performSpellCasting
	MobBlinded
	// MobDolphinTreasure emits Dolphin's happy entity event.
	MobDolphinTreasure
	// MobBurned: sourceless damage (fire, suffocation, drowning, lava).
	// DamageType names it: in_fire, on_fire, in_wall, drown or lava.
	MobBurned
	// MobExtinguished: water or rain put out a burning mob; the server plays
	// the fizz.
	// vanilla: Entity.playEntityOnFireExtinguishedSound
	MobExtinguished
	// MobDismountedUnderwater: the mob's head went under water on a mount that
	// throws its rider; the server unseats it.
	// vanilla: LivingEntity.baseTick
	MobDismountedUnderwater
	// MobMoaned: the idle sound's time came.
	MobMoaned
	// MobBubbleSurface: the mob rode the top of the bubble column at X, Y, Z,
	// where the splash rises.
	// vanilla: Entity.sendBubbleColumnParticles
	MobBubbleSurface
	// MobPrimed: a creeper's fuse started from zero; the server plays
	// entity.creeper.primed in the hostile category.
	MobPrimed
	// MobExploded: a creeper's fuse reached max. X, Y, Z and Radius are the
	// Level.explode feet position and radius. The mob is discarded with the
	// event (explodeCreeper): no corpse, death sound or drops. Blocks and
	// players are the server's.
	MobExploded
	// MobVanished: a despawn removed the mob: no death animation, sound or
	// drops.
	MobVanished
	// MobShotArrow: a skeleton released an arrow. X, Y, Z is the spawn point
	// (the eye less 0.1), VX, VY, VZ the velocity with aim and inaccuracy,
	// Damage its base damage and Effect its tip. Pitch is the shoot sound's (not
	// set for a crossbow bolt). The server creates the arrow.
	MobShotArrow
	// MobThrewTrident carries the origin, velocity and shoot sound pitch.
	MobThrewTrident
	// MobThrewPotion: a witch threw a splash Potion from X, Y, Z at VX, VY, VZ;
	// Pitch is the throw sound's. The server creates the projectile.
	// vanilla: Witch.performRangedAttack
	MobThrewPotion
	// MobWitchDrank: a witch began to drink; Pitch is the drink sound's.
	MobWitchDrank
	// MobWitchDrankUp: a witch finished drinking and its effects applied.
	// vanilla: Witch.aiStep
	MobWitchDrankUp
	// MobWitchSparkled: a witch's rare particle burst, entity event 15.
	MobWitchSparkled
	// MobConverted: a body converted into Into, including a cured zombie villager.
	// The mob is gone with the event; the server creates the new body.
	MobConverted
	// MobJumped: a cube mob's hop began; Pitch is the hop sound's.
	MobJumped
	// MobHopped: a rabbit's hop left the ground; the server broadcasts entity
	// event 1.
	// vanilla: Rabbit.jumpFromGround
	MobHopped
	// MobRaidedCrop: a rabbit ate the carrots at X, Y, Z (one stage, or the
	// plant at age 0); the server changes the block if mob_griefing allows.
	// vanilla: Rabbit.RaidGardenGoal.tick
	MobRaidedCrop
	// MobPickedBerries: a fox reached the berries at X, Y, Z; the server picks
	// them if mob_griefing allows.
	// vanilla: Fox.FoxEatBerriesGoal.onReachedTarget
	MobPickedBerries
	// MobSquished: a cube mob landed; Pitch is the squish sound's.
	MobSquished
	// MobLanded: a sourceless fall past the safe three hurt the body. Damage is
	// the floored damage, which picks the big or small fall sound. A horse also
	// raises it unhurt: Damage 0 and Fall the distance.
	MobLanded
	// MobLandingDust: a landing past the safe distance bursts into block
	// particles of BlockState at X, Y, Z, Count of them, hurt or not.
	// vanilla: LivingEntity.checkFallDamage
	MobLandingDust
	// MobBounced: the landing bounced off the block at X, Y, Z, whose bounce
	// Sound the server plays at the block's centre.
	// vanilla: ShelfMushroomBlock.bounceOn
	MobBounced
	// MobBeganEating: grazing started; entity event 10 plays the eating
	// animation.
	MobBeganEating
	// MobAte: the grazing bite at X, Y, Z (the block below, or the feet cell for
	// a plant). The server changes the block unless mob_griefing vetoes; the
	// sheep has eaten either way.
	MobAte
	// MobBred: a courtship ended in breeding. Partner is the other parent,
	// Amount the experience (finalizeSpawnChildFromBreeding's 1-7). The parents
	// are already settled; the server creates the child, hearts and orb.
	MobBred
	// MobArmourBroke: the sun wore through the head piece and the slot is
	// cleared; the server broadcasts the head's equipment break.
	MobArmourBroke
	// MobBodyBroke: the sun wore through a zombie horse's body armour.
	MobBodyBroke
	// MobEffectChanged: a status effect was added, updated or removed, for the
	// trackers' metadata particles.
	MobEffectChanged
	// MobTotemSaved: a held totem spent itself against a lethal blow (entity
	// event 35). Its effects come as MobEffectChanged.
	MobTotemSaved
	// MobLeashDropped: the lead came off (holder too far, dead or gone, or the
	// knot destroyed). The lead drops as an item at the mob's feet; Broke means
	// it snapped, heard at the holder's X, Y, Z. HolderID is the holder, for a
	// knot's last-mob bookkeeping.
	MobLeashDropped
	// MobTentacleReset: a squid's tentacle cycle wrapped (entity event 19).
	// vanilla: Squid.aiStep's broadcastEntityEvent
	MobTentacleReset
	// MobPassengersChanged: the vehicle Mob's passengers changed (boarded,
	// left, or put off at death) and must be re-sent.
	MobPassengersChanged
	// MobPhantomSwooped: a phantom began a swoop; Pitch is the swoop
	// sound's. vanilla: PhantomAttackStrategyGoal.tick
	MobPhantomSwooped
	// MobPhantomBit: a phantom's bite landed, the bite level event.
	// vanilla: PhantomSweepAttackGoal.tick
	MobPhantomBit
	// MobFlopped: a fish on dry ground flopped; the server plays its flop
	// sound. vanilla: AbstractFish.aiStep
	MobFlopped
	// MobSwam: a fish in water moved a block's worth of way; the server plays
	// its swim sound at Volume and Pitch. vanilla: Entity.waterSwimSound
	MobSwam
	// MobBatTookOff: a hanging bat woke; the server plays level event 1025
	// at its cell. vanilla: Bat.customServerAiStep
	MobBatTookOff
	// MobMeltedSnow: a burning mob was extinguished in the powder snow at
	// X, Y, Z; the server removes it if mob_griefing allows.
	// vanilla: PowderSnowBlock.entityInside
	MobMeltedSnow
	// MobDousedCauldron: a burning mob was extinguished in the layered cauldron
	// at X, Y, Z; the server lowers it a level.
	// vanilla: LayeredCauldronBlock.entityInside
	MobDousedCauldron
	// MobPackLocated: a worn stack carries an enchantment with a location_changed
	// or tick component, or an entry is active; Level is the LocatedMoved and
	// piece bits that say which entries to run. The server runs them.
	// vanilla: EnchantmentHelper.runLocationChangedEffects, tickEffects
	MobPackLocated
	// MobSteppedOnSculk: a mob stands on the sculk sensor at X, Y, Z (cell
	// coordinates); the server schedules its step there.
	// vanilla: SculkSensorBlock.stepOn
	MobSteppedOnSculk
	// MobSteppedOnRedstoneOre: a mob stands on the unlit redstone ore at X,
	// Y, Z (cell coordinates), which the server lights.
	// vanilla: RedStoneOreBlock.stepOn
	MobSteppedOnRedstoneOre
	// MobStepped: the body stepped on BlockState, the supporting block; the
	// server posts a step game event. vanilla: Entity.vibrationAndSoundEffectsFromBlock
	MobStepped
	// MobSplashed: the body first touched water; the server plays SoundID at
	// Volume and Pitch and posts a splash game event.
	// vanilla: Entity.doWaterSplashEffect
	MobSplashed
	// MobFlapped: the body flapped; the server posts a flap game event.
	// vanilla: Entity.processFlappingMovement
	MobFlapped
	// MobBouncedOff: the body bounced; the server posts a bounce game event.
	// vanilla: Entity.restituteMovementAfterCollisions
	MobBouncedOff
	// MobSwamStride: the body swam a stride; the server posts a swim game
	// event. vanilla: Entity.applyMovementEmissionAndPlaySound
	MobSwamStride
	// MobLaidSnow: a snow golem's trail wants a snow layer at X, Y, Z, placed if
	// mob_griefing allows.
	// vanilla: SnowGolem.aiStep
	MobLaidSnow
	// MobWolfShook: a wet wolf began shaking. The server broadcasts entity
	// event 8.
	// vanilla: Wolf.aiStep
	MobWolfShook
	// MobWolfShakeSound: a shaking wolf's first dry tick; the server plays the
	// shake sound at Pitch.
	// vanilla: Wolf.tick
	MobWolfShakeSound
	// MobWolfShakeCancelled: a shaking wolf got wet again. The server
	// broadcasts entity event 56.
	// vanilla: Wolf.tick
	MobWolfShakeCancelled
	// MobWolfArmorCracked: a blow crossed a crack threshold of the wolf armour;
	// the server plays the crack sound and scute particles.
	// vanilla: Wolf.actuallyHurt
	MobWolfArmorCracked
	// MobWolfArmorBroke: a blow broke the wolf armour; the server broadcasts the
	// body break event and sound.
	// vanilla: LivingEntity.onEquippedItemBroken
	MobWolfArmorBroke
	// MobCalledReinforcement: a hurt zombie on hard rolled a call. Target is the
	// entity the new body hunts and Seed the site search's dice; the server
	// answers with ReinforcementSite and charges the caller only if a site was
	// found.
	// vanilla: Zombie.hurtServer
	MobCalledReinforcement
	// MobGrewUp: a baby turtle grew up; the server drops the turtle_grow loot.
	// vanilla: Turtle.ageBoundaryReached
	MobGrewUp
	// MobLaidTurtleEgg: a turtle laid; X, Y, Z is the air cell above the sand,
	// Amount the 1-4 eggs and Pitch the sound's.
	// vanilla: Turtle.TurtleLayEggGoal.tick
	MobLaidTurtleEgg
	// MobStompingEgg: a zombie stomping the egg at X, Y, Z; Amount 1 asks for
	// the egg's item particles, a nonzero Pitch for the destroy_egg sound.
	// vanilla: RemoveBlockGoal.tick
	MobStompingEgg
	// MobStompedEgg: the stomp finished; the server removes the egg at X, Y, Z
	// with the poof particles and the break sound at Pitch.
	// vanilla: RemoveBlockGoal.tick
	MobStompedEgg
	// MobLaidFrogspawn places frogspawn at X, Y, Z.
	MobLaidFrogspawn
	// MobDugSand: a laying turtle's dig, the block-break level event of the sand
	// at X, Y, Z.
	// vanilla: Turtle.aiStep
	MobDugSand
	// MobWokeInfested: a silverfish calls friends out of the infested block at
	// X, Y, Z, which breaks (or reverts to its host with mob_griefing off).
	// vanilla: Silverfish.SilverfishWakeUpFriendsGoal.tick
	MobWokeInfested
	// MobMergedWithStone: a silverfish entered the host block at X, Y, Z; the
	// server infests it and the mob is discarded.
	// vanilla: Silverfish.SilverfishMergeWithStoneGoal.start
	MobMergedWithStone
	// MobInfestedRelease: the infested effect lets a silverfish out at X, Y, Z
	// with velocity VX, VY, VZ.
	// vanilla: InfestedMobEffect.spawnSilverfish
	MobInfestedRelease
	// MobHorseTamed: riding wore a wild horse down; Horse.Owner is the
	// rider. The server plays the hearts.
	// vanilla: AbstractHorse.tameWithName
	MobHorseTamed
	// MobSkeletonTrapSprung: a player came near a trap horse; the server
	// strikes it and seats the riders.
	// vanilla: SkeletonTrapGoal.tick
	MobSkeletonTrapSprung
	// MobHorseThrewRider: a wild horse bucked its rider; the server dismounts
	// it and plays the angry sound and smoke.
	// vanilla: RunAroundLikeCrazyGoal.tick
	MobHorseThrewRider
	// MobTeleported: an enderman teleported from X, Y, Z, where the first sound
	// plays, to VX, VY, VZ.
	// vanilla: EnderMan.teleport
	MobTeleported
	// MobTookBlock: an enderman picked up the block at X, Y, Z, which the
	// server removes.
	// vanilla: EnderMan.EndermanTakeBlockGoal.tick
	MobTookBlock
	// MobPlacedBlock: an enderman put its carried BlockState down at X, Y, Z.
	// vanilla: EnderMan.EndermanLeaveBlockGoal.tick
	MobPlacedBlock
	// MobGuardianCharged: a guardian's beam started charging; the server plays
	// the attack sound with entity event 21.
	// vanilla: Guardian.GuardianAttackGoal.tick
	MobGuardianCharged
	// MobGuardianBeam: the beam finished charging at Player. Magic is the beam's
	// damage; Damage the guardian's blow that follows.
	// vanilla: Guardian.GuardianAttackGoal.tick
	MobGuardianBeam
	// MobElderCurse: an elder guardian's curse is due at X, Y, Z.
	// vanilla: ElderGuardian.customServerAiStep
	MobElderCurse
	// MobCreakingHurt: a player's blow swayed a heart-bound creaking, whose
	// heart answers with particles.
	// vanilla: Creaking.hurtServer
	MobCreakingHurt
	// MobShotFireball: a blaze fired a small fireball from X, Y, Z aimed VX,
	// VY, VZ; the server creates it.
	// vanilla: Blaze.BlazeAttackGoal.tick
	MobShotFireball
	// MobShotBreezeCharge launches a breeze wind charge with velocity VX, VY, VZ.
	MobShotBreezeCharge
	// MobShotLargeFireball: a ghast fired a large fireball from X, Y, Z aimed
	// VX, VY, VZ, with explosion power Radius.
	// vanilla: Ghast.GhastShootFireballGoal.tick
	MobShotLargeFireball
	// MobSpat: a llama spat from X, Y, Z along VX, VY, VZ, its sound at Pitch;
	// the server creates the spit.
	// vanilla: Llama.spit
	MobSpat
	// MobThrewSnowball: a snow golem threw a snowball from X, Y, Z along VX,
	// VY, VZ, its sound at Pitch; the server creates the snowball.
	// vanilla: SnowGolem.performRangedAttack
	MobThrewSnowball
	// MobGolemCracked: a blow moved an iron golem to another crack level.
	// vanilla: IronGolem.hurtServer
	MobGolemCracked
	// MobGhastWarned: a ghast's charge reached ten ticks; the server plays the
	// warning.
	MobGhastWarned
	// MobOffersChanged: a villager's restock or demand catch-up rewrote the
	// offers a player has open.
	// vanilla: Villager.resendOffersToTradingPlayer
	MobOffersChanged
	// MobWarned: a polar bear on its hind legs growls at its target.
	// vanilla: PolarBear.playWarningSound
	MobWarned
	// MobGoatLoweredHead and MobGoatRaisedHead: entity events 58 and 59 for the
	// ram's run-up and finish.
	// vanilla: PrepareRamNearestTarget.tick, RamTarget.finishRam
	MobGoatLoweredHead
	MobGoatRaisedHead
	// MobGoatSounded: a goat's sound event SoundID, played at Pitch.
	MobGoatSounded
	// MobGoatRammedStand: a goat rammed the armor stand Player.EntityID names;
	// it is shoved RamPush along RamYaw and takes no damage.
	MobGoatRammedStand
	// MobGoatDroppedHorn: a horn snapped off at X, Y, Z with velocity VX, VY,
	// VZ; the goat's horn flags are already updated.
	// vanilla: Goat.dropHorn
	MobGoatDroppedHorn
	// MobSounded: SoundID or SoundHolder, played at Volume and Pitch.
	MobSounded
	// MobEntityAction emits the entity_action game event at the mob.
	MobEntityAction
	// MobStatus: entity event Status on the mob.
	MobStatus
	// MobFoxFaceplantDust: break particles of block state Status at X, Y, Z
	// around a faceplanted fox.
	// vanilla: Fox.tick
	MobFoxFaceplantDust
	// MobShulkerShot: a shulker fired at Player from X, Y, Z; Status is its
	// attach face's axis (0 x, 1 y, 2 z) and Seed the bullet's random source.
	// vanilla: Shulker.ShulkerAttackGoal.tick
	MobShulkerShot
	// MobShulkerCopied: a bullet-hit shulker teleported and left a new one at
	// X, Y, Z, the cell it left, dyed Status (a DyeColor id, 16 for none).
	// vanilla: Shulker.hitByShulkerBullet
	MobShulkerCopied
	// MobShulkerShell: the shell's raw opening became Status; 0 is shut. The
	// sound and the container game event follow.
	// vanilla: Shulker.setRawPeekAmount
	MobShulkerShell
	// MobShulkerTeleported: the shulker left X, Y, Z for VX, VY, VZ. The
	// sound plays and the game event is posted at the old place.
	// vanilla: Shulker.teleportSomewhere
	MobShulkerTeleported
	// MobShulkerShoved: the growing shell swept Slab, to move every body but a
	// mob (which the step moved) by VX, VY, VZ.
	// vanilla: Shulker.onPeekAmountChange
	MobShulkerShoved
	// MobEnteredHive: a bee entered the hive at X, Y, Z; the server stores it
	// and removes the body.
	// vanilla: BeehiveBlockEntity.addOccupant
	MobEnteredHive
	// MobGrewCrop: a bee grew the crop at X, Y, Z to BlockState.
	// vanilla: Bee.BeeGrowCropGoal
	MobGrewCrop
	// MobFarm: apply Farm at X, Y, Z, rechecking the block and inventory.
	MobFarm
	// MobThrewItem: a piglin threw Stack from X, Y, Z with the velocity VX,
	// VY, VZ.
	// vanilla: BehaviorUtils.throwItem
	MobThrewItem
	// MobSwungOffHand: the mob swings its off hand.
	// vanilla: PiglinAi.throwItemsTowardPos
	MobSwungOffHand
	// MobSeated: the passengers of Mob (a vehicle) changed; the server sends
	// Set Passengers.
	// vanilla: Entity.startRiding, stopRiding
	MobSeated
	// MobMounted and MobDismounted post the rider Mob's event at vehicle X, Y, Z.
	MobMounted
	MobDismounted
	// MobRemovedBlock removes a cell without drops.
	MobRemovedBlock
	// MobLevelEvent sends Status as a level event at X, Y, Z.
	MobLevelEvent

	// MobDragonDeath carries the death animation tick in Status.
	MobDragonDeath
	// MobBrokeBlock: the mob breaks the block at X, Y, Z, drops and all.
	// vanilla: Level.destroyBlock
	MobBrokeBlock
	// MobActed: the mob posts an entity_action game event at X, Y, Z.
	// vanilla: Ravager.roar
	MobActed
	// MobGaveGift: a cat dropped Stack at X, Y, Z with the velocity VX, VY,
	// VZ, for the owner it slept beside.
	// vanilla: Cat.CatRelaxOnOwnerGoal.giveMorningGift
	MobGaveGift
	// MobWantsGolem: enough villagers want an iron golem near the mob, which
	// the server tries to spawn.
	// vanilla: Villager.spawnGolemIfNeeded
	MobWantsGolem
	// MobRangBell rings the bell at X, Y, Z.
	MobRangBell
	// MobCelebrated launches Stack at X, Y, Z.
	MobCelebrated
	// MobCastFangs: evoker fangs at X, Y, Z facing Radius radians, biting after
	// Amount ticks.
	// vanilla: Evoker.EvokerAttackSpellGoal.createSpellEntity
	MobCastFangs
	// MobSummonedVex: an evoker summons a vex bound to X, Y, Z, living Amount
	// ticks.
	// vanilla: Evoker.EvokerSummonSpellGoal.performSpellCasting
	MobSummonedVex
	// MobWardenDarkness: the warden refreshes darkness within twenty blocks.
	MobWardenDarkness
	// MobWardenVibration: the warden's listener scheduled or reloaded the
	// vibration particle in Vibration.
	MobWardenVibration
	// MobContactHurt: a cube's contact damage on Player, Damage of DamageType,
	// caused by the cube.
	MobContactHurt
	// MobShotWitherSkull: a wither head shot a skull from X, Y, Z along VX, VY,
	// VZ; Amount is 1 for a dangerous one. The server creates it.
	// vanilla: WitherBoss.performRangedAttack
	MobShotWitherSkull
	// MobWitherExploded: the summoning ended; the blast of radius Radius is at
	// X, Y, Z and the world hears level event 1023.
	// vanilla: WitherBoss.customServerAiStep
	MobWitherExploded
	// MobWitherSmashed: the wither's block-breaking pass over the cells within
	// Radius of the block at X, Y, Z and Amount above it.
	// vanilla: WitherBoss.customServerAiStep
	MobWitherSmashed
	// MobWitherBar: a wither's tick, which the server shows its boss bar by.
	MobWitherBar
	// MobLandedOnShoulder: a tame parrot reached Player's shoulder; the server
	// saves it onto the player and removes it.
	// vanilla: ShoulderRidingEntity.setEntityOnShoulder
	MobLandedOnShoulder
	// MobSparkled: one particle of type Status at X, Y, Z, with no spread and
	// max speeds VX, VY, VZ; Alternative picks the ALTERNATIVE randomization.
	MobSparkled
)

// MobEvent is one event from a mob's tick; Mob is the mob it happened to.
// Each kind's doc names the fields it uses.
type MobEvent struct {
	Mob    *Mob
	Kind   MobEventKind
	Damage float64
	// Stab rides the MobAttackedPlayer of a kinetic weapon's contact.
	Stab *SpearStab
	// Hoglin rides the MobAttackedPlayer of a hoglin or zoglin.
	Hoglin *HoglinBlow
	// Magic rides a MobGuardianBeam.
	Magic float64
	// DamageType is the minecraft:damage_type of a fire, fall or killing blow.
	DamageType registry.DamageType
	Player     Player

	X, Y, Z float64
	Radius  float64
	Level   int32

	VX, VY, VZ float64
	// Slab rides a MobShulkerShoved: the swept box's minX, minY, minZ, maxX,
	// maxY, maxZ.
	Slab   [6]float64
	Effect ArrowEffect
	// Crossbow marks a MobShotArrow as a crossbow bolt.
	Crossbow bool
	// Fire marks a MobExploded blast that sets the cleared ground alight.
	Fire bool
	// Stack is the item in MobThrewItem or the crossbow that fired MobShotArrow.
	Stack protocol.ItemStack
	// Ammo is the projectile a crossbow's MobShotArrow fires; nil for any
	// other shot.
	Ammo *protocol.ItemStack
	// Owned marks a MobThrewItem whose item records the mob as its thrower.
	// vanilla: BehaviorUtils.throwItem, Dolphin.PlayWithItemsGoal
	Owned bool
	// Liked is the player an allay's delivery throw is for, uuid.Nil for
	// none, and DropOn the block under the throw's target, which
	// allay_drop_item_on_block tests. vanilla: AllayAi.throwItem
	Liked  uuid.UUID
	DropOn [3]int32
	// Potion rides a MobThrewPotion, by its minecraft:potion name.
	Potion string

	// Alternative rides a MobSparkled.
	Alternative bool

	// Touch marks a MobAttackedPlayer that was a cube's touch, not a swing.
	Touch bool
	// DragonContact pushes before dealing a dragon contact attack.
	DragonContact bool
	ContactPush   [3]float64
	// Roar marks a MobAttackedPlayer that was a ravager's roar, which neither
	// swings nor carries the ravager's attack knockback.
	Roar bool
	// Sonic is a warden ray, which bypasses the melee pipeline.
	Sonic *SonicBoom
	// Vibration rides a MobWardenVibration.
	Vibration *WardenVibrationParticle
	// Ram marks a goat's ram or nautilus charge on Player.EntityID (a
	// player or mob), pushed RamPush along RamYaw after the blow.
	// vanilla: RamTarget.tick, ChargeAttack.tick
	Ram     bool
	RamYaw  float64
	RamPush float64
	// Puff rides a pufferfish's MobAttackedPlayer: its puff state, whose sting
	// poisons the victim for 60 ticks a state.
	Puff int
	// SoundHolder retains a component sound, including its inline fixed range.
	SoundHolder *protocol.SoundHolder
	// SoundID carries registered events without a name lookup.
	SoundID registry.Sound
	Pitch   float32
	// Centred rides MobSounded for a level sound played at the centre of the
	// mob's block (Level.playSound with a BlockPos).
	Centred bool
	// Unmuted rides MobSounded for level sounds that bypass the body's Silent flag.
	Unmuted bool
	// Volume rides a MobSwam, a MobSounded and a MobGoatSounded (whose zero is 1).
	Volume float32
	// Status rides a MobStatus.
	Status int32
	Into   MobType
	// Infection is the zombie whose MobDied victim converts instead of dying.
	Infection *Mob
	// BlockState rides a MobPlacedBlock.
	BlockState uint16
	// Farm rides a MobFarm.
	Farm FarmAction
	// Target and Seed ride a MobCalledReinforcement.
	Target int32
	Seed   int64

	// EffectChange is a MobEffectChanged's change.
	EffectChange effect.Change

	// Softened marks a MobLanded or MobBurned that only landed its excess
	// through the window: the damage event and hurt sound stay back, but fall
	// sounds and the drowning bubbles still play.
	Softened bool

	// NoBubble marks a drowning MobBurned without the bubble burst: a water
	// creature's breath out of water.
	// vanilla: AgeableWaterCreature.handleAirSupply
	NoBubble bool

	// Count rides a MobLandingDust.
	Count int32

	// Fall rides a MobLanded, the distance the landing settled.
	Fall float64

	// AfterTickDeath marks a MobDied from aiStep, after this tick's tickDeath:
	// its corpse countdown starts next tick, so removal lands 20 ticks after
	// the event, where a death before tickDeath (baseTick bites) or between
	// ticks (an attack, /kill) counts 19.
	AfterTickDeath bool

	// Partner is a MobBred's other parent; Amount its experience.
	Partner *Mob
	Amount  int

	// HolderID is a MobLeashDropped's holder; Broke marks a snap, heard at the
	// holder's X, Y, Z.
	HolderID int32
	Broke    bool
	// Untracked marks a MobLeashDropped whose body watchers already lost, so
	// no link release is sent. vanilla: CopperGolem.turnToStatue
	Untracked bool

	// MobX, MobY and MobZ are where a MobLeashDropped lead is dropped.
	MobX, MobY, MobZ float64
}

// Mobs is a world's mobs in the order added, ticked front to back.
type Mobs []*Mob

// Tick steps every mob once and gathers the events. Each mob sees the whole
// herd (for breeding and parent goals); pushing is served from a grid built
// this tick, so a body checks only its neighbourhood. Leashed mobs resolve
// holders from the holders snapshot. A dead or despawned mob reports and
// leaves the collection at once, so it never ticks twice.
func (mobs *Mobs) Tick(w World, players []Player, holders []LeashHolder) []MobEvent {
	return mobs.tickBuf(w, players, holders, nil, nil, nil, nil, nil, nil)
}

// TickInto appends events to buf[:0] and reuses paths across ticks. crowd,
// if not nil, is the boats and minecarts the mobs' cramming counts.
func (mobs *Mobs) TickInto(buf []MobEvent, w World, players []Player, holders []LeashHolder, crowd *PushCrowd, stands []StandTarget, corpses []*Mob, paths *MobPathScratch) []MobEvent {
	return mobs.tickBuf(w, players, holders, nil, buf[:0], crowd, stands, corpses, paths)
}

// tick is Tick; a non-nil gathered counts the push grid's candidates.
func (mobs *Mobs) tick(w World, players []Player, holders []LeashHolder, gathered *int) []MobEvent {
	return mobs.tickBuf(w, players, holders, gathered, nil, nil, nil, nil, nil)
}

// tickBuf is tick appending to events.
func (mobs *Mobs) tickBuf(w World, players []Player, holders []LeashHolder, gathered *int, events []MobEvent, crowd *PushCrowd, stands []StandTarget, corpses []*Mob, paths *MobPathScratch) []MobEvent {
	// A snapshot, not the live slice: the compaction below writes into the
	// backing array the loop reads, and a push pass would see it half
	// rebuilt.
	others := make([]*Mob, len(*mobs))
	copy(others, *mobs)
	// One push grid per tick over the snapshot. A body that moves to another
	// cell is re-filed right after its tick, so later queries find it.
	grid := takePushGrid(len(others), gathered)
	grid.crowd = crowd
	defer grid.release()
	grid.build(others)
	var prey, villagers []*Mob
	for _, o := range others {
		if isPrey(o.Type) {
			prey = append(prey, o)
		}
		if o.Type == TypeVillager {
			villagers = append(villagers, o)
		}
	}
	t := herdTick{w: w, players: players, holders: holders, others: others, grid: grid, prey: prey, villagers: villagers, stands: stands, corpses: corpses, paths: paths, events: events}
	for _, m := range others {
		if m.Vehicle == nil {
			// A rider on a mob ticks after its vehicle.
			// vanilla: ServerLevel.tickNonPassenger
			t.tick(m)
		}
	}
	// A vehicle may have left the herd without releasing its riders. Reach
	// those bodies before compaction clears the marks used by recursion.
	for _, m := range others {
		if !m.herdTicked {
			t.tick(m)
		}
	}
	alive := (*mobs)[:0]
	for _, m := range others {
		if m.herdKept {
			alive = append(alive, m)
		}
		m.herdTicked, m.herdKept = false, false
	}
	*mobs = alive
	return t.events
}

// herdTick is one Mobs.tickBuf pass.
type herdTick struct {
	w         World
	players   []Player
	holders   []LeashHolder
	others    []*Mob
	grid      *pushGrid
	prey      []*Mob
	villagers []*Mob
	stands    []StandTarget
	corpses   []*Mob
	paths     *MobPathScratch
	tv        tickView
	events    []MobEvent
}

// tick steps m, then its passengers, and records whether the collection
// keeps m. A rider is seated right after its own tick, before its own
// riders tick.
// vanilla: ServerLevel.tickNonPassenger, ServerLevel.tickPassenger, Entity.rideTick
func (t *herdTick) tick(m *Mob) {
	if m.herdTicked {
		return
	}
	m.herdTicked = true
	if m.Dormant {
		// checkDespawn runs before the entity-ticking range test.
		// vanilla: ServerLevel.tick
		m.herdKept = true
		if m.despawnStep(t.w, t.players) {
			m.gone, m.herdKept = true, false
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobVanished})
			t.events = append(t.events, m.endSeats()...)
		}
		return
	}
	if m.LeashHolderID != 0 {
		// Resolve the holder from this tick's list; one that is missing, dead
		// or in another dimension is no holder.
		m.leash = nil
		for i := range t.holders {
			if t.holders[i].EntityID == m.LeashHolderID {
				m.leash = &t.holders[i]
				break
			}
		}
	}
	// The grid is set for this body's tick only and cleared after, so
	// an early return or a later tick reads no stale grid.
	m.push, m.prey, m.preyKnown, m.villagers, m.villagersKnown, m.stands, m.corpses = t.grid, t.prey, true, t.villagers, true, t.stands, t.corpses
	m.tickPaths = t.paths
	firstEvent := len(t.events)
	w := t.w
	if colliders, ok := w.(interface{ ForMob(*Mob) World }); ok {
		w = colliders.ForMob(m)
	}
	t.events = m.tickInto(t.events, &t.tv, w, t.players, t.others)
	m.tickPaths = nil
	m.push, m.prey, m.preyKnown, m.villagers, m.villagersKnown, m.stands, m.corpses = nil, nil, false, nil, false, nil, nil
	t.grid.moved(m)
	m.herdKept = !m.Dead && !m.gone
	if !m.herdKept {
		riders := append([]*Mob(nil), m.Passengers...)
		// SINGLE conversions transfer these links when their replacement is
		// registered; ending them here would lose the vehicle and first rider.
		if !t.converting(m, t.events[firstEvent:]) {
			t.events = append(t.events, m.endSeats()...)
		}
		for _, p := range riders {
			t.tick(p)
		}
		return
	}
	if v := m.Vehicle; v != nil {
		v.seatPassenger(m)
		t.grid.moved(m)
	}
	t.tickRiders(m)
}

// converting marks an infected villager's death before its seats can end.
// vanilla: Zombie.killedEntity, ConversionType.SINGLE.convert
func (t *herdTick) converting(m *Mob, events []MobEvent) bool {
	for i := range events {
		ev := &events[i]
		if ev.Kind == MobConverted {
			return true
		}
		if ev.Kind != MobDied || m.Type != TypeVillager {
			continue
		}
		for _, killer := range t.others {
			if killer.ID == m.BlowAttackerID() && killer.Infects(m, t.w.Difficulty()) {
				ev.Infection = killer
				return true
			}
		}
	}
	return false
}

// tickRiders ticks m's passengers in order. A rider's tick may end its
// seat, so the list is copied.
func (t *herdTick) tickRiders(m *Mob) {
	if len(m.Passengers) == 0 {
		return
	}
	for _, p := range append([]*Mob(nil), m.Passengers...) {
		t.tick(p)
	}
}
