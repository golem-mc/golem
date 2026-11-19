package command

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestTransferTreeMatchesVanilla holds /transfer's nodes to the vanilla
// tree: an executable quotable-phrase hostname, a port bounded to the port
// range, and a players-only entity argument, none of them asking the client
// for suggestions.
// vanilla: TransferCommand.register
func TestTransferTreeMatchesVanilla(t *testing.T) {
	if Transfer.Level != PermLevelAdmins {
		t.Errorf("transfer permission = %d, want the admins level", Transfer.Level)
	}
	hostname := Transfer.Build().Children[0]
	if !hostname.Executable || hostname.Parser != ParserString || !hostname.Quotable {
		t.Errorf("hostname node = %+v, want an executable quotable string", hostname)
	}
	if hostname.Properties == nil {
		t.Fatal("hostname node carries no string-mode properties")
	}
	var b protocol.Buffer
	hostname.Properties(&b)
	if got := protocol.NewReader(b.Bytes()).ReadVarInt(); got != 1 {
		t.Errorf("hostname string mode = %d, want the quotable phrase 1", got)
	}
	port := hostname.Children[0]
	if !port.Executable || port.Int == nil ||
		!port.Int.HasMin || port.Int.Min != 1 ||
		!port.Int.HasMax || port.Int.Max != 65535 {
		t.Errorf("port node = %+v, want an executable integer bounded to 1..65535", port)
	}
	players := port.Children[0]
	if !players.Executable || players.EntityArg == nil || !players.EntityArg.PlayersOnly ||
		players.Suggests != "" {
		t.Errorf("players node = %+v, want an executable players-only argument without suggestions", players)
	}
}
