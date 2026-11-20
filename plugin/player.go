package plugin

import (
	"net"
	"time"
)

// CommandSender runs commands and receives their replies: a player, the
// console, a command block. Bukkit: CommandSender
type CommandSender interface {
	Name() string
	SendMessage(msg Text)
	// OpLevel answers the operator level, 0 to 4; the console has 4.
	OpLevel() int
	// HasPermission decides a permission node: a provider service first,
	// then the player's attachments, then the node's default.
	HasPermission(node string) bool
	// PermissionSet tells whether a permission provider or attachment
	// decided the node explicitly, rather than leaving it to the node's
	// default or to operator level. Bukkit: Permissible.isPermissionSet
	PermissionSet(node string) bool
}

// OfflinePlayer is a player who has played on the server, online or not.
// Bukkit: OfflinePlayer
type OfflinePlayer interface {
	UUID() UUID
	// Name answers the last known name.
	Name() string
	Online() (Player, bool)
	FirstPlayed() time.Time
	LastSeen() time.Time
	// RespawnPoint answers the bed or anchor the player respawns at.
	RespawnPoint() (Location, bool)
	// Statistic reads a counter from the player's stats file when offline.
	Statistic(s Statistic) int
}

// Profile is a game profile: a player's identity and skin.
type Profile struct {
	ID         UUID
	Name       string
	Properties []ProfileProperty
}

// ProfileProperty is a signed profile property such as "textures".
type ProfileProperty struct {
	Name, Value, Signature string
}

// Player is a player in the game. Bukkit: Player
type Player interface {
	LivingEntity
	CommandSender
	OfflinePlayer

	Profile() Profile
	Address() net.Addr
	// Ping answers the latency the server measured.
	Ping() time.Duration
	// Locale, ViewDistance and Brand are what the client reported.
	Locale() string
	ViewDistance() int
	Brand() string

	// DisplayName is the name in chat; ListName the name in the player
	// list, nil for the display name.
	DisplayName() Text
	SetDisplayName(Text)
	ListName() *Text
	SetListName(*Text)
	SetListHeaderFooter(header, footer Text)

	GameMode() GameMode
	SetGameMode(GameMode)
	// SetOpLevel makes the player an operator (0 removes them).
	SetOpLevel(level int)
	// AddAttachment attaches a transient permission set to this player,
	// owned by the calling plugin and removed when the plugin is disabled
	// or the player leaves. Answers nil if the player is no longer online.
	// Bukkit: Permissible.addAttachment
	AddAttachment() PermissionAttachment
	// Kick disconnects the player with reason, firing PlayerKickEvent.
	Kick(reason Text)
	// Transfer sends the player to another server (the Transfer packet).
	Transfer(host string, port int)

	// Chat sends msg as the player chatting; a message starting with a
	// slash runs it as a command.
	Chat(msg string)
	// RunCommand runs line, without its slash, as the player.
	RunCommand(line string) bool
	SendActionBar(msg Text)
	// SendTitle shows a title and subtitle, fading in, staying and fading
	// out over the ticks given. When all three are negative the client's
	// current timings are kept.
	SendTitle(title, subtitle Text, fadeIn, stay, fadeOut int)
	ClearTitle()
	// ShowDialog opens a dialog of the server's data packs.
	ShowDialog(dialog Key)
	PlaySound(pos Vec3, s Sound, cat SoundCategory, volume, pitch float32)
	StopSound(s Sound, cat SoundCategory)
	// SpawnParticles shows particles to this player only.
	SpawnParticles(p Particle, pos Vec3, count int, offset Vec3, speed float64)
	// SendBlockChange shows the player a state at pos that the world does
	// not have, until the block next changes or the chunk is resent.
	SendBlockChange(pos BlockPos, s BlockState)
	// SendPluginMessage sends data on a plugin channel.
	SendPluginMessage(channel Key, data []byte)
	// SendResourcePack asks the client to load a pack, answered by
	// PlayerResourcePackStatusEvent; required kicks a player who declines.
	SendResourcePack(id UUID, url, sha1 string, required bool, prompt *Text)
	RemoveResourcePack(id UUID)
	// StoreCookie and RequestCookie keep data on the client across
	// transfers (the cookie packets); the answer arrives in fn.
	StoreCookie(key Key, data []byte)
	RequestCookie(key Key, fn func(data []byte, ok bool))

	Inventory() PlayerInventory
	EnderChest() Inventory
	// OpenInventory shows a menu made by Server.CreateInventory, or another
	// player's Inventory or EnderChest as a chest menu that follows the
	// owner's slots both ways within a tick (/invsee, /enderchest: the 36
	// slots hotbar first, then boots, leggings, chestplate, helmet, offhand),
	// closing when the owner leaves. It fires InventoryOpenEvent; any other
	// inventory is not shown, and answers nil, as does a cancelled event.
	OpenInventory(inv Inventory) InventoryView
	// OpenView answers the inventory view the player has open, if any.
	OpenView() (InventoryView, bool)
	CloseInventory()
	// Cursor is the stack the player holds on the cursor in a view.
	Cursor() ItemStack
	SetCursor(ItemStack)
	// Cooldown answers the ticks left on an item's cooldown group.
	Cooldown(group Key) int
	SetCooldown(group Key, ticks int)

	Food() int
	SetFood(int)
	Saturation() float32
	SetSaturation(float32)
	Exhaustion() float32
	SetExhaustion(float32)
	// Level and Progress are the experience bar; TotalExperience the points
	// earned.
	Level() int
	SetLevel(int)
	Progress() float32
	SetProgress(float32)
	TotalExperience() int
	GiveExperience(points int)
	// SetTotalExperience restarts the bar from zero and grants points, with
	// no PlayerExpChangeEvent. SetExperience sets the points within the
	// current level, refusing the level's full requirement or more.
	SetTotalExperience(points int)
	Experience() int
	SetExperience(points int) bool

	// FallDistance is the blocks fallen since the player last stood on
	// something; setting it changes the next landing's damage.
	FallDistance() float64
	SetFallDistance(blocks float64)
	// PlayerTime answers the clock the player sees, and whether it is held
	// still (fixed) or differs from the world's (overridden).
	// SetPlayerTime shows a fixed clock, or the world's plus ticks when
	// relative; Reset shows the world's again. Bukkit: setPlayerTime
	PlayerTime() (ticks int64, fixed, overridden bool)
	SetPlayerTime(ticks int64, relative bool)
	ResetPlayerTime()
	// PlayerWeather answers the weather the player is shown instead of the
	// world's; SetPlayerWeather shows rain or a clear sky, whatever the
	// world does. Bukkit: setPlayerWeather
	PlayerWeather() (raining, overridden bool)
	SetPlayerWeather(raining bool)
	ResetPlayerWeather()
	// CompassTarget is where compasses point, the world spawn until
	// SetCompassTarget; nil restores the world spawn.
	CompassTarget() Location
	SetCompassTarget(loc *Location)

	Flying() bool
	SetFlying(bool)
	AllowFlight() bool
	SetAllowFlight(bool)
	// FlySpeed and WalkSpeed are on Bukkit's scale, from -1 to 1: a new
	// player flies at 0.1 and walks at 0.2. The setters clamp to that range.
	FlySpeed() float32
	SetFlySpeed(float32)
	WalkSpeed() float32
	SetWalkSpeed(float32)
	Sneaking() bool
	Sprinting() bool
	SetRespawnPoint(loc *Location, forced bool)

	// CanSee reports whether this player is shown other; Hide and Show
	// change it for this player only. Bukkit: hidePlayer, hideEntity
	CanSee(other Entity) bool
	Hide(other Entity)
	Show(other Entity)
	// Scoreboard answers the scoreboard the player is shown, the main one
	// unless SetScoreboard changed it.
	Scoreboard() Scoreboard
	SetScoreboard(Scoreboard)

	Advancements(a Advancement) AdvancementProgress
	// Recipes answers the keys of the recipes the player has unlocked.
	// Bukkit: getDiscoveredRecipes
	Recipes() []Key
	// UnlockRecipes and LockRecipes change the recipe book as /recipe give
	// and take do, answering how many changed; with silent set the client
	// shows no "new recipe" toast. Bukkit: discoverRecipes, undiscoverRecipes
	UnlockRecipes(keys []Key, silent bool) int
	LockRecipes(keys []Key) int
	Statistic(s Statistic) int
	SetStatistic(s Statistic, v int)
	// Spectating answers the entity a spectator views through.
	Spectating() (Entity, bool)
	SetSpectating(Entity)
}

// GameMode is a player's game mode.
type GameMode uint8

const (
	Survival GameMode = iota
	Creative
	Adventure
	Spectator
)

// AdvancementProgress is a player's progress in one advancement.
type AdvancementProgress interface {
	Done() bool
	Awarded() []string
	Remaining() []string
	// Award and Revoke change one criterion, as /advancement does.
	Award(criterion string) bool
	Revoke(criterion string) bool
	// SetAwarded makes exactly these criteria obtained, silently: no toast,
	// chat line, reward or event. Unknown names are ignored.
	SetAwarded(criteria []string)
}
