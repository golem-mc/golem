package plugin

import "net"

// ServerLoadEvent fires once every plugin is enabled, before the listener
// opens. Bukkit: ServerLoadEvent
type ServerLoadEvent struct{ Base }

// PluginEnableEvent and PluginDisableEvent fire after a plugin is enabled
// and before it is disabled.
type PluginEnableEvent struct {
	Base
	Plugin Info
}

type PluginDisableEvent struct {
	Base
	Plugin Info
}

// ServerTickStartEvent fires at the start of every server tick, ServerTickEndEvent at
// its end with the time it took. Paper: ServerTickStartEvent
type ServerTickStartEvent struct {
	Base
	Tick int64
}

type ServerTickEndEvent struct {
	Base
	Tick int64
	// MSPT is the tick's duration in milliseconds.
	MSPT float64
}

// ServerListPingEvent fires for a server list ping, on the connection's
// goroutine; its fields may be changed. Bukkit: ServerListPingEvent
type ServerListPingEvent struct {
	Cancellable // closes the connection without an answer
	Address     net.Addr
	// Protocol is the client's protocol version.
	Protocol int
	MOTD     Text
	// Online and Max are the counts shown; Sample the names in the tooltip.
	Online, Max int
	Sample      []Profile
	HidePlayers bool
	// Favicon is a 64x64 PNG; nil for the server's.
	Favicon []byte
}

// ServerCommandEvent fires for a command typed at the console, before it
// runs; Command (without its slash) may be changed. Bukkit:
// ServerCommandEvent
type ServerCommandEvent struct {
	Cancellable
	Command string
}

// RemoteServerCommandEvent fires for a command from RCON or the management
// protocol. Bukkit: RemoteServerCommandEvent
type RemoteServerCommandEvent struct {
	Cancellable
	Address net.Addr
	Command string
}

// BroadcastMessageEvent fires for a message sent to every player (Server.Broadcast,
// /say); Message may be changed. Bukkit: BroadcastMessageEvent
type BroadcastMessageEvent struct {
	Cancellable
	Message Text
}

// PlayerCustomClickEvent fires when a player clicks a text or dialog button
// with a custom action (the Custom Click Action packet). Paper:
// PlayerCustomClickEvent
type PlayerCustomClickEvent struct {
	Base
	Player  Player
	ID      Key
	Payload []byte
}
