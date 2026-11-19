package command

import (
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
)

// Version is the /version command.
var Version = Command{
	Name:  "version",
	Level: PermLevelGamemasters,
	Build: func() *Node {
		return &Node{Type: NodeLiteral, Name: "version", Executable: true, Required: PermLevelGamemasters}
	},
	Run: runVersion,
}

// The build the vanilla 26.3 server reports, off its version.json.
const (
	versionDataVersion = 5023
	versionSeries      = "main"
	// versionBuildTime is java.util.Date.toString of the build_time, in UTC.
	versionBuildTime    = "Tue Sep 15 11:20:48 UTC 2026"
	versionResourcePack = "97.1"
	versionDataPack     = "121.0"
)

// runVersion prints the version info as plain system messages, not command
// feedback, so no operator is told of it.
// vanilla: VersionCommand
func runVersion(src Source, input string, args []string) error {
	line := func(key string, with ...string) error {
		t := protocol.Text{Translate: key}
		for _, w := range with {
			t.With = append(t.With, protocol.Text{Text: w})
		}
		return src.Message(t)
	}
	proto := strconv.Itoa(protocol.Version)
	for _, l := range []struct {
		key  string
		with []string
	}{
		{"commands.version.header", nil},
		{"commands.version.id", []string{protocol.GameVersion}},
		{"commands.version.name", []string{protocol.GameVersion}},
		{"commands.version.data", []string{strconv.Itoa(versionDataVersion)}},
		{"commands.version.series", []string{versionSeries}},
		{"commands.version.protocol", []string{proto, "0x" + strconv.FormatInt(protocol.Version, 16)}},
		{"commands.version.build_time", []string{versionBuildTime}},
		{"commands.version.pack.resource", []string{versionResourcePack}},
		{"commands.version.pack.data", []string{versionDataPack}},
		{"commands.version.stable.yes", nil},
	} {
		if err := line(l.key, l.with...); err != nil {
			return err
		}
	}
	return nil
}
