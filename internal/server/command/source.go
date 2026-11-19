package command

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/commandsyntax"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/metrics"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// EntityQuery is what a selector can rule out before an entity is
// snapshotted. The zero value keeps every entity.
// vanilla: ServerLevel.getEntities(EntityTypeTest, AABB, ...)
type EntityQuery struct {
	Type      string      // an entity type's name; "" for any
	Dimension string      // a dimension's name; "" for every dimension
	Box       *[6]float64 // min x, y, z then max x, y, z; nil for anywhere
	UUID      *uuid.UUID  // the one entity asked for; nil for any
	Tags      []TagFilter // every one must pass

	// Limit bounds results passing Accept (nil accepts all); 0 is unlimited.
	// Results have the lowest entity IDs first.
	Limit  int
	Accept func(Entity) bool

	// IncludeDead lets a direct UUID lookup reach a dying mob that is no
	// longer in the live-herd index.
	IncludeDead bool
}

// TagFilter is one tag option: Name "" asks for a body carrying no tags,
// any other Name for a body holding it; Invert flips either.
type TagFilter = commandsyntax.TagFilter

// RejectsTags reports whether a body with the tags fails one of the query's
// tag filters. A nil query rejects nothing.
func (q *EntityQuery) RejectsTags(size int, contains func(string) bool) bool {
	if q == nil {
		return false
	}
	for _, f := range q.Tags {
		if !f.Passes(size, contains) {
			return true
		}
	}
	return false
}

// Rejects reports whether an entity of the type, in the dimension, with the
// box and the identity cannot match. A nil query rejects nothing. The box
// test is Selector.predicate's, strict on every side.
func (q *EntityQuery) Rejects(typ, dim string, box [6]float64, id uuid.UUID) bool {
	if q == nil {
		return false
	}
	if q.Type != "" && typ != q.Type || q.Dimension != "" && dim != q.Dimension {
		return true
	}
	if q.UUID != nil && id != *q.UUID {
		return true
	}
	if b := q.Box; b != nil {
		return !(box[3] > b[0] && box[0] < b[3] && box[4] > b[1] && box[1] < b[4] && box[5] > b[2] && box[2] < b[5])
	}
	return false
}

// ItemFilter reports whether a stack is one the command asks for; nil means
// every stack.
type ItemFilter func(stack protocol.ItemStack) bool

// RandomSequence is one of the server's named random sequences; a /random
// draw advances it for the next drawer.
type RandomSequence interface {
	// IntBetween draws a number between lo and hi, both ends included.
	IntBetween(lo, hi int32) int32
}

// Context is what an execution's modifiers settled on: the world position,
// dimension, the @s body and the anchor.
type Context struct {
	// Pos is the position the tail's relative arguments resolve against.
	Pos player.Position
	// HasPos says whether a modifier has settled the position at all;
	// without one the tail reads the runner's own.
	HasPos bool
	// Dim is the wire name of the dimension the tail's block and mob business happens in.
	Dim string
	// HasDim says whether a modifier has settled the dimension at all.
	HasDim bool
	// Executor is the body the tail's @s names.
	Executor Entity
	// Eyes says the anchor is the eyes rather than the feet, which is where a facing aims from.
	Eyes bool
	// Quiet says the chain has forked its way here, so the tail's refusals go unheard.
	Quiet bool
	// Silent suppresses feedback and refusals (a function's commands).
	// vanilla: CommandSourceStack.withSuppressedOutput
	Silent bool
}

// Move is where a /teleport puts a body. Pos is the absolute end point and
// facing, Offset the per-axis move, and the Relative axes are sent to the
// client as offsets. DeltaX/Y/Z are the velocity axes the client takes
// relatively, surviving a dimension change that drops position relativity;
// RotateDelta (ROTATE_DELTA) first turns that velocity by the rotation
// change. KeepRotation means no rotation was given, so each target keeps
// its facing.
// LookAt is a facing clause's aim: a player is sent Player Look At, any other
// body is turned here. LookAtFromEyes anchors it at the eyes (each target's
// own eye height); LookAtEntity, when set, sends the Look At in entity form
// with LookAtEntityEyes as that entity's anchor.
// vanilla: Relative, PositionMoveRotation
type Move struct {
	Pos          player.Position
	Offset       player.Position
	RelX         bool
	RelY         bool
	RelZ         bool
	RelYaw       bool
	RelPitch     bool
	DeltaX       bool
	DeltaY       bool
	DeltaZ       bool
	RotateDelta  bool
	KeepRotation bool
	LookAt       *player.Position

	LookAtFromEyes   bool
	LookAtEntity     int32
	LookAtEntityEyes bool
}

// Entity is anything a command can target (player, mob, item, arrow, orb).
// It works in entity state and text components, never packets: the adapter
// sends what a change owes the client.
type Entity interface {
	// DisplayName is the feedback name: the type's translation with the
	// UUID insertion and show-entity hover that make it pasteable.
	// vanilla: Entity.getDisplayName
	DisplayName() protocol.Text

	// PlainName is the text the display name renders as on the server, what
	// a selector's name option is matched against.
	PlainName() string

	// EntityType is the name the entity answers to in the minecraft:entity_type registry.
	EntityType() string

	// UUID is the identity a bare target word that parses as a UUID is looked up by.
	UUID() uuid.UUID

	// EntityID is the protocol entity ID the packets name the entity by.
	EntityID() int32

	// DimensionName is the wire name of the dimension the entity stands in.
	DimensionName() string

	// Position is where the entity stands and which way it faces.
	Position() player.Position

	// EyePosition is where the entity's eyes are: the position with the
	// height the kind holds its eyes at added.
	EyePosition() player.Position

	// Box is the entity's bounding box, which a selector's volume options pick by.
	Box() (minX, minY, minZ, maxX, maxY, maxZ float64)

	// Alive says whether the entity is a living thing still in the world,
	// what the @e and @n selectors' alive filter asks after.
	Alive() bool

	// Kill kills the entity the way the /kill command does.
	Kill()

	// Swing plays a hand's arm swing of the animation given for watchers;
	// false for a non-living body.
	// vanilla: LivingEntity.swing
	Swing(offHand bool, anim registry.SwingAnimation) bool

	// Teleport moves the entity to the move's absolute point and facing,
	// telling watchers. dim is the arrival dimension's wire name; by names
	// the runner in the move's log.
	Teleport(move Move, dim string, by string)

	// Rotate turns the entity in place: a player is sent Player Rotation,
	// any other body turns and watchers are told.
	Rotate(yaw, pitch float32)

	// ItemIn returns the stack in a raw slot (a player's window, equipment,
	// ender chest, cursor and crafting slots, a mob's hands); false for a
	// slot it lacks.
	// vanilla: LivingEntity.getSlot
	ItemIn(slot ItemSlot) (protocol.ItemStack, bool)

	// SetItemIn puts the stack in the raw slot the way /item does,
	// reporting false for a slot this entity accepts nothing in.
	SetItemIn(slot ItemSlot, stack protocol.ItemStack) bool

	// UpdateItemIn reads, updates and (if update says so) writes back a raw
	// slot's stack under one hold of its lock, so a write never lands on a
	// slot changed in between; false for a slot it lacks.
	UpdateItemIn(slot ItemSlot, update func(protocol.ItemStack) (protocol.ItemStack, bool)) bool

	// Tags are the entity's scoreboard tags in insertion order.
	// vanilla: Entity.entityTags
	Tags() []string

	// AddTag adds a scoreboard tag; false when already held or at
	// vanilla's cap of 1024.
	// vanilla: Entity.addTag
	AddTag(tag string) bool

	// RemoveTag removes a scoreboard tag; false when not held.
	// vanilla: Entity.removeTag
	RemoveTag(tag string) bool
}

// Target is a player a command can act on, the runner included: decisions
// stay here, packets in the server's adapters. A player is also an Entity.
type Target interface {
	Entity

	// Name is the player, the way the feedback messages spell them.
	Name() string

	// Profile is the player's UUID and name, what the operator list keeps
	// them by and what a direct message names.
	Profile() player.Profile

	// GameModeNumber is the game mode the player plays in, as the packets
	// number it, what a selector's gamemode option matches against.
	GameModeNumber() int32

	// EyePosition is the position with the eye height of a standing player added.
	EyePosition() player.Position

	// SetGameMode moves the player to mode (client behaviour, abilities, tab
	// list) and reports whether it changed; same mode is a no-op.
	// vanilla: ServerPlayer.setGameMode
	SetGameMode(mode int32) bool

	// Notify shows the player a line in their chat window: the
	// gameMode.changed a /gamemode run by another player tells them.
	Notify(text protocol.Text)

	// ShowTitle sends the player's client the text of kind with a Set Title
	// Text, Set Subtitle Text or Set Action Bar Text packet.
	ShowTitle(kind TitleKind, text protocol.Text)

	// SetTitleTimes sends the fade and stay ticks with Set Titles Animation.
	SetTitleTimes(fadeIn, stay, fadeOut int32)

	// ClearTitles sends Clear Titles, which also resets the times when reset.
	ClearTitles(reset bool)

	// AddPostEffect, RemovePostEffect and ClearPostEffects change the
	// client's post-processing effects (ids as "namespace:path"), resend
	// them, and report whether the set changed.
	// vanilla: ServerPlayer.addPostEffect
	AddPostEffect(id string) bool
	RemovePostEffect(id string) bool
	ClearPostEffects() bool

	// PostEffects lists the player's post effects in the order they were added.
	PostEffects() []string

	// Teleport moves the player with a Synchronize Player Position, whose
	// relative axes apply to where the client stands; others learn the new
	// position from the client's own report. dim and by are Entity.Teleport's.
	Teleport(move Move, dim string, by string)

	// Rotate turns the player where they stand: the client is sent a Player
	// Rotation carrying the absolute yaw and pitch.
	Rotate(yaw, pitch float32)

	// Kill kills the player outright — no invulnerability window, and no
	// game mode protecting against it but a spectator's.
	Kill()

	// SetCamera sends Set Camera with the entity's ID; the player's own ID
	// returns the camera. vanilla: ServerPlayer.setCamera
	SetCamera(target Entity)

	// PickBlock is the block the look meets within reach, or on a miss the
	// one holding the ray's end. vanilla: Entity.pick
	PickBlock(reach float64) (x, y, z int32)

	// HighlightTestPos sends Game Test Highlight Pos.
	// vanilla: ClientboundGameTestHighlightPosPacket
	HighlightTestPos(absolute, relative [3]int32)

	// GiveItem adds a stack as a pickup lands (onto stacks first, then the
	// first empty slot, hand first), dropping what does not fit.
	GiveItem(stack protocol.ItemStack)

	// AddItem adds a stack as a pickup does, reporting whether any went in
	// (always for creative); what does not fit is lost, not dropped.
	// vanilla: Inventory.add
	AddItem(stack protocol.ItemStack) bool

	// ClearInventory takes what a /clear asks for out of the inventory and
	// returns how many items went. A nil filter takes every stack.
	ClearInventory(filter ItemFilter, count int64, hasCount bool) int64

	// SetSpawnPoint sets the respawn block, arrival angles and dimension,
	// persisted with the player.
	SetSpawnPoint(pos world.SpawnPos, yaw, pitch float32, dimension string)

	// PlaySound sends a Sound Effect; source is the category number and
	// seed the one variant seed the command drew for every target.
	PlaySound(name string, source int32, x, y, z float64, volume, pitch float32, seed int64)

	// StopSound sends Stop Sound; source -1 is every category, an empty
	// sound every sound.
	StopSound(source int32, sound string)

	// SendParticle sends Level Particles with encoded options; force draws
	// it beyond the normal particle distance.
	SendParticle(id int32, data []byte, force bool, x, y, z float64, dx, dy, dz, speed float32, count int32)

	// Transfer tells the client to reconnect to host:port; the client
	// closes the connection itself.
	Transfer(host string, port int32)

	// The experience the /experience command moves; every change tells the
	// player's client where their experience now stands.
	GiveExperiencePoints(points int)
	GiveExperienceLevels(levels int)
	// SetExperiencePoints moves the points to an exact standing and reports
	// false, changing nothing, for points of a whole level or more.
	SetExperiencePoints(points int) bool
	SetExperienceLevels(levels int) bool
	ExperiencePoints() int
	ExperienceLevels() int

	// Advancements is the player's progress, nil for a non-player.
	// FlushAdvancements sends what changed and announces completions; show
	// is Update Advancements' flag.
	// vanilla: PlayerAdvancements.flushDirty
	Advancements() *advancement.Progress
	FlushAdvancements(show bool)
	// AnnounceAdvancements rewards and announces what the player completed.
	AnnounceAdvancements()
	// BroadcastItemChanges fires inventory criteria and announces completions
	// after a command writes the player's slots.
	BroadcastItemChanges()

	// IncomingMessage shows a direct message in the incoming form, naming
	// the sender. message preserves the signed input; resolved includes selectors.
	// vanilla: MsgCommand, ServerPlayer.sendChatMessage
	IncomingMessage(from protocol.Text, message *chatsecurity.Message, resolved protocol.Text)

	// OutgoingMessage shows the sender their direct message in the outgoing
	// form, naming the recipient (to).
	// vanilla: MsgCommand, CommandSourceStack.sendChatMessage
	OutgoingMessage(from protocol.Text, message *chatsecurity.Message, to protocol.Text, resolved protocol.Text)

	// TeamMessage shows a team message naming the team (team), in the
	// outgoing form to its sender and the incoming form to the rest.
	// vanilla: TeamMsgCommand.sendMessage
	TeamMessage(outgoing bool, from protocol.Text, message *chatsecurity.Message, team protocol.Text, resolved protocol.Text)

	// IP is the address the player connected from, without its port, which
	// /ban-ip bans by when it names an online player.
	IP() string

	// Disconnect closes the player's connection with the reason: the reason
	// reaches the client before the connection ends.
	Disconnect(reason protocol.Text)

	// ShowDialog opens a dialog; ClearDialog closes the open one.
	// vanilla: ServerPlayer.openDialog, ClientboundClearDialogPacket
	ShowDialog(dialog DialogRef)
	ClearDialog()

	// PermLevel is the player's permission level. SetPermLevel changes it
	// silently; ResendPermissionState sends what it decides (the command
	// tree and the level's entity event).
	PermLevel() int
	SetPermLevel(level int)
	ResendPermissionState()
}

// BanListEntry is one row of /banlist: the banned name or address, who banned
// it, and the reason shown for it.
type BanListEntry struct {
	// Name is the player name or address, a bare literal the row spells it
	// with.
	Name protocol.Text
	// Source is the name of whoever banned it.
	Source string
	// Reason is the message the ban shows, the entry's or vanilla's default.
	Reason protocol.Text
}

// Source is the execution context and server access a command runs with.
// Methods are added when a command needs one. It works in text components
// and plain values, keeping this package free of packets.
type Source interface {
	Name() string
	DisplayName() protocol.Text
	Position() player.Position
	EyePosition() player.Position
	PermLevel() int
	OutgoingMessage(from protocol.Text, message *chatsecurity.Message, to protocol.Text, resolved protocol.Text)

	// Debug logs from inside a command on the logger that names the player.
	Debug(msg string, args ...any)
	// Info logs a line an operator reading the log sees a command leave.
	Info(msg string, args ...any)

	// Feedback sends a translated line.
	// vanilla: CommandSourceStack.sendSuccess
	Feedback(key string, with ...protocol.Text) error
	// FeedbackText is Feedback of a finished, styled line.
	FeedbackText(text protocol.Text) error

	// Silent reports that neither feedback nor the admins' echo of it reaches
	// anyone, so a command may return before building them.
	// vanilla: CommandSourceStack.sendSuccess asks its Supplier only for a listener
	Silent() bool

	// InformAdmins sends a success to the other operators as the grey
	// italic "[Name: message]" (send_command_feedback) and to the console
	// (log_admin_commands); the server's own commands are not echoed.
	InformAdmins(message protocol.Text)

	// Message sends the player a finished message in the chat window.
	Message(text protocol.Text) error

	// Error sends the bare red message of a command that parsed but found
	// nothing to act on, without a parse error's context. The returned error
	// is not the refusal.
	// vanilla: EntityArgument
	Error(key string) error

	// ErrorWith sends the bare red message with the values its %s
	// placeholders take. The error it returns is not the refusal.
	ErrorWith(key string, with ...protocol.Text) error

	// CommandTree returns the command tree the player's permission level is
	// offered: the one the Commands packet was built from and /help describes.
	CommandTree() *Node

	// OnlinePlayers returns every player's profile in player-list order, the
	// order /list names them in.
	OnlinePlayers() []player.Profile
	// MaxPlayers is the most the server takes.
	MaxPlayers() int

	// OnlineMode reports whether players are authenticated, which decides
	// whether a never-joined name resolves to an offline profile.
	OnlineMode() bool

	// OnlineTargets returns every player in the world as a target, in
	// player-list order, the order vanilla resolves a selector's targets in.
	OnlineTargets() []Target

	// OnlineTargetsIn returns a dimension's players in its player-list
	// order, the order a world-limited selector resolves in.
	OnlineTargetsIn(dimension string) []Target

	// SelfEntity is the runner themself as one of the entities a selector
	// answers with — vanilla CommandSourceStack.getEntity.
	SelfEntity() Entity

	// Entities returns snapshots of every entity the query does not reject,
	// so a tick moving a mob mid-resolve cannot leave the answer half a step
	// old. Never nil.
	Entities(q EntityQuery) []Entity

	// InContext returns the source an execution's tail runs against, the
	// world read from ctx. false for an unknown dimension, which the
	// dimension argument has already refused.
	InContext(ctx Context) (Source, bool)

	// AnchorEyes says local coordinates are measured from the eyes, as an
	// execute anchored eyes leaves them, rather than from the feet.
	AnchorEyes() bool

	// BiomeAt is the biome's wire name at a position, "" when unloaded.
	BiomeAt(x, y, z int32) string

	// EnvironmentFloat is a float-convertible environment attribute's value
	// at a position in the source's dimension.
	EnvironmentFloat(attribute string, x, y, z float64) float32
	// IsRaining and IsThundering are Level.isRaining and isThundering of
	// the source's dimension.
	IsRaining() bool
	IsThundering() bool

	// HeightmapHeight is the named heightmap's height of the column: one
	// above its highest matching block, the dimension's min y for an empty
	// column. A positioned over of an /execute puts the position's y at it.
	// vanilla: Level.getHeight
	HeightmapHeight(kind string, x, z int32) int32

	// Random is the source @r shuffles its picks with.
	Random() *rand.Rand

	// RandomSequence returns the named sequence, creating it from the
	// world seed and current reset defaults if missing, as vanilla's first
	// draw does.
	// vanilla: RandomSequences.get
	RandomSequence(id string) RandomSequence
	// ResetRandomSequence restarts a sequence with the given salt and flags,
	// or the current defaults where none were given.
	// vanilla: RandomSequences.reset
	ResetRandomSequence(id string, salt int32, hasSalt bool, includeWorldSeed, includeSequenceID bool)
	// ResetAllRandomSequences clears every sequence, answering the count;
	// the salt and flags become the defaults for later sequences.
	// vanilla: RandomSequences.setSeedDefaults, RandomSequences.clear
	ResetAllRandomSequences(salt int32, hasSalt bool, includeWorldSeed, includeSequenceID bool) int
	// RandomSequenceIDs names the existing sequences, for completion.
	// vanilla: RandomCommand.suggestRandomSequence
	RandomSequenceIDs() []string

	// ChatArgument returns an immutable verified argument scoped to this invocation.
	ChatArgument(name string) *chatsecurity.Message

	// BroadcastSay and BroadcastMe announce over the /say and /me chat
	// types, preserving the verified message and selector-resolved content.
	BroadcastSay(message *chatsecurity.Message, resolved protocol.Text)
	BroadcastMe(message *chatsecurity.Message, resolved protocol.Text)

	// BroadcastSystemMessage sends every player a system chat line (a
	// /random roll) and logs it.
	// vanilla: PlayerList.broadcastSystemMessage
	BroadcastSystemMessage(text protocol.Text)

	// Seed is the seed the world was generated from, or zero for one that
	// never had any — what the vanilla server prints for the same.
	Seed() int64

	// Difficulty is how hard the world plays, as the Change Difficulty
	// packet numbers it.
	Difficulty() uint8
	// SetDefaultGameMode sets the mode players join in from now on; with
	// force-gamemode on, it also moves every online player into it. It
	// returns how many players changed mode.
	// vanilla: MinecraftServer.setDefaultGameType, enforceGameTypeForPlayers
	SetDefaultGameMode(mode int32) int32

	// SetDifficulty changes it for everyone in the world, persisted by the
	// next save.
	SetDifficulty(d uint8)
	// BroadcastDifficulty tells every client what the new difficulty is.
	BroadcastDifficulty()

	// SetWeather sets the weather flags and timers, clearTime holding a
	// forced clear in place of the rain timer.
	SetWeather(raining, thundering bool, rainTime, thunderTime, clearTime int32)
	// RollDice throws the dice on the runner's world's seeded random, the
	// level random vanilla's commands roll on.
	RollDice(dice world.Dice) int32

	// ClockTime is the world age and a clock's total ticks; false for a
	// clock with no state, which the argument refuses first.
	ClockTime(clock string) (worldAge, total int64, ok bool)
	// GameTime is the world age, which needs no clock.
	GameTime() int64
	// DefaultClock is the current dimension type's optional default clock.
	DefaultClock() (string, bool)
	// MoveClockToMarker jumps a clock forward to the marker's next
	// occurrence over its period (/time set <marker>).
	MoveClockToMarker(clock string, ticks, period int64) bool
	// SetClockTime, AddClockTime, SetClockPaused and SetClockRate change a
	// clock and tell every player at once rather than at the next periodic
	// sync. The ones answering a bool report false, changing nothing, for a
	// clock already as asked.
	SetClockTime(clock string, ticks int64) bool
	AddClockTime(clock string, ticks int64)
	SetClockPaused(clock string, paused bool) bool
	SetClockRate(clock string, rate float32) bool

	// BoolRule and IntRule read one rule of the world's table, the default
	// standing in where the table has none.
	BoolRule(id world.GameRuleIndex) bool
	IntRule(id world.GameRuleIndex) int32
	// SetBoolRule and SetIntRule put one where the command asks, persisted
	// by the next save the way the clock and the weather are.
	SetBoolRule(id world.GameRuleIndex, value bool)
	SetIntRule(id world.GameRuleIndex, value int32)
	// RuleChanged sends what a changed rule owes players: advance_time's
	// clock sync, immediate_respawn's and limited_crafting's game events,
	// reduced_debug_info's entity events.
	RuleChanged(id string)

	// DimensionName is the wire name of the dimension the player is in,
	// which the feedback of the spawn-point commands names.
	DimensionName() string
	// DimensionTypeName is the current dimension type's registry name.
	DimensionTypeName() string
	// DimensionKnown reports whether the server runs a dimension of this
	// wire name.
	DimensionKnown(name string) bool
	// BlockAt reads the block at the coordinates of the runner's world, or
	// air where the chunk is not loaded.
	BlockAt(x, y, z int32) uint16
	// SetBlock sets a state with the world's follow-ups (relight, broadcast,
	// portal updates), reporting whether the position was loaded.
	SetBlock(x, y, z int32, state uint16) bool
	// SetBlockTagged is SetBlock that then loads the argument's tag into the
	// block entity, as a placement does. destroy breaks the old block first
	// (loot and effect, as a player break), and destroying air over the
	// break's own air places nothing yet answers true; a non-destroy
	// placement leaving the cell unchanged answers false. strict wakes no
	// neighbours. tag is nil when none was given; it is dropped on a block
	// without a block entity and carried by the chunk for an unmodelled one.
	// An unchanged block takes the tag (answering true) only if it changes
	// the block entity.
	SetBlockTagged(x, y, z int32, state uint16, defined map[string]string, tag nbt.CompoundTag, strict, destroy bool) bool
	// HoldChunk loads the chunk holding the block column and keeps it until
	// the returned function is called.
	HoldChunk(x, z int32) (release func())
	// HeightRange is the lowest and highest block Y of the runner's dimension.
	HeightRange() (minY, maxY int32)
	// BlockLoaded reports whether the position's chunk is loaded, the first
	// check vanilla's position arguments run (a /fill corner refusal).
	BlockLoaded(x, y, z int32) bool
	// ContainerSizeAt is a container block's slot count, 0 for a block /item
	// cannot address.
	// vanilla: ItemCommands.getContainer
	ContainerSizeAt(x, y, z int32) int
	// ItemInContainerAt is a container slot's stack, empty for a slot it
	// lacks, so callers read the size first.
	ItemInContainerAt(x, y, z int32, slot int) protocol.ItemStack
	// SetItemInContainerAt puts the stack in the container block's slot the
	// way a /item set does, telling the viewers of the container.
	SetItemInContainerAt(x, y, z int32, slot int, stack protocol.ItemStack)
	// CanPlaceItemInContainerAt reports whether a container slot takes the
	// stack (a furnace's output and a full jukebox take none).
	// vanilla: Container.canPlaceItem
	CanPlaceItemInContainerAt(x, y, z int32, slot int, stack protocol.ItemStack) bool
	// BlockLootContextAt is a block's loot context (state, block entity
	// contents and name); the tool and breaker are the caller's.
	// vanilla: Block.dropResources
	BlockLootContextAt(x, y, z int32) rules.LootContext
	// InWorldBounds reports whether the position is within the dimension's
	// height and 30,000,000 blocks each way, the arguments' second check.
	InWorldBounds(x, y, z int32) bool
	// FillCells writes every change at once (all written before any wakes
	// anything), loading the argument's tag into each block entity. destroy
	// breaks the old blocks, strict wakes nothing, handNew gives neighbours
	// the new block rather than the old. It returns the cells changed, as
	// vanilla's feedback counts: an unchanged cell counts only if destroy
	// broke something there or the tag changed its block entity. A
	// non-strict fill that hands neighbours the new block places cell by
	// cell, reshaping neighbours (and dropping what loses its support) after
	// each, and skips a cell whose current state accept (nil: all) refuses.
	FillCells(changes []world.BlockChange, accept func(x, y, z int32, old uint16) bool, defined map[string]string, tag nbt.Compound, strict, destroy, handNew bool) int
	// FillBiomes writes biome into the box's 4x4x4 cells filter accepts (nil
	// all), re-sends the changed chunks' biomes and returns how many cells
	// held another biome; false when a spanned chunk is unloaded.
	FillBiomes(box world.BiomeBox, biome string, filter func(current string) bool) (count int, ok bool)
	// PlaceFeature places the configured feature at the position; false when
	// it has no generator or does not fit.
	// vanilla: ConfiguredFeature.place
	PlaceFeature(key string, x, y, z int32) bool
	// PlaceStructure builds the structure's start in the chunk holding
	// (x, z) and writes its pieces.
	// vanilla: PlaceCommand.placeStructure
	PlaceStructure(id string, x, z int32) world.StructurePlacement
	// PlaceJigsaw grows the jigsaw assembly of the pool from the jigsaw block
	// named target, at the position, and writes its pieces.
	// vanilla: PlaceCommand.placeJigsaw
	PlaceJigsaw(pool, target string, depth int, x, y, z int32) world.StructurePlacement
	// PlaceTemplate writes a structure template laid out by Template.Place.
	// vanilla: StructureTemplate.placeInWorld
	PlaceTemplate(t *world.PlacedTemplate, strict bool)
	// FindClosestBiome searches the runner's dimension by its generator,
	// loaded or not; false when none is in range.
	// vanilla: ServerLevel.findClosestBiome3d
	FindClosestBiome(search world.BiomeSearch) (world.BiomeHit, bool)
	// NearestStructure finds the nearest of ids within radius chunks,
	// returning its block column and the id.
	// vanilla: ChunkGenerator.findNearestMapStructure
	NearestStructure(ids []string, x, y, z int32, radius int) (fx, fz int32, found string, ok bool)
	// NearestPOI finds the block nearest the point, within radius blocks,
	// whose point-of-interest type is one of types, returning that type.
	// vanilla: PoiManager.findClosestWithType
	NearestPOI(types []string, x, y, z int32, radius int) (fx, fy, fz int32, typ string, ok bool)
	// TestInstanceBlocks lists the test instance blocks within radius blocks
	// of the point: in the source's dimension, or in every dimension.
	// vanilla: StructureUtils.findTestBlocks
	TestInstanceBlocks(x, y, z int32, radius int, everyDimension bool) []TestInstanceBlock
	// CreateTest clears a structure box of size above the point and puts
	// there a test instance block for id, its box floored with bedrock.
	// vanilla: StructureUtils.createNewEmptyTest, TestCommand.createNewStructure
	CreateTest(id string, x, y, z int32, size [3]int32)
	// ResetTest restores a test's template and clears its status and markers.
	// It reports whether the template was placed.
	ResetTest(x, y, z int32) bool
	// ClearTest clears a test's padded box and barriers and removes its block.
	ClearTest(x, y, z int32)
	// StopTests detaches the running tests and tears down their environment.
	StopTests()
	// RunTests starts selected blocks or a grid of registered tests in batches.
	RunTests(request TestRun)
	// TestInstance resolves a registered test and its environment.
	TestInstance(id string) (datapack.TestInstance, bool)
	// FailedTests lists the previous run's failures, optionally only required ones.
	FailedTests(onlyRequired bool) []string
	// CloneCells writes /clone's copied cells at once (all written before
	// any wakes anything), each block entity taking its source's tag; strict
	// wakes nothing. It counts every landed cell, unchanged ones included,
	// as vanilla's barrier write does.
	CloneCells(changes []world.BlockChange, tags []nbt.Compound, strict bool) int
	// BlockTicksIn is the block ticks pending inside the inclusive box.
	BlockTicksIn(loX, loY, loZ, hiX, hiY, hiZ int32) world.BlockTicks
	// ScheduleBlockTicks books ticks here, offset; one whose cell holds
	// another block than its source did is dropped.
	// vanilla: LevelTicks.copyAreaFrom
	ScheduleBlockTicks(ticks world.BlockTicks, offX, offY, offZ int32)
	// BlockEntityTagAt is the cell's block entity's saved compound, and
	// whether it has one.
	BlockEntityTagAt(x, y, z int32) (nbt.CompoundTag, bool)
	// BlockBoxLoaded reports whether every chunk of the box is loaded,
	// /clone's whole-region check after its corners.
	// vanilla: LevelReader.hasChunksAt
	BlockBoxLoaded(x0, z0, x1, z1 int32) bool
	// ListTemplates returns the ids of every structure template, the
	// completions of /place template.
	ListTemplates() []string
	// LoadTemplate reads a structure template from the world's generated
	// folder and data packs; false for an unknown id or a non-template.
	// vanilla: StructureTemplateManager.get
	LoadTemplate(id string) (t *world.Template, ok bool)
	// SetWorldSpawn moves the world spawn (new players, and respawns
	// without a spawn point) in dimension, saved to level.dat and told to
	// every player.
	SetWorldSpawn(pos world.SpawnPos, dimension string)
	// WorldBorder is the border of the runner's dimension.
	WorldBorder() WorldBorder
	// ForcedChunks lists the runner's dimension's force-loaded chunks in the
	// order /forceload query prints them.
	ForcedChunks() []world.ChunkPos
	// ChunkForced reports whether the chunk is force-loaded in the runner's
	// dimension.
	ChunkForced(x, z int32) bool
	// SetChunkForced forces or releases the chunk in the runner's dimension,
	// reporting whether that changed anything.
	SetChunkForced(x, z int32, forced bool) bool
	// SummonMob spawns a mob of kind in the runner's dimension and answers
	// its entity ID and UUID; false for an unsimulated kind.
	SummonMob(kind string, x, y, z float64) (int32, uuid.UUID, bool)
	// SummonMobTagged is SummonMob from a compound tag: vanilla builds the
	// entity from the tag and skips finalizeSpawn, so nothing is rolled
	// against the biome. The tag is read as a saved mob's; the error is
	// ErrDuplicateUUID for a UUID the dimension already holds.
	SummonMobTagged(kind string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonOrb is SummonMob for an experience orb (the one non-mob here) of
	// value; tag's UUID and Tags are read as Entity.load reads them, and
	// a UUID a live body holds is ErrDuplicateUUID.
	SummonOrb(x, y, z float64, value int, noGravity bool, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonTNT is SummonMob for primed TNT with fuse (80 when the tag
	// omits it).
	SummonTNT(x, y, z float64, fuse int, noGravity bool, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonItem is SummonMob for an item entity.
	SummonItem(x, y, z float64, stack protocol.ItemStack, pickupDelay int, noGravity bool, tag nbt.Compound) (int32, uuid.UUID, error)
	// EntityDisplayName is the entity's display name built from name, its
	// custom name or else its type's name, as every target's DisplayName is.
	// vanilla: Entity.getDisplayName
	EntityDisplayName(entityType string, id uuid.UUID, name protocol.Text) protocol.Text
	// SummonLightning is SummonMob for a lightning bolt, which strikes
	// where it stands.
	SummonLightning(x, y, z float64) (int32, uuid.UUID, bool)
	// SummonArmorStand is SummonMob for an armor stand, its tag read as a
	// saved stand's; ErrDuplicateUUID for a live stand's UUID.
	SummonArmorStand(x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonFallingBlock is SummonMob for a falling block of the tag's
	// BlockState (sand when absent), read as a saved one.
	SummonFallingBlock(x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonArrow is SummonMob for the arrow family (arrow, spectral arrow,
	// trident), its tag read as a saved shaft's.
	SummonArrow(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonThrown is SummonMob for a thrown body (snowball, egg, ender
	// pearl, splash and lingering potions), base fields read as Entity.load
	// does.
	SummonThrown(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonPainting is SummonMob for a painting, read as a saved one,
	// hanging from the floored cell.
	SummonPainting(x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonFixture is SummonMob for a text, block or item display, an
	// interaction or a marker, made from the tag.
	SummonFixture(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonSaved is SummonMob for a kind built by its saved loader: end
	// crystal, area effect cloud, evoker fangs, firework rocket, eye of
	// ender, leash knot.
	SummonSaved(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonFrame is SummonMob for an item frame (glowing per name), read
	// as a saved one, hanging from the floored cell.
	SummonFrame(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonBoat is SummonMob for a boat or chest boat of kind, read as a
	// saved one.
	SummonBoat(kind string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	// SummonMinecart is SummonMob for a minecart of kind, read as a saved
	// one.
	SummonMinecart(kind string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)

	// ReadCommandStorage passes a storage's live compound to fn under the
	// storages' lock (an unknown id reads empty); fn must neither keep nor
	// modify it.
	// vanilla: CommandStorage.get
	ReadCommandStorage(id string, fn func(storage nbt.CompoundTag))
	// ModifyCommandStorage edits a storage's live compound under the storages'
	// lock, storing it and marking it dirty when fn returns true. fn must not
	// retain the compound or call Source methods, and must leave it unchanged
	// when returning false.
	// vanilla: CommandStorage.get, CommandStorage.Container.put
	ModifyCommandStorage(id string, fn func(live nbt.CompoundTag) (changed bool))
	// SetCommandStorage stores data, taking ownership of it; an empty data
	// unstores the id.
	// vanilla: CommandStorage.set
	SetCommandStorage(id string, data nbt.CompoundTag)

	// CreateStopwatch creates a stopwatch running from now, answering false
	// when the id is taken.
	CreateStopwatch(id string) bool
	// StopwatchSeconds is a stopwatch's elapsed seconds on the monotonic
	// clock; false when there is none.
	StopwatchSeconds(id string) (float64, bool)
	// RestartStopwatch resets the named stopwatch and runs it from now,
	// answering false when there is none.
	RestartStopwatch(id string) bool
	// RemoveStopwatch drops the named stopwatch, answering false when there
	// is none.
	RemoveStopwatch(id string) bool
	// StopwatchIDs is every stopwatch's id, in id order, which the command's
	// suggestions offer.
	StopwatchIDs() []string

	// Functions is the library of the data packs' loaded functions.
	Functions() *Functions

	// DataPacks is the world's data packs.
	DataPacks() DataPacks

	// ScheduledEvents is the queue /schedule keeps its events in.
	ScheduledEvents() *world.TimerQueue

	// Scoreboard is the server's scoreboard.
	Scoreboard() Scoreboard

	// DefaultOpLevel is the op-permission-level of server.properties, which
	// /op grants.
	DefaultOpLevel() int
	// OpListExists reports whether there is a list at all, which is what
	// decides whether granting is creating one.
	OpListExists() bool
	// GrantOp records a player, possibly offline, as an operator.
	GrantOp(profile player.Profile, level int)
	// RevokeOp takes a player off the operator list.
	RevokeOp(profile player.Profile)
	// OpListLevel reads the level the list holds for a player — the level a
	// creating list takes away from everyone it does not name.
	OpListLevel(profile player.Profile) int
	// OpListed reports whether a player is listed at any level, which /op
	// and /deop decide "nothing changed" by. vanilla: PlayerList.isOp
	OpListed(profile player.Profile) bool

	// KnownProfiles is the user cache plus everyone on the ban and white
	// lists, which a player argument falls back to so /op and /ban can name
	// an offline player.
	KnownProfiles() []player.Profile

	// LookupProfile asks Mojang for the profile of a name nobody has cached;
	// it blocks, and reports false when there is no such player or no answer.
	LookupProfile(name string) (player.Profile, bool)

	// FetchProfileByName and FetchProfileByID look a profile up where the
	// server keeps them (online players, the user cache); they may block, so
	// callers run them off the command's goroutine.
	// vanilla: ProfileResolver.fetchByName, fetchById
	FetchProfileByName(name string) (player.Profile, bool)
	FetchProfileByID(id uuid.UUID) (player.Profile, bool)

	// AvatarProfile is the profile of a player or mannequin as
	// ResolvableProfile.CODEC writes it; false for any other entity.
	AvatarProfile(e Entity) (nbt.Compound, bool)

	// BanPlayer bans a player, false when already banned, answering the
	// online target (nil if offline) the command disconnects after its
	// feedback.
	BanPlayer(profile player.Profile, reason string) (target Target, added bool)
	// PardonPlayer lifts the player's ban, reporting whether they were
	// banned.
	PardonPlayer(profile player.Profile) bool
	// BannedPlayers is the banned players' identities, in file order, which
	// /pardon suggests.
	BannedPlayers() []player.Profile
	// BanList is the banned players as /banlist rows.
	BanList() []BanListEntry

	// OnlinePlayerIP reports the address the named online player connected
	// from, which /ban-ip bans by; false for a name nobody online answers to.
	OnlinePlayerIP(name string) (string, bool)
	// BanIP bans an address, false when already banned, answering the online
	// players behind it for the command to disconnect after its feedback.
	BanIP(ip, reason string) (affected []Target, added bool)
	// PardonIP lifts the address's ban, reporting whether it was banned.
	PardonIP(ip string) bool
	// BannedIPs is the banned addresses, in file order, which /pardon-ip
	// suggests.
	BannedIPs() []string
	// IPBanList is the banned addresses as /banlist ips rows.
	IPBanList() []BanListEntry

	// WhitelistEnabled reports whether the white-list is on.
	WhitelistEnabled() bool
	// SetWhitelistEnabled turns the white-list on or off.
	SetWhitelistEnabled(enabled bool)
	// WhitelistHas reports whether the player is on the white-list.
	WhitelistHas(profile player.Profile) bool
	// WhitelistAdd puts the player on the white-list.
	WhitelistAdd(profile player.Profile)
	// WhitelistRemove takes the player off the white-list.
	WhitelistRemove(profile player.Profile)
	// WhitelistProfiles is the whitelisted players in file order.
	WhitelistProfiles() []player.Profile
	// ReloadWhitelist rereads whitelist.json.
	ReloadWhitelist()
	// KickUnlisted disconnects the online players who are not on the
	// white-list, when enforce-whitelist is on and the white-list is on.
	KickUnlisted()

	// Metrics is the main loop's tick history and the process's CPU
	// samples, what /tick query and the performance commands read.
	Metrics() *metrics.Recorder

	// ServerDir is the directory the server runs in, where /debug writes
	// its trace files.
	ServerDir() string

	// TickRate is the main loop's ticking state /tick reads and changes.
	TickRate() TickRateManager

	// StopServer stops the server as /stop does: stop listening, save, exit.
	// Player adapters hand it to their server, the console to its own.
	StopServer()

	// AddBossBar creates a custom boss bar and answers its display name;
	// false when the id exists. Nothing is sent, as a new bar has no players.
	// vanilla: BossBarCommands.createBar
	AddBossBar(id string, name nbt.Tag) (display protocol.Text, created bool)

	// SetBossBarPlayers sets exactly the bar's players, sending adds and
	// removes; they stay listed while offline.
	// vanilla: BossBarCommands.setPlayers
	SetBossBarPlayers(id string, targets []Target) BossBarResult

	// SetBossBarValue sets the value, telling the bar's players.
	// vanilla: BossBarCommands.setValue
	SetBossBarValue(id string, value int32) BossBarResult

	// SetBossBarMax sets the maximum, telling the bar's players.
	// vanilla: CustomBossEvent.setMax
	SetBossBarMax(id string, maxValue int32) BossBarResult

	// SetBossBarVisible shows or hides the bar, keeping its players.
	// vanilla: BossBarCommands.setVisible
	SetBossBarVisible(id string, visible bool) BossBarResult

	// RemoveBossBar deletes the bar from its players, answering its display
	// name and the bars left; false for an unknown id.
	// vanilla: BossBarCommands.removeBar
	RemoveBossBar(id string) (display protocol.Text, remaining int, ok bool)

	// SetBossBarName renames the bar, telling the players who see it.
	// vanilla: ServerBossEvent.setName
	SetBossBarName(id string, name nbt.Tag) BossBarResult

	// SetBossBarColor sets a Boss Event colour id, telling viewers.
	// vanilla: ServerBossEvent.setColor
	SetBossBarColor(id string, color int32) BossBarResult

	// SetBossBarStyle sets a Boss Event overlay id, telling viewers.
	// vanilla: ServerBossEvent.setOverlay
	SetBossBarStyle(id string, style int32) BossBarResult

	// BossBar describes the bar the id names.
	BossBar(id string) (BossBarInfo, bool)

	// BossBars describes every bar, sorted by id.
	BossBars() []BossBarInfo

	// BossBarIDs are the ids of the custom boss bars, sorted,
	// what the id arguments of /bossbar suggest from.
	BossBarIDs() []string

	// SaveAll saves every player, mob and chunk, reporting success. flush
	// (the synchronous saves here always flush) prints the per-dimension
	// chunk-store report.
	// vanilla: MinecraftServer.saveEverything
	SaveAll(flush bool) bool

	// SetSaving turns auto-save on or off, reporting whether it changed.
	// vanilla: ServerLevel.noSave
	SetSaving(on bool) bool

	// SetIdleTimeout sets the idle kick in minutes; zero turns it off.
	// vanilla: MinecraftServer.setPlayerIdleTimeout
	SetIdleTimeout(minutes int)
}

// BossBarInfo is what /bossbar get and list read of a bar.
type BossBarInfo struct {
	// Display is the bar's bracketed name, coloured as the bar is.
	Display    protocol.Text
	Value, Max int32
	Visible    bool
	// Players are the UUIDs the bar names, online or not.
	Players []uuid.UUID
}

// BossBarResult is what a /bossbar setter did: whether the id named a bar,
// whether it moved the bar, and how the bar reads in feedback afterwards.
type BossBarResult struct {
	Existed, Changed bool
	// Display is the bar's bracketed name, coloured as the bar is.
	// vanilla: CustomBossEvent.getDisplayName
	Display protocol.Text
	// Players is how many players the bar names, offline ones included.
	Players int
}

// feedbackToAdmins is vanilla's sendSuccess(message, true): the feedback to
// the runner, and the echo InformAdmins makes of it.
func feedbackToAdmins(src Source, key string, with ...protocol.Text) error {
	if err := src.Feedback(key, with...); err != nil {
		return err
	}
	src.InformAdmins(protocol.Text{Translate: key, With: with})
	return nil
}

// TickRateManager is the main loop's ticking state; each change applies to
// the whole game at once, not at the next tick.
type TickRateManager interface {
	// Rate is the rate the loop aims at, in ticks a second.
	Rate() float32
	// SetRate aims the loop at the rate. Every player is told the new
	// pace at once.
	SetRate(rate float32)
	// NanosecondsPerTick is the spacing the loop waits out between its
	// ticks, what /tick query's lagging judgement reads.
	NanosecondsPerTick() int64

	// Frozen reports whether the game is held in a freeze.
	Frozen() bool
	// SetFrozen freezes or unfreezes, ending a running sprint and any steps
	// left; every player is told at once.
	SetFrozen(frozen bool)

	// Step runs ticks more steps of a frozen game, false when not frozen;
	// the steps left go out in Step Tick.
	Step(ticks int) bool
	// StopStep cancels the steps a frozen game has left, reporting whether
	// there were any.
	StopStep() bool

	// Sprinting reports a sprint whose ticks the loop has not all run.
	Sprinting() bool
	// Sprint runs ticks without pacing, reporting whether a sprint was
	// already running (its report dropped). The sprint restores the freeze
	// it found when it ends or is stopped.
	Sprint(ticks int) bool
	// StopSprint ends a turning sprint at once, reporting whether one was.
	StopSprint() bool
}

// FeedbackWanter is a Source that can say no one will see its feedback, so a
// command skips building the message. Feedback and InformAdmins must then do
// nothing.
type FeedbackWanter interface {
	WantsFeedback() bool
}

func wantsFeedback(src Source) bool {
	if src.Silent() {
		return false
	}
	w, ok := src.(FeedbackWanter)
	return !ok || w.WantsFeedback()
}
