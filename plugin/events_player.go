package plugin

import "net"

// PlayerPreLoginEvent fires once a connecting player's identity is known (after
// authentication in online mode), before they join, on the connection's
// goroutine: the place for slow checks such as a database ban lookup.
// Bukkit: AsyncPlayerPreLoginEvent
type PlayerPreLoginEvent struct {
	Base
	Profile Profile
	Address net.Addr
	// Deny refuses the login with Reason when set.
	Deny   bool
	Reason Text
}

// PlayerLoginEvent fires on the tick goroutine as the player is let in, after the
// ban, whitelist and full checks; Result says which refused them, and a
// listener may change it. Bukkit: PlayerLoginEvent
type PlayerLoginEvent struct {
	Base
	Player  Player
	Address net.Addr
	// Hostname is the server address from the client's handshake, as
	// sent (host and port). Bukkit: getHostname
	Hostname string
	Result   LoginResult
	// Reason is the disconnect message for a result other than Allowed.
	Reason Text
}

// LoginResult is whether a login is let in.
type LoginResult uint8

const (
	LoginAllowed LoginResult = iota
	LoginFull
	LoginBanned
	LoginWhitelist
	LoginOther
)

// PlayerConfigureEvent fires while a player is in the configuration phase,
// before they enter the world: the place to send resource packs, store
// cookies or transfer them. Paper: AsyncPlayerConnectionConfigureEvent
type PlayerConfigureEvent struct {
	Base
	Player Player
}

// PlayerJoinEvent fires once the player is in the world and has their chunks;
// Message is the join message every player is sent, nil for none.
// Bukkit: PlayerJoinEvent
type PlayerJoinEvent struct {
	Base
	Player  Player
	Message *Text
}

// PlayerQuitEvent fires as a player leaves, before they are saved; Message is
// the quit message, nil for none. Bukkit: PlayerQuitEvent
type PlayerQuitEvent struct {
	Base
	Player  Player
	Message *Text
}

// PlayerKickEvent fires before a player is kicked; Reason and the broadcast
// Message may be changed. Bukkit: PlayerKickEvent
type PlayerKickEvent struct {
	Cancellable
	Player  Player
	Reason  Text
	Message *Text
}

// PlayerChatEvent fires for a chat message, before it is sent. Recipients may be
// trimmed. A changed Message is sent as an unsigned system message, since
// the player's signature covers only what they typed. Bukkit:
// AsyncPlayerChatEvent (here on the tick goroutine, where vanilla handles
// chat)
type PlayerChatEvent struct {
	Cancellable
	Player     Player
	Message    string
	Recipients []Player
	// Format renders the line each recipient sees; nil for vanilla's chat
	// type. Paper: ChatRenderer
	Format func(sender Player, message Text, viewer Player) Text
}

// PlayerCommandEvent fires for a command a player typed, before it is parsed;
// Command (without its slash) may be changed. Bukkit:
// PlayerCommandPreprocessEvent
type PlayerCommandEvent struct {
	Cancellable
	Player  Player
	Command string
}

// PlayerCommandSendEvent fires when a player is sent the command tree; Hide
// removes root commands from what they see. Bukkit: PlayerCommandSendEvent
type PlayerCommandSendEvent struct {
	Base
	Player Player
	Hide   []string
}

// PlayerMoveEvent fires when a player's position or rotation changes. Cancelling
// sends them back to From; To may be changed. Bukkit: PlayerMoveEvent
type PlayerMoveEvent struct {
	Cancellable
	Player   Player
	From, To Location
}

// EntityTeleportEvent fires before an entity or player teleports; To may be
// changed. Bukkit: PlayerTeleportEvent and EntityTeleportEvent
type EntityTeleportEvent struct {
	Cancellable
	Entity   Entity
	From, To Location
	Cause    TeleportCause
}

// PlayerChangedWorldEvent fires after a player moves to another world.
type PlayerChangedWorldEvent struct {
	Base
	Player Player
	From   World
}

// EntityPortalEvent fires before an entity uses a portal; To may be changed.
// For a nether portal it fires before the exit is searched for, To being the
// scaled target in the vanilla destination: the exit is then searched for
// within SearchRadius of To, and built within CreationRadius of it when none
// is found and CanCreatePortal is set. Cancelled, nothing is searched or built.
// For an end portal To is the arrival.
// Bukkit: PlayerPortalEvent and EntityPortalEvent
type EntityPortalEvent struct {
	Cancellable
	Entity   Entity
	From, To Location
	Cause    TeleportCause
	// SearchRadius and CreationRadius are in blocks; they apply to nether portals.
	SearchRadius, CreationRadius int
	CanCreatePortal              bool
}

// PlayerRespawnEvent fires as a player respawns; Location may be changed.
// Bukkit: PlayerRespawnEvent
type PlayerRespawnEvent struct {
	Base
	Player   Player
	Location Location
	// Bed and Anchor say which respawn point was used, if any.
	Bed, Anchor bool
}

// PlayerInteractEvent fires when a player clicks: a block or the air, with either
// hand. Cancelling stops both the item's use and the block's. Bukkit:
// PlayerInteractEvent
type PlayerInteractEvent struct {
	Cancellable
	Player Player
	Action InteractAction
	Hand   Hand
	Item   ItemStack
	// Block and Face are the clicked block's; nil for the air.
	Block *Block
	Face  BlockFace
	// Cursor is where on the face the click landed, 0 to 1 per axis.
	Cursor Vec3
	// DenyBlock and DenyItem stop only the block's use or the item's.
	DenyBlock, DenyItem bool
}

// InteractAction is what a click was.
type InteractAction uint8

const (
	LeftClickBlock InteractAction = iota
	RightClickBlock
	LeftClickAir
	RightClickAir
	// Physical is a pressure plate, tripwire or farmland stepped on.
	Physical
)

// PlayerInteractEntityEvent fires when a player right-clicks an entity; Pos is
// where on it, for armour stands. Bukkit: PlayerInteractEntityEvent
type PlayerInteractEntityEvent struct {
	Cancellable
	Player Player
	Target Entity
	Hand   Hand
	Pos    Vec3
}

// PlayerAttackEvent fires when a player attacks an entity, before damage is
// worked out. Paper: PrePlayerAttackEntityEvent
type PlayerAttackEvent struct {
	Cancellable
	Player Player
	Target Entity
}

// PlayerDropItemEvent fires when a player drops an item. Bukkit:
// PlayerDropItemEvent
type PlayerDropItemEvent struct {
	Cancellable
	Player Player
	Item   ItemEntity
}

// EntityPickupItemEvent fires when a player or mob picks up an item entity; it
// may pick up only Count of it. Bukkit: EntityPickupItemEvent
type EntityPickupItemEvent struct {
	Cancellable
	Entity LivingEntity
	Item   ItemEntity
	Count  int
}

// PlayerItemHeldEvent fires when a player selects another hotbar slot.
// Bukkit: PlayerItemHeldEvent
type PlayerItemHeldEvent struct {
	Cancellable
	Player        Player
	Previous, New int
}

// PlayerSwapHandItemsEvent fires when a player swaps their hands' items; the two
// stacks may be changed. Bukkit: PlayerSwapHandItemsEvent
type PlayerSwapHandItemsEvent struct {
	Cancellable
	Player            Player
	MainHand, OffHand ItemStack
}

// PlayerItemConsumeEvent fires when a player finishes eating or drinking; Item may
// be changed. Bukkit: PlayerItemConsumeEvent
type PlayerItemConsumeEvent struct {
	Cancellable
	Player Player
	Item   ItemStack
	Hand   Hand
}

// PlayerItemDamageEvent fires when an item loses durability; Damage may be
// changed. Bukkit: PlayerItemDamageEvent
type PlayerItemDamageEvent struct {
	Cancellable
	Player Player
	Item   ItemStack
	Damage int
}

// PlayerItemBreakEvent fires when an item breaks from use. Bukkit:
// PlayerItemBreakEvent
type PlayerItemBreakEvent struct {
	Base
	Player Player
	Item   ItemStack
}

// PlayerToggleSneakEvent, PlayerToggleSprintEvent and PlayerToggleFlightEvent fire when a
// player starts or stops sneaking, sprinting or flying.
type PlayerToggleSneakEvent struct {
	Cancellable
	Player Player
	On     bool
}

type PlayerToggleSprintEvent struct {
	Cancellable
	Player Player
	On     bool
}

type PlayerToggleFlightEvent struct {
	Cancellable
	Player Player
	On     bool
}

// PlayerGameModeChangeEvent fires before a player's game mode changes.
type PlayerGameModeChangeEvent struct {
	Cancellable
	Player Player
	New    GameMode
}

// PlayerBedEnterEvent fires when a player tries to sleep; Result says whether
// vanilla lets them, and may be changed. Bukkit: PlayerBedEnterEvent
type PlayerBedEnterEvent struct {
	Cancellable
	Player Player
	Bed    Block
	Result BedResult
}

// BedResult is vanilla's answer to a sleep attempt.
type BedResult uint8

const (
	BedOK BedResult = iota
	BedNotPossibleHere
	BedNotPossibleNow
	BedTooFarAway
	BedObstructed
	BedNotSafe
)

// PlayerBedLeaveEvent fires when a player gets out of bed.
type PlayerBedLeaveEvent struct {
	Base
	Player Player
	Bed    Block
}

// PlayerBucketEvent fires when a player fills or empties a bucket; Result is the
// bucket afterwards and may be changed. Bukkit: PlayerBucketFillEvent,
// PlayerBucketEmptyEvent
type PlayerBucketEvent struct {
	Cancellable
	Player Player
	Block  Block
	Face   BlockFace
	Fill   bool
	Item   ItemStack
	Result ItemStack
}

// PlayerExpChangeEvent fires when a player gains experience points; Amount may
// be changed. Bukkit: PlayerExpChangeEvent
type PlayerExpChangeEvent struct {
	Base
	Player Player
	Amount int
}

// PlayerLevelChangeEvent fires after a player's level changes.
type PlayerLevelChangeEvent struct {
	Base
	Player   Player
	Old, New int
}

// PlayerFishEvent fires at each stage of fishing. Bukkit: PlayerFishEvent
type PlayerFishEvent struct {
	Cancellable
	Player Player
	Hook   Entity
	// Caught is the item or entity reeled in, at StateCaught.
	Caught Entity
	State  FishState
	// Experience may be changed at StateCaught.
	Experience int
}

// FishState is a stage of fishing.
type FishState uint8

const (
	FishCast FishState = iota
	FishBite
	FishCaught
	FishCaughtEntity
	FishInGround
	FishFailed
	FishReelIn
)

// PlayerEditBookEvent fires when a player edits or signs a book; the pages and
// title may be changed. Bukkit: PlayerEditBookEvent
type PlayerEditBookEvent struct {
	Cancellable
	Player Player
	Pages  []string
	Title  *string
}

// PlayerAdvancementDoneEvent fires when a player completes an advancement; Message
// is the chat announcement, nil for none. Bukkit: PlayerAdvancementDoneEvent
type PlayerAdvancementDoneEvent struct {
	Base
	Player      Player
	Advancement Advancement
	Message     *Text
}

// PlayerAdvancementCriterionGrantEvent fires before a criterion is granted, by
// a trigger, /advancement or a plugin; cancelling leaves it ungranted, with no
// progress update, toast or reward. A listener runs while the player's
// advancement progress is locked, so it must not read or change that progress.
// Bukkit: PlayerAdvancementCriterionGrantEvent
type PlayerAdvancementCriterionGrantEvent struct {
	Cancellable
	Player      Player
	Advancement Advancement
	Criterion   string
}

// PlayerResourcePackStatusEvent fires for each status a client reports for a
// pack. Bukkit: PlayerResourcePackStatusEvent
type PlayerResourcePackStatusEvent struct {
	Base
	Player Player
	ID     UUID
	Status PackStatus
}

// PackStatus is a client's resource pack status.
type PackStatus uint8

const (
	PackLoaded PackStatus = iota
	PackDeclined
	PackFailedDownload
	PackAccepted
	PackDownloaded
	PackInvalidURL
	PackFailedReload
	PackDiscarded
)

// PlayerClientSettingsEvent fires when a player changes their client settings.
// Paper: PlayerClientOptionsChangeEvent
type PlayerClientSettingsEvent struct {
	Base
	Player       Player
	Locale       string
	ViewDistance int
}

// PlayerAnimationEvent fires when a player swings an arm. Bukkit:
// PlayerAnimationEvent
type PlayerAnimationEvent struct {
	Cancellable
	Player Player
	Hand   Hand
}

// PlayerShearEntityEvent fires when a player or dispenser shears a sheep, mooshroom,
// snow golem or bogged; Drops may be changed. Bukkit: PlayerShearEntityEvent
type PlayerShearEntityEvent struct {
	Cancellable
	Player Player // nil for a dispenser
	Target Entity
	Drops  []ItemStack
}

// PlayerArmorStandManipulateEvent fires when a player swaps an item with an armour stand.
// Bukkit: PlayerArmorStandManipulateEvent
type PlayerArmorStandManipulateEvent struct {
	Cancellable
	Player     Player
	Stand      ArmorStand
	Slot       EquipmentSlot
	PlayerItem ItemStack
	StandItem  ItemStack
}

// PortalKind is which portal block an entity touches.
type PortalKind uint8

const (
	PortalKindNether PortalKind = iota
	PortalKindEnd
	PortalKindEndGateway
)

// EntityPortalEnterEvent fires each tick an entity's body is inside a nether
// portal, end portal or end gateway block, even while its portal cooldown
// holds it there. A player in spectator mode or riding another body does not
// touch the block. It fires on the tick goroutine, after the tick's entity
// passes. Bukkit: EntityPortalEnterEvent
type EntityPortalEnterEvent struct {
	Base
	Entity Entity
	Block  Block
	Kind   PortalKind
}

// EntityPortalExitEvent fires once an entity has arrived through a portal
// (nether or end), with the place it left and where it now stands. Bukkit:
// EntityPortalExitEvent
type EntityPortalExitEvent struct {
	Base
	Entity   Entity
	From, To Location
	Cause    TeleportCause
}
