package servertest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
)

// SyncBuffer is a writer the log can share between goroutines.
type SyncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *SyncBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *SyncBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

// BanRecord is one line of banned-players.json, in the shape the vanilla
// server writes.
type BanRecord struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	Created string `json:"created"`
	Source  string `json:"source"`
	Expires string `json:"expires"`
	Reason  string `json:"reason,omitempty"`
}

// IpBanRecord is one line of banned-ips.json.
type IpBanRecord struct {
	IP      string `json:"ip"`
	Created string `json:"created"`
	Source  string `json:"source"`
	Expires string `json:"expires"`
	Reason  string `json:"reason,omitempty"`
}

// WhitelistRecord is one line of whitelist.json.
type WhitelistRecord struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// WriteAdminFiles writes the three administrative list files into a directory
// of the test's own and points the server at them before it takes a
// connection, which is when they are first read.
func WriteAdminFiles(t *testing.T, bans []BanRecord, ipBans []IpBanRecord, whitelist []WhitelistRecord) func(*server.Server) {
	t.Helper()
	dir := t.TempDir()
	playersPath := filepath.Join(dir, "banned-players.json")
	ipsPath := filepath.Join(dir, "banned-ips.json")
	whitelistPath := filepath.Join(dir, "whitelist.json")
	configPath := filepath.Join(dir, "server.properties")
	WriteJSONList(t, playersPath, bans)
	WriteJSONList(t, ipsPath, ipBans)
	WriteJSONList(t, whitelistPath, whitelist)
	return func(s *server.Server) {
		s.SetBannedPlayersFile(playersPath)
		s.SetBannedIPsFile(ipsPath)
		s.SetWhitelistFile(whitelistPath)
		s.SetConfigFile(configPath)
	}
}

// WritePackFile writes body to path, relative to the data folder of the data
// pack named pack in cfg's level, along with the pack's pack.mcmeta; call it
// before the server starts or before a /reload.
func WritePackFile(t *testing.T, cfg *config.Config, pack, path, body string) {
	t.Helper()
	root := filepath.Join(cfg.LevelName, "datapacks", pack)
	for name, data := range map[string]string{
		"pack.mcmeta":               `{"pack":{"description":"` + pack + `"}}`,
		filepath.Join("data", path): body,
	} {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func WriteJSONList[T any](t *testing.T, path string, entries []T) {
	t.Helper()
	if entries == nil {
		entries = []T{}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ReadListFile parses the JSON list file at path.
func ReadListFile[T any](t *testing.T, path string) []T {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var entries []T
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return entries
}

// AdminSolo makes Alice an operator and brings her into the world alone, with
// the list files pointed where the setup says. The connect function opens
// further connections to the same server.
func AdminSolo(t *testing.T, cfg *config.Config, setup ...func(*server.Server)) (*server.Server, func() *Client, *Client) {
	t.Helper()
	setups := append([]func(*server.Server){OpsListing(t, "Alice")}, setup...)
	srv, connect := Start(t, cfg, setups...)

	alice := connect().Join("Alice")
	return srv, connect, alice
}

// AdminPair is AdminSolo with Bob joining beside Alice.
func AdminPair(t *testing.T, cfg *config.Config, setup ...func(*server.Server)) (*server.Server, func() *Client, *Client, *Client) {
	t.Helper()
	srv, connect, alice := AdminSolo(t, cfg, setup...)

	bob := connect().Join("Bob")

	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")
	return srv, connect, alice, bob
}

// FeedbackComponent reads the next System Chat message and decodes the
// component it carries, which the tests whose argument is a display name they
// cannot spell by hand check field by field.
func FeedbackComponent(t *testing.T, c *Client) protocol.Text {
	t.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySystemChat))
	component, ok := protocol.ReadText(r)
	if !ok {
		t.Fatalf("decode system chat component: %v", r.Err())
	}
	return component
}

// ExpectDisconnect fails unless the next packet the client reads is a Play
// Disconnect carrying the component.
func ExpectDisconnect(t *testing.T, c *Client, want protocol.Text) {
	t.Helper()
	var b protocol.Buffer
	b.PutText(want)
	if got := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(got, b.Bytes()) {
		t.Errorf("disconnect = % X, want % X", got, b.Bytes())
	}
}

// NumberText is a number as a feedback argument, the shape a count travels in.
func NumberText(n int64) protocol.Text { return protocol.Text{Number: &n} }

// ExpectSystemText reads the next System Chat message and checks it is want.
func (c *Client) ExpectSystemText(want protocol.Text) {
	c.T.Helper()
	var b protocol.Buffer
	b.PutText(want)
	b.PutBool(false)
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	if !bytes.Equal(data, b.Bytes()) {
		got, _ := protocol.ReadText(protocol.NewReader(data))
		c.T.Errorf("system chat = %+v, want %+v", got, want)
	}
}

// OpRecord is one line of ops.json, in the shape the vanilla server writes.
type OpRecord struct {
	UUID                string `json:"uuid"`
	Name                string `json:"name"`
	Level               int    `json:"level"`
	BypassesPlayerLimit bool   `json:"bypassesPlayerLimit"`
}

// WriteOpsFile writes an operator list in the vanilla shape and points a
// server that has not taken a connection yet at it. The returned setup runs
// before any connection, which is when the list is first read.
func WriteOpsFile(t *testing.T, records ...OpRecord) func(*server.Server) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ops.json")
	if len(records) > 0 {
		data, err := json.MarshalIndent(records, "", "  ")
		if err != nil {
			t.Fatalf("encode ops.json: %v", err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write ops.json: %v", err)
		}
	}
	return func(s *server.Server) { s.SetOpsFile(path) }
}

// OpsListing is WriteOpsFile with the named players listed as operators of
// the level operators are granted, which is what the tests that run commands
// set up: a player the list does not name holds no permission level at all,
// a missing ops.json reading as an empty list.
func OpsListing(t *testing.T, names ...string) func(*server.Server) {
	t.Helper()
	records := make([]OpRecord, 0, len(names))
	for _, name := range names {
		records = append(records, OpRecord{
			UUID:  protocol.OfflineUUID(name).String(),
			Name:  name,
			Level: config.Default().OpPermissionLevel,
		})
	}
	return WriteOpsFile(t, records...)
}

// SkipJoin reads the rest of a join sequence whose start the test has already
// read past: everything from where the reading stopped to the end of the
// first chunk batch, answering keep-alives on the way.
func (c *Client) SkipJoin() {
	c.T.Helper()
	c.ReadJoin(func(int32, []byte) {})
}

// ExpectCommandFeedback reads the next System Chat message and checks it is
// the translation the given key carries with the given plain-text arguments,
// which is the shape every command's success and failure message takes.
func (c *Client) ExpectCommandFeedback(key string, args ...string) {
	c.T.Helper()
	CheckCommandFeedback(c.T, c.ExpectPlay(protocol.IDPlaySystemChat), key, FeedbackArgsForTest(key, args)...)
}

// ExpectCommandFeedbackText is ExpectCommandFeedback with the arguments
// given as the components they travel as, which is what a feedback that
// counts by a number or names an entity by its display name carries.
func (c *Client) ExpectCommandFeedbackText(key string, with ...protocol.Text) {
	c.T.Helper()
	CheckCommandFeedback(c.T, c.ExpectPlay(protocol.IDPlaySystemChat), key, with...)
}

// CheckCommandFeedback fails the test unless the System Chat payload is the
// translation the key carries with the given components, shown in the chat
// window.
func CheckCommandFeedback(t *testing.T, data []byte, key string, with ...protocol.Text) {
	t.Helper()
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: key, With: with})
	want.PutBool(false) // in the chat window, not over the hotbar
	if !bytes.Equal(data, want.Bytes()) {
		t.Errorf("command feedback = % X, want % X", data, want.Bytes())
	}
}

// SeedFromFeedback reads the seed out of the /seed feedback, whose number
// sits inside the square-bracket click-to-copy wrapper.
func SeedFromFeedback(t *testing.T, feedback protocol.Text) int64 {
	t.Helper()
	if feedback.Translate != "commands.seed.success" || len(feedback.With) != 1 || len(feedback.With[0].With) != 1 {
		t.Fatalf("/seed answered %+v, want the bracketed seed", feedback)
	}
	seed, err := strconv.ParseInt(feedback.With[0].With[0].Text, 10, 64)
	if err != nil {
		t.Fatalf("/seed answered %+v, want the seed", feedback)
	}
	return seed
}

// ExpectSeed reads the next System Chat message as the /seed feedback and
// returns the seed it names.
func (c *Client) ExpectSeed() int64 {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySystemChat))
	text, ok := protocol.ReadText(r)
	if !ok || r.Err() != nil {
		c.T.Fatalf("/seed feedback is no text component")
	}
	return SeedFromFeedback(c.T, text)
}

// TestCommandNode is one node of a decoded Commands packet, with the fields
// the tests care about.
type TestCommandNode struct {
	Typ        byte // 0 root, 1 literal, 2 argument
	Name       string
	Children   []int32
	Executable bool
	Parser     int32 // the argument's parser registry id, -1 on other nodes
	Redirect   int32 // -1 when the node redirects nowhere
	TimeMin    int32 // a minecraft:time argument's floor in ticks
}

// DecodeCommands parses a Commands packet the way a client does, down
// through the parser properties, so that a malformed tree shows up as a read
// error rather than as a wrong list of names. The parsers it has to skip
// past are the ones this server sends; an unknown one fails the test, since
// a client would have to stop unpacking there.
func DecodeCommands(t *testing.T, data []byte) []TestCommandNode {
	t.Helper()
	r := protocol.NewReader(data)
	count := r.ReadVarInt()
	nodes := make([]TestCommandNode, 0, count)
	for range count {
		flags := r.ReadU8()
		childCount := r.ReadVarInt()
		children := make([]int32, 0, childCount)
		for range childCount {
			children = append(children, r.ReadVarInt())
		}
		redirect := int32(-1)
		if flags&0x08 != 0 {
			redirect = r.ReadVarInt()
		}
		node := TestCommandNode{Typ: flags & 0x03, Children: children, Executable: flags&0x04 != 0,
			Parser: -1, Redirect: redirect}
		switch flags & 0x03 {
		case 1, 2:
			node.Name = r.ReadString(32767)
		}
		if flags&0x03 == 2 {
			node.Parser = r.ReadVarInt()
			if node.Parser == 43 { // minecraft:time
				node.TimeMin = r.ReadI32()
			} else {
				SkipCommandParserProperties(t, r, node.Parser)
			}
		}
		if flags&0x10 != 0 {
			r.ReadString(32767) // the suggestion type
		}
		nodes = append(nodes, node)
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("commands root index = %d, want 0", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("decode commands: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("commands packet has %d bytes left over", r.Remaining())
	}
	return nodes
}

// SkipCommandParserProperties consumes one argument node's parser-specific
// properties, which is what keeps the reader aligned with the node that
// follows. The shapes are the ones tools/probe/parsers.go lists.
func SkipCommandParserProperties(t *testing.T, r *protocol.Reader, parser int32) {
	t.Helper()
	switch parser {
	case 1: // brigadier:float
		flags := r.ReadU8()
		if flags&0x01 != 0 {
			r.ReadF32()
		}
		if flags&0x02 != 0 {
			r.ReadF32()
		}
	case 2: // brigadier:double
		flags := r.ReadU8()
		if flags&0x01 != 0 {
			r.ReadF64()
		}
		if flags&0x02 != 0 {
			r.ReadF64()
		}
	case 3: // brigadier:integer
		flags := r.ReadU8()
		if flags&0x01 != 0 {
			r.ReadI32()
		}
		if flags&0x02 != 0 {
			r.ReadI32()
		}
	case 4: // brigadier:long
		flags := r.ReadU8()
		if flags&0x01 != 0 {
			r.ReadI64()
		}
		if flags&0x02 != 0 {
			r.ReadI64()
		}
	case 5: // brigadier:string
		r.ReadVarInt() // the parsing behaviour
	case 6, 31: // minecraft:entity, minecraft:score_holder
		r.ReadU8()
	case 43: // minecraft:time
		r.ReadI32()
	case 44, 45, 46, 47, 48: // the resource parsers, each naming its registry
		r.ReadString(32767)
	default:
		// Every other parser this server sends carries no properties; one
		// the protocol has not defined would leave the reader lost, which
		// is what this failure is for.
		if parser < 0 || parser > 61 {
			t.Fatalf("commands packet carries parser %d, which the protocol does not define", parser)
		}
	}
}

// ReadOpsFile reads back what the server wrote, for the tests that check a
// grant or a revoke reached the disk.
func ReadOpsFile(t *testing.T, s *server.Server) []OpRecord {
	t.Helper()
	data, err := os.ReadFile(s.OpsFile())
	if err != nil {
		t.Fatalf("read ops.json: %v", err)
	}
	var records []OpRecord
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("parse ops.json: %v", err)
	}
	return records
}
