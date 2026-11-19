package management

import (
	"time"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// OnlinePlayer is a connected player as the management server sees them.
type OnlinePlayer struct {
	Profile player.Profile
	IP      string
}

// Operator is one entry of the operator list. vanilla: ServerOpListEntry
type Operator struct {
	Profile             player.Profile
	Level               int
	BypassesPlayerLimit bool
}

// UserBan is one entry of the player ban list; a zero Expires never lapses.
// vanilla: UserBanListEntry
type UserBan struct {
	Profile player.Profile
	Reason  string
	Source  string
	Expires time.Time
}

// IPBan is one entry of the address ban list. vanilla: IpBanListEntry
type IPBan struct {
	IP      string
	Reason  string
	Source  string
	Expires time.Time
}

// Host is what the management methods ask of the game server.
type Host interface {
	// OnlinePlayers returns the connected players in join order.
	OnlinePlayers() []OnlinePlayer
	// SaveAll saves everything and reports whether it succeeded.
	SaveAll(flush bool) bool
	RequestStop()
	TellAll(text protocol.Text, overlay bool)
	Tell(id uuid.UUID, text protocol.Text, overlay bool)

	// LookupProfile resolves a request's player from an id, a name or both;
	// false when it names nobody. vanilla: MinecraftPlayerListService.getUser
	LookupProfile(id *uuid.UUID, name string) (player.Profile, bool)

	// Allowlist is the white-list in file order.
	Allowlist() []player.Profile
	AllowlistAdd(p player.Profile)
	AllowlistRemove(p player.Profile)
	AllowlistClear()
	// KickUnlisted disconnects the online players the enforced white-list
	// does not name. vanilla: MinecraftServer.kickUnlistedPlayers
	KickUnlisted()

	// Operators is the operator list in file order.
	Operators() []Operator
	// DefaultOpLevel is op-permission-level.
	DefaultOpLevel() int
	// OpPut writes the entry, replacing the player's own, and tells an
	// online player their level.
	OpPut(op Operator)
	// Deop removes the player's entry and tells them if online.
	Deop(p player.Profile)
	// OpsClear removes every entry and tells the online players removed.
	OpsClear()

	// Bans is the player ban list in file order.
	Bans() []UserBan
	// BanPut writes the entry, replacing the player's own, created now.
	BanPut(b UserBan)
	BanRemove(p player.Profile)
	BansClear()

	// IPBans is the address ban list in file order.
	IPBans() []IPBan
	// IPBanPut writes the entry, replacing the address's own, created now.
	IPBanPut(b IPBan)
	IPBanRemove(ip string)
	IPBansClear()

	// Kick disconnects the online player with reason; false when they are
	// not online.
	Kick(id uuid.UUID, reason protocol.Text) bool

	// GameRules is the offered rules with their current values.
	GameRules() []GameRule
	// SetGameRule sets the rule and sends what the change owes players and
	// clients.
	SetGameRule(r GameRule)
	// BoolSetting, IntSetting and StringSetting read the serversettings
	// value called name; each Set applies and persists it and returns the
	// value now in force.
	BoolSetting(name string) bool
	SetBoolSetting(name string, v bool) bool
	IntSetting(name string) int
	SetIntSetting(name string, v int) int
	StringSetting(name string) string
	SetStringSetting(name string, v string) string
}
