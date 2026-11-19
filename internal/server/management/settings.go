package management

import (
	"encoding/json"
	"slices"
	"strings"
)

// settingKind is the JSON type a server setting is read and written as.
type settingKind int

const (
	settingBool settingKind = iota
	settingInt
	settingString
)

// serverSetting is one serversettings/<name> getter and its /set.
type serverSetting struct {
	// name is the method suffix after serversettings/ and the key the Host
	// looks the setting up by.
	name string
	kind settingKind
	// log formats the /set log line from the old and the new value.
	log string
	// clamp, when set, bounds an int before the Host sees it.
	clamp func(int) int
	// logValue, when set, renders an int for log.
	logValue func(int) string
	// enum, when set, is the only strings a string setting takes; the log
	// shows them upper-cased, as Java enum names.
	// vanilla: Codec.stringResolver
	enum []string
}

// The wire names of Schema.DIFFICULTY_SCHEMA and GAME_TYPE_SCHEMA.
var (
	difficultyNames = []string{"peaceful", "easy", "normal", "hard"}
	gameTypeNames   = []string{"survival", "creative", "adventure", "spectator"}
)

// opLevelNames are PermissionLevel's names for levels 0-4.
var opLevelNames = [...]string{"ALL", "MODERATORS", "GAMEMASTERS", "ADMINS", "OWNERS"}

// serverSettings are the settings served, in schema order.
// vanilla: MinecraftServerSettingsServiceImpl
var serverSettings = []serverSetting{
	{name: "autosave", kind: settingBool, log: "Update autosave from %t to %t"},
	{name: "difficulty", kind: settingString, log: "Update difficulty from '%s' to '%s'", enum: difficultyNames},
	{name: "enforce_allowlist", kind: settingBool, log: "Update enforce allowlist from %t to %t"},
	{name: "use_allowlist", kind: settingBool, log: "Update using allowlist from %t to %t"},
	{name: "max_players", kind: settingInt, log: "Update max players from %d to %d"},
	{name: "motd", kind: settingString, log: "Update MOTD from '%s' to '%s'"},
	{name: "spawn_protection_radius", kind: settingInt, log: "Update spawn protection radius from %d to %d"},
	{name: "pause_when_empty_seconds", kind: settingInt, log: "Update pause when empty from %d seconds to %d seconds"},
	// The value is in minutes, though the schema names the param seconds.
	{name: "player_idle_timeout", kind: settingInt,
		log: "Update player idle timeout from %d minutes to %d minutes"},
	{name: "allow_flight", kind: settingBool, log: "Update allow flight from %t to %t"},
	{name: "force_game_mode", kind: settingBool, log: "Update force game mode from %t to %t"},
	{name: "game_mode", kind: settingString, log: "Update game mode from '%s' to '%s'", enum: gameTypeNames},
	{name: "view_distance", kind: settingInt, log: "Update view distance from %d to %d"},
	{name: "simulation_distance", kind: settingInt, log: "Update simulation distance from %d to %d"},
	{name: "accept_transfers", kind: settingBool, log: "Update accepts transfers from %t to %t"},
	{name: "hide_online_players", kind: settingBool, log: "Update hides online players from %t to %t"},
	{name: "status_replies", kind: settingBool, log: "Update replies to status from %t to %t"},
	{name: "entity_broadcast_range", kind: settingInt, log: "Update entity broadcast range percentage from %d%% to %d%%"},
	{name: "status_heartbeat_interval", kind: settingInt, log: "Updated status heartbeat interval from %d to %d"},
	// vanilla: PermissionLevel.INT_CODEC, which clamps through ByIdMap.
	{name: "operator_user_permission_level", kind: settingInt,
		log:   "Update operator user permission level from permission level: %s to %s",
		clamp: func(v int) int { return min(max(v, 0), 4) },
		logValue: func(v int) string {
			return opLevelNames[min(max(v, 0), len(opLevelNames)-1)]
		}},
}

// settingMethods registers each setting's getter and /set.
// vanilla: IncomingRpcMethods.bootstrap, ServerSettingsService
func (s *Server) settingMethods(m map[string]method) {
	for _, row := range serverSettings {
		name := "minecraft:serversettings/" + row.name
		m[name] = method{run: func(*conn, json.RawMessage) (any, error) {
			return s.getSetting(row), nil
		}}
		m[name+"/set"] = method{run: func(c *conn, p json.RawMessage) (any, error) {
			return s.setSetting(c, row, p)
		}}
	}
}

// getSetting reads the setting's value from the Host.
func (s *Server) getSetting(row serverSetting) any {
	switch row.kind {
	case settingBool:
		return s.host.BoolSetting(row.name)
	case settingInt:
		return s.host.IntSetting(row.name)
	default:
		return s.host.StringSetting(row.name)
	}
}

// setSetting decodes p as the setting's kind, logs the change and returns
// the value in force afterwards.
func (s *Server) setSetting(c *conn, row serverSetting, p json.RawMessage) (any, error) {
	switch row.kind {
	case settingBool:
		var v bool
		if json.Unmarshal(p, &v) != nil {
			return nil, errInvalidParams("Not a boolean: " + compactJSON(p))
		}
		c.logf(row.log, s.host.BoolSetting(row.name), v)
		return s.host.SetBoolSetting(row.name, v), nil
	case settingInt:
		var f float64
		if json.Unmarshal(p, &f) != nil {
			return nil, errInvalidParams("Not a number: " + compactJSON(p))
		}
		v := int(f)
		if row.clamp != nil {
			v = row.clamp(v)
		}
		old := s.host.IntSetting(row.name)
		if row.logValue != nil {
			c.logf(row.log, row.logValue(old), row.logValue(v))
		} else {
			c.logf(row.log, old, v)
		}
		return s.host.SetIntSetting(row.name, v), nil
	default:
		v, err := decodeString(p)
		if err != nil {
			return nil, errInvalidParams(err.Error())
		}
		if row.enum == nil {
			c.logf(row.log, s.host.StringSetting(row.name), v)
			return s.host.SetStringSetting(row.name, v), nil
		}
		if !slices.Contains(row.enum, v) {
			return nil, errInvalidParams("Unknown element name:" + v)
		}
		c.logf(row.log, strings.ToUpper(s.host.StringSetting(row.name)), strings.ToUpper(v))
		return s.host.SetStringSetting(row.name, v), nil
	}
}
