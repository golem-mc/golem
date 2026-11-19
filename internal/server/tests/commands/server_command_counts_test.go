package commands

import "testing"

// Server, admin and chat commands answer their count to /execute store, not 1.
func TestServerAdminAndChatCommandsReturnTheirCountToExecuteStore(t *testing.T) {
	srv, c := joinOpForStore(t, "Bob")
	expectStored(t, srv, c, [][2]string{
		{"list", "2"},
		{"list uuids", "2"},
		{`tellraw @a "x"`, "2"},
		{"msg @a hi", "2"},
		{"setidletimeout 7", "7"},
		{"banlist", "0"},
		{"whitelist list", "0"},
		{"defaultgamemode creative", "0"},
		{"stopwatch create t:w", "1"},
		{"stopwatch query t:w 0", "0"},
		{"stopwatch remove t:w", "1"},
	})
}
