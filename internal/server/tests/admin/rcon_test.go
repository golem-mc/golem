package admin

import (
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// The RCON wire protocol: each frame is a little-endian length covering the
// rest of it, the request id, the type, the payload and a null terminator
// each for the payload and the frame. Type 3 logs in and type 2 runs a
// command; the answers are type 2 for a login and type 0 for command output.
// vanilla: net.minecraft.server.rcon.thread.RconClient
const (
	rconPacketAuth    = 3
	rconPacketCommand = 2
	rconPacketLogin   = 2
	rconPacketOutput  = 0
)

// rconConn drives an RCON connection the way a remote console client does.
type rconConn struct {
	t    *testing.T
	conn net.Conn
}

// dialRcon serves one RCON connection from srv and connects a client to it,
// the listener-per-connection pattern the harness serves game connections
// with.
func dialRcon(t *testing.T, srv *server.Server) *rconConn {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	served := make(chan struct{})
	go func() {
		defer close(served)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		srv.ServeRcon(conn)
	}()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		conn.Close()
		<-served
	})
	return &rconConn{t: t, conn: conn}
}

// send writes one request whose request id the answer echoes.
func (c *rconConn) send(id, typ int32, payload string) {
	c.t.Helper()
	frame := make([]byte, 0, 14+len(payload))
	var field [4]byte
	binary.LittleEndian.PutUint32(field[:], uint32(8+len(payload)+2))
	frame = append(frame, field[:]...)
	binary.LittleEndian.PutUint32(field[:], uint32(id))
	frame = append(frame, field[:]...)
	binary.LittleEndian.PutUint32(field[:], uint32(typ))
	frame = append(frame, field[:]...)
	frame = append(frame, payload...)
	frame = append(frame, 0, 0)
	if _, err := c.conn.Write(frame); err != nil {
		c.t.Fatalf("write rcon packet: %v", err)
	}
}

// sendShort writes the shortest frame vanilla accepts: a length field of 8
// covering only the id and the type, with no payload and no terminator pair.
func (c *rconConn) sendShort(id, typ int32) {
	c.t.Helper()
	frame := make([]byte, 0, 12)
	var field [4]byte
	for _, word := range []uint32{8, uint32(id), uint32(typ)} {
		binary.LittleEndian.PutUint32(field[:], word)
		frame = append(frame, field[:]...)
	}
	if _, err := c.conn.Write(frame); err != nil {
		c.t.Fatalf("write short rcon packet: %v", err)
	}
}

// rconPacket is one answer read off the wire.
type rconPacket struct {
	id      int32
	typ     int32
	payload string
}

// receive reads one answer and fails the test unless its shape is the
// protocol's: the length covering exactly what arrives, the two trailing
// nulls, and the payload ending at the first of them.
func (c *rconConn) receive() rconPacket {
	c.t.Helper()
	if err := c.conn.SetReadDeadline(time.Now().Add(servertest.ExpectTimeout)); err != nil {
		c.t.Fatalf("read deadline: %v", err)
	}
	var head [4]byte
	if _, err := io.ReadFull(c.conn, head[:]); err != nil {
		c.t.Fatalf("read rcon packet: %v", err)
	}
	length := int(binary.LittleEndian.Uint32(head[:]))
	if length < 10 {
		c.t.Fatalf("rcon packet length = %d, want room for the tail", length)
	}
	frame := make([]byte, length)
	if _, err := io.ReadFull(c.conn, frame); err != nil {
		c.t.Fatalf("read rcon packet body: %v", err)
	}
	if len(frame) < 2 || frame[len(frame)-1] != 0 || frame[len(frame)-2] != 0 {
		c.t.Fatalf("rcon packet tail = % X, want two null bytes", frame[len(frame)-2:])
	}
	p := rconPacket{
		id:  int32(binary.LittleEndian.Uint32(frame[0:4])),
		typ: int32(binary.LittleEndian.Uint32(frame[4:8])),
	}
	payload := frame[8:]
	if nul := byteIndex(payload, 0); nul >= 0 {
		payload = payload[:nul]
	}
	p.payload = string(payload)
	return p
}

// byteIndex is strings.IndexByte for a byte slice.
func byteIndex(b []byte, want byte) int {
	for i, c := range b {
		if c == want {
			return i
		}
	}
	return -1
}

// rconStart starts a server with the RCON password set, the way
// enable-rcon and rcon.password do in server.properties.
func rconStart(t *testing.T) (*server.Server, *rconConn) {
	t.Helper()
	cfg := config.Default()
	cfg.EnableRcon = true
	cfg.RconPassword = "sesame"
	srv, _ := servertest.Start(t, cfg)
	return srv, dialRcon(t, srv)
}

// TestRconLoginRunsACommandAndReturnsItsOutput logs in with the password and
// runs list, which has to answer through the owner-level console source:
// the /list output with nobody online.
func TestRconLoginRunsACommandAndReturnsItsOutput(t *testing.T) {
	t.Parallel()
	_, c := rconStart(t)

	// A command before the login is answered as a failed one.
	c.send(1, rconPacketCommand, "list")
	if got := c.receive(); got.id != -1 || got.typ != rconPacketLogin || got.payload != "" {
		t.Errorf("unauthenticated command answered %+v, want id -1, type 2, no payload", got)
	}

	// The login echoes the request id.
	c.send(2, rconPacketAuth, "sesame")
	if got := c.receive(); got.id != 2 || got.typ != rconPacketLogin || got.payload != "" {
		t.Errorf("login answered %+v, want the id echoed, type 2, no payload", got)
	}

	// list answers with the console's line for an empty world.
	c.send(3, rconPacketCommand, "list")
	want := "There are 0 of a max of 20 players online: "
	if got := c.receive(); got.id != 3 || got.typ != rconPacketOutput || got.payload != want {
		t.Errorf("list answered %+v, want id 3, type 0, %q", got, want)
	}
}

// TestRconWrongPasswordIsAnsweredWithMinusOne logs in with the wrong
// password and has to get the request id -1 back, in the answer the
// connection can then log in on anyway.
// vanilla: RconClient.sendAuthFailure
func TestRconWrongPasswordIsAnsweredWithMinusOne(t *testing.T) {
	t.Parallel()
	_, c := rconStart(t)

	c.send(1, rconPacketAuth, "open sesame")
	if got := c.receive(); got.id != -1 || got.typ != rconPacketLogin || got.payload != "" {
		t.Errorf("wrong password answered %+v, want id -1, type 2, no payload", got)
	}

	// A refused login leaves the connection open: the right password on
	// the same one works.
	c.send(2, rconPacketAuth, "sesame")
	if got := c.receive(); got.id != 2 || got.typ != rconPacketLogin {
		t.Errorf("login after a refused one answered %+v, want the id echoed", got)
	}

	// A request type the protocol does not know is answered rather than
	// dropped, with the type in hex.
	c.send(3, 5, "hi")
	if got := c.receive(); got.id != 3 || got.typ != rconPacketOutput || got.payload != "Unknown request 5" {
		t.Errorf("unknown request answered %+v, want %q", got, "Unknown request 5")
	}
}

// TestRconRunsCommandsAsTheRconSource has RCON run an operator's command
// and a line the parse refuses, and checks both the level the commands ran
// at — /op is beyond a plain player — and that a line with several lines of
// console output has them back glued as RunLine answered them, nothing
// between. The output matches the console's because both run as the owner
// level; only the source's name differs.
func TestRconRunsCommandsAsTheRconSource(t *testing.T) {
	t.Parallel()
	ops := filepath.Join(t.TempDir(), "ops.json")
	cfg := config.Default()
	cfg.OnlineMode = false // in offline mode a typed name resolves to a profile
	cfg.EnableRcon = true
	cfg.RconPassword = "sesame"
	srv, _ := servertest.Start(t, cfg, func(s *server.Server) { s.SetOpsFile(ops) })
	c := dialRcon(t, srv)

	c.send(1, rconPacketAuth, "sesame")
	if got := c.receive(); got.id != 1 {
		t.Fatalf("login answered id %d, want 1", got.id)
	}

	// /op needs a level the connection only has as the console source.
	c.send(2, rconPacketCommand, "op Steve")
	if got := c.receive(); got.id != 2 || got.typ != rconPacketOutput || got.payload != "Made Steve a server operator" {
		t.Errorf("op Steve answered %+v, want the console feedback", got)
	}
	list, err := os.ReadFile(ops)
	if err != nil {
		t.Fatalf("read ops.json: %v", err)
	}
	if !strings.Contains(string(list), "Steve") {
		t.Errorf("ops.json = %s, want Steve listed", list)
	}

	// A line the parse refuses answers with the same lines the console
	// gets, glued: the message and its context line with nothing between.
	c.send(3, rconPacketCommand, "nonsense")
	var want strings.Builder
	for _, out := range srv.RunLine("nonsense") {
		want.WriteString(out.Plain())
	}
	if got := c.receive(); got.id != 3 || got.typ != rconPacketOutput || got.payload != want.String() {
		t.Errorf("nonsense answered %+v, want the lines RunLine answers: %q", got, want.String())
	}
	if !strings.Contains(want.String(), "or incomplete command") {
		t.Errorf("the refusal %q does not look like the unknown-command answer", want.String())
	}

	// A line that produced nothing is an empty answer, not silence.
	c.send(4, rconPacketCommand, "say hi")
	if got := c.receive(); got.id != 4 || got.typ != rconPacketOutput || got.payload != "" {
		t.Errorf("say answered %+v, want an empty answer", got)
	}
}

// TestRconEmptyCommandIsRefused runs the empty command: vanilla's dispatcher
// refuses an empty line with the unknown-command message and its context,
// not with silence. vanilla: Commands.performCommand
func TestRconEmptyCommandIsRefused(t *testing.T) {
	t.Parallel()
	_, c := rconStart(t)

	c.send(1, rconPacketAuth, "sesame")
	if got := c.receive(); got.id != 1 {
		t.Fatalf("login answered id %d, want 1", got.id)
	}

	c.send(2, rconPacketCommand, "")
	got := c.receive()
	if got.id != 2 || got.typ != rconPacketOutput {
		t.Fatalf("empty command answered %+v, want id 2, type 0", got)
	}
	if got.payload != "Unknown or incomplete command. See below for error<--[HERE]" {
		t.Errorf("empty command answered %q, want the unknown-command refusal", got.payload)
	}
}

// TestRconShortFrameIsAnswered sends the shortest frame vanilla accepts, a
// length of 8 covering only the id and the type: it is answered as an
// unknown request, not dropped by a minimum that counted the length prefix
// twice. vanilla: RconClient.run
func TestRconShortFrameIsAnswered(t *testing.T) {
	t.Parallel()
	_, c := rconStart(t)

	c.send(1, rconPacketAuth, "sesame")
	if got := c.receive(); got.id != 1 {
		t.Fatalf("login answered id %d, want 1", got.id)
	}

	c.sendShort(2, 5)
	if got := c.receive(); got.id != 2 || got.typ != rconPacketOutput || got.payload != "Unknown request 5" {
		t.Errorf("short frame answered %+v, want %q", got, "Unknown request 5")
	}
}

// TestRconAdminEchoNamesRcon runs /op over RCON while an operator is in the
// world: the operator reads the admin echo under vanilla's Rcon source name,
// not under Server.
func TestRconAdminEchoNamesRcon(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.EnableRcon = true
	cfg.RconPassword = "sesame"
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)

	c := dialRcon(t, srv)
	c.send(1, rconPacketAuth, "sesame")
	if got := c.receive(); got.id != 1 {
		t.Fatalf("login answered id %d, want 1", got.id)
	}
	c.send(2, rconPacketCommand, "op Steve")
	if got := c.receive(); got.id != 2 {
		t.Fatalf("op answered id %d, want 2", got.id)
	}

	servertest.AwaitAdminBroadcast(t, alice, protocol.Text{Text: "Rcon"}, protocol.Text{
		Translate: "commands.op.success",
		With:      []protocol.Text{{Text: "Steve"}},
	})
}

// TestRconAdminEchoFollowsTheProperty turns broadcast-rcon-to-ops off and
// runs /op over RCON: the operator reads no admin echo, since the remote
// console's shouldInformAdmins is that property.
// vanilla: RconConsoleSource.shouldInformAdmins
func TestRconAdminEchoFollowsTheProperty(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.EnableRcon = true
	cfg.RconPassword = "sesame"
	cfg.BroadcastRconToOps = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)

	c := dialRcon(t, srv)
	c.send(1, rconPacketAuth, "sesame")
	if got := c.receive(); got.id != 1 {
		t.Fatalf("login answered id %d, want 1", got.id)
	}
	c.send(2, rconPacketCommand, "op Steve")
	if got := c.receive(); got.id != 2 {
		t.Fatalf("op answered id %d, want 2", got.id)
	}

	servertest.AwaitNoSystemChat(t, alice)
}

// TestConsoleAdminEchoFollowsTheProperty turns broadcast-console-to-ops off
// and runs /op from the console: the operator reads no admin echo.
// vanilla: DedicatedServer.shouldInformAdmins
func TestConsoleAdminEchoFollowsTheProperty(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.BroadcastConsoleToOps = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)

	srv.RunLine("op Steve")

	servertest.AwaitNoSystemChat(t, alice)
}
