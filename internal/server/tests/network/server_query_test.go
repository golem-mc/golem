package network

import (
	"bytes"
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/servertest"
	"net"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// queryConfig is the configuration the query tests run in: the query
// listener on, and a MOTD, port and address of their own so the stat
// answers carry values a test can tell apart.
func queryConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.EnableQuery = true
	cfg.ServerIP = "127.0.0.7"
	cfg.ServerPort = 25571
	cfg.MaxPlayers = 12
	cfg.MOTD = "A Query Test Server"
	return cfg
}

// startQueryConn opens a UDP socket on a free local port and runs the
// server's query answering loop on it, the socket Listen opens when
// enable-query is on. The test owns the socket the way the connection tests
// own their listeners, and so knows the port it is answered on.
func startQueryConn(t *testing.T, srv *server.Server) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen for queries: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go srv.ServeQuery(conn)
	return conn.LocalAddr().(*net.UDPAddr)
}

// dialQuery dials the UDP socket a query client would use.
func dialQuery(t *testing.T, addr *net.UDPAddr) *net.UDPConn {
	t.Helper()
	conn, err := net.DialUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, addr)
	if err != nil {
		t.Fatalf("dial the query listener: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// queryReply reads one datagram within the wait, or returns nil when none
// arrives: the answer to a request the server hears, the silence to one it
// does not.
func queryReply(t *testing.T, conn *net.UDPConn, wait time.Duration) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(wait)); err != nil {
		t.Fatalf("set the query read deadline: %v", err)
	}
	var buf [2048]byte
	n, err := conn.Read(buf[:])
	if err != nil {
		return nil
	}
	return buf[:n]
}

// queryHandshake mints a challenge token and returns it. Every answer the
// following stats get carries this handshake's session before its own: the
// server files the session with the token, not with the stat.
func queryHandshake(t *testing.T, conn *net.UDPConn, session [4]byte) int32 {
	t.Helper()
	if _, err := conn.Write(append([]byte{0xFE, 0xFD, 0x09}, session[:]...)); err != nil {
		t.Fatalf("send a handshake: %v", err)
	}
	reply := queryReply(t, conn, 2*time.Second)
	if reply == nil {
		t.Fatal("the handshake got no answer")
	}
	if reply[0] != 0x09 {
		t.Fatalf("handshake answer type = %#x, want 0x09", reply[0])
	}
	if !bytes.Equal(reply[1:5], session[:]) {
		t.Fatalf("handshake answer echoed % X, want the session % X", reply[1:5], session)
	}
	if got := reply[len(reply)-1]; got != 0 {
		t.Errorf("handshake answer ended in %#x, want a NUL", got)
	}
	token, err := strconv.Atoi(string(reply[5 : len(reply)-1]))
	if err != nil {
		t.Fatalf("challenge token %q is not decimal: %v", reply[5:len(reply)-1], err)
	}
	return int32(token)
}

// statRequest builds a stat datagram: the session, the challenge token it
// must carry big-endian, and one NUL per byte of padding asked for. Four
// bytes of it are a full stat; none is a basic one.
func statRequest(session [4]byte, token int32, padding int) []byte {
	b := make([]byte, 0, 15)
	b = append(b, 0xFE, 0xFD, 0x00)
	b = append(b, session[:]...)
	b = binary.BigEndian.AppendUint32(b, uint32(token))
	return append(b, make([]byte, padding)...)
}

// queryCString reads one NUL-terminated field off the front of data.
func queryCString(t *testing.T, data []byte) (string, []byte) {
	t.Helper()
	end := bytes.IndexByte(data, 0)
	if end < 0 {
		t.Fatalf("query field %q is not NUL-terminated", data)
	}
	return string(data[:end]), data[end+1:]
}

// expectTypeAndSession fails unless the answer carries the stat type and the
// session given, and returns the rest of it.
func expectTypeAndSession(t *testing.T, reply []byte, session [4]byte) []byte {
	t.Helper()
	if reply[0] != 0x00 {
		t.Fatalf("stat answer type = %#x, want 0x00", reply[0])
	}
	if !bytes.Equal(reply[1:5], session[:]) {
		t.Fatalf("stat answer echoed % X, want the session % X", reply[1:5], session)
	}
	return reply[5:]
}

// TestQueryHandshakeReturnsChallengeToken checks the handshake: a token in
// decimal digits, in vanilla's 24-bit range, with the session echoed.
func TestQueryHandshakeReturnsChallengeToken(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, queryConfig())
	q := dialQuery(t, startQueryConn(t, srv))

	token := queryHandshake(t, q, [4]byte{0x0F, 0x12, 0x34, 0x56})
	if token < 0 || token >= 0x1000000 {
		t.Errorf("challenge token = %d, want one of vanilla's 24-bit numbers", token)
	}
	queryHandshake(t, q, [4]byte{4, 3, 2, 1})
}

// TestQueryMapKeepsTheLevelIDUnderAUniverse checks the full stat's map when
// the world's files live beneath a separate universe directory.
// vanilla: QueryThreadGs4, DedicatedServer.getLevelIdName
func TestQueryMapKeepsTheLevelIDUnderAUniverse(t *testing.T) {
	t.Parallel()
	cfg := queryConfig()
	cfg.Universe = t.TempDir()
	cfg.LevelName = "w"
	srv, _ := servertest.Start(t, cfg)
	q := dialQuery(t, startQueryConn(t, srv))
	session := [4]byte{5, 6, 7, 7}
	token := queryHandshake(t, q, session)
	if _, err := q.Write(statRequest(session, token, 4)); err != nil {
		t.Fatalf("send a full stat: %v", err)
	}
	reply := queryReply(t, q, 2*time.Second)
	if !bytes.Contains(reply, []byte("\x00map\x00w\x00")) {
		t.Errorf("full stat does not report map w: %q", reply)
	}
}

// TestQueryBasicStat checks the basic stat answers: the MOTD, game type,
// map, player counts, port and host address, in that order, the port alone
// little endian. A datagram whose length is anything but fifteen bytes
// still answers the basic form, the length vanilla tells the two apart by.
func TestQueryBasicStat(t *testing.T) {
	t.Parallel()
	cfg := queryConfig()
	srv, _ := servertest.Join(t, cfg, "Chair")

	q := dialQuery(t, startQueryConn(t, srv))
	session := [4]byte{9, 9, 9, 9}
	token := queryHandshake(t, q, session)
	if _, err := q.Write(statRequest(session, token, 0)); err != nil {
		t.Fatalf("send a basic stat: %v", err)
	}
	reply := queryReply(t, q, 2*time.Second)
	if reply == nil {
		t.Fatal("the basic stat got no answer")
	}

	rest := expectTypeAndSession(t, reply, session)
	var got []string
	for range 5 {
		var field string
		field, rest = queryCString(t, rest)
		got = append(got, field)
	}
	want := []string{cfg.MOTD, "SMP", cfg.LevelName, "1", strconv.Itoa(cfg.MaxPlayers)}
	if !slices.Equal(got, want) {
		t.Errorf("basic stat = %q, want %q", got, want)
	}

	port := binary.LittleEndian.Uint16(rest[:2])
	rest = rest[2:]
	host, rest := queryCString(t, rest)
	if port != uint16(cfg.ServerPort) {
		t.Errorf("basic stat port = %d, want %d", port, cfg.ServerPort)
	}
	if host != cfg.ServerIP {
		t.Errorf("basic stat host = %q, want %q", host, cfg.ServerIP)
	}
	if len(rest) != 0 {
		t.Errorf("basic stat carried %q after the host", rest)
	}

	// One byte too many is still the short form.
	if _, err := q.Write(statRequest(session, token, 1)); err != nil {
		t.Fatalf("send a padded basic stat: %v", err)
	}
	if reply := queryReply(t, q, 2*time.Second); reply == nil {
		t.Error("a twelve-byte stat got no basic answer")
	} else if rest := expectTypeAndSession(t, reply, session); !bytes.HasPrefix(rest, []byte(cfg.MOTD)) {
		t.Errorf("twelve-byte stat answered %q, want the basic form", rest[:min(len(rest), 40)])
	}
}

// TestQueryReportsTheLocalHostForABindAllAddress checks the reported host:
// server-ip 0.0.0.0 is a bind-all address, not a host, so the stat answers
// the machine's own address as it does for an empty server-ip (vanilla:
// QueryThreadGs4's constructor).
func TestQueryReportsTheLocalHostForABindAllAddress(t *testing.T) {
	t.Parallel()
	readHost := func(serverIP string) string {
		cfg := queryConfig()
		cfg.ServerIP = serverIP
		srv, _ := servertest.Start(t, cfg)
		q := dialQuery(t, startQueryConn(t, srv))
		session := [4]byte{3, 1, 4, 1}
		token := queryHandshake(t, q, session)
		if _, err := q.Write(statRequest(session, token, 0)); err != nil {
			t.Fatalf("send a basic stat: %v", err)
		}
		reply := queryReply(t, q, 2*time.Second)
		if reply == nil {
			t.Fatalf("the basic stat for server-ip %q got no answer", serverIP)
		}
		rest := expectTypeAndSession(t, reply, session)
		for range 5 { // MOTD, game type, map, online, max
			_, rest = queryCString(t, rest)
		}
		rest = rest[2:] // the little-endian port
		host, _ := queryCString(t, rest)
		return host
	}
	if got, local := readHost("0.0.0.0"), readHost(""); got != local {
		t.Errorf("server-ip 0.0.0.0 reported host %q, want the empty server-ip's %q", got, local)
	}
}

// TestQueryFullStatListsThePlayerNames checks the full stat answers: the
// rule set with the MOTD, map, counts, port and address the basic stat also
// carries, and the names of everyone online. The four bytes that ask for
// the full form are counted, not read: any four answer the same.
func TestQueryFullStatListsThePlayerNames(t *testing.T) {
	t.Parallel()
	cfg := queryConfig()
	srv, connect := servertest.Start(t, cfg)

	connect().Join("First")
	connect().Join("Second")

	q := dialQuery(t, startQueryConn(t, srv))
	session := [4]byte{7, 7, 7, 7}
	token := queryHandshake(t, q, session)
	if _, err := q.Write(statRequest(session, token, 4)); err != nil {
		t.Fatalf("send a full stat: %v", err)
	}
	reply := queryReply(t, q, 2*time.Second)
	if reply == nil {
		t.Fatal("the full stat got no answer")
	}

	rest := expectTypeAndSession(t, reply, session)
	prefix := append(append([]byte("splitnum"), 0), 0x80, 0)
	if !bytes.HasPrefix(rest, prefix) {
		t.Fatalf("full stat prefix = %q, want %q", rest[:min(len(rest), 12)], prefix)
	}
	rest = rest[len(prefix):]

	rules := map[string]string{}
	var order []string
	for {
		if rest[0] == 0 { // the empty key that ends the rule list
			rest = rest[1:]
			break
		}
		var key, value string
		key, rest = queryCString(t, rest)
		value, rest = queryCString(t, rest)
		rules[key] = value
		order = append(order, key)
	}
	wantOrder := []string{
		"hostname", "gametype", "game_id", "version", "plugins",
		"map", "numplayers", "maxplayers", "hostport", "hostip",
	}
	wantRules := map[string]string{
		"hostname":   cfg.MOTD,
		"gametype":   "SMP",
		"game_id":    "MINECRAFT",
		"version":    protocol.GameVersion,
		"plugins":    "",
		"map":        cfg.LevelName,
		"numplayers": "2",
		"maxplayers": strconv.Itoa(cfg.MaxPlayers),
		"hostport":   strconv.Itoa(cfg.ServerPort),
		"hostip":     cfg.ServerIP,
	}
	if !slices.Equal(order, wantOrder) {
		t.Errorf("full stat rules = %q, want %q", order, wantOrder)
	}
	if !slices.EqualFunc(order, wantOrder, func(a, b string) bool { return rules[a] == wantRules[b] }) {
		t.Errorf("full stat rule values = %q, want %q", rules, wantRules)
	}

	if rest[0] != 0x01 {
		t.Fatalf("player section intro = %#x, want 0x01", rest[0])
	}
	rest = rest[1:]
	tag, rest := queryCString(t, rest)
	if tag != "player_" {
		t.Fatalf("player section tag = %q, want \"player_\"", tag)
	}
	if rest[0] != 0 {
		t.Fatalf("player section padding = %#x, want a NUL", rest[0])
	}
	rest = rest[1:]

	var players []string
	for {
		var name string
		name, rest = queryCString(t, rest)
		if name == "" { // the empty name that ends the list
			break
		}
		players = append(players, name)
	}
	if !slices.Equal(players, []string{"First", "Second"}) {
		t.Errorf("full stat players = %q, want [First Second]", players)
	}
	if len(rest) != 0 {
		t.Errorf("full stat carried %q after the players", rest)
	}

	// The padding is a length, not content to check.
	if _, err := q.Write(append(statRequest(session, token, 0), 1, 2, 3, 4)); err != nil {
		t.Fatalf("send a full stat with other padding: %v", err)
	}
	reply = queryReply(t, q, 2*time.Second)
	if reply == nil {
		t.Fatal("the full stat with other padding got no answer")
	}
	if rest = expectTypeAndSession(t, reply, session); !bytes.HasPrefix(rest, prefix) {
		t.Error("a full stat's padding was read as more than a length")
	}
}

// TestQueryStatAnswersWithTheHandshakesSession checks the echo: each stat
// answer carries the session of the handshake that minted the token, not
// the one the stat itself was sent with.
func TestQueryStatAnswersWithTheHandshakesSession(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, queryConfig())
	q := dialQuery(t, startQueryConn(t, srv))

	handshake := [4]byte{0x0F, 0x12, 0x34, 0x56}
	token := queryHandshake(t, q, handshake)
	stat := [4]byte{8, 8, 7, 7}
	for _, padding := range []int{0, 4} {
		if _, err := q.Write(statRequest(stat, token, padding)); err != nil {
			t.Fatalf("send a stat with %d bytes of padding: %v", padding, err)
		}
		reply := queryReply(t, q, 2*time.Second)
		if reply == nil {
			t.Fatalf("the stat with %d bytes of padding got no answer", padding)
		}
		if !bytes.Equal(reply[1:5], handshake[:]) {
			t.Errorf("stat answer echoed % X, want the handshake's session % X", reply[1:5], handshake)
		}
		if bytes.Equal(reply[1:5], stat[:]) {
			t.Error("stat answer echoed the stat's own session")
		}
	}
}

// TestQueryNeedsTheChallengeToken checks the silence: a stat is answered
// only with the token the address's latest handshake minted, and a datagram
// that is not a handshake or a stat is not answered at all.
func TestQueryNeedsTheChallengeToken(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, queryConfig())
	q := dialQuery(t, startQueryConn(t, srv))
	session := [4]byte{1, 2, 3, 4}

	// No handshake happened yet, so no token has been minted to carry.
	if _, err := q.Write(statRequest(session, 1234, 0)); err != nil {
		t.Fatalf("send a stat without a handshake: %v", err)
	}
	if got := queryReply(t, q, 200*time.Millisecond); got != nil {
		t.Errorf("a stat without a handshake answered: %q", got)
	}

	// An unknown type is not answered.
	if _, err := q.Write(append([]byte{0xFE, 0xFD, 0x42}, session[:]...)); err != nil {
		t.Fatalf("send an unknown type: %v", err)
	}
	if got := queryReply(t, q, 200*time.Millisecond); got != nil {
		t.Errorf("an unknown type answered: %q", got)
	}

	token := queryHandshake(t, q, session)
	now := queryHandshake(t, q, session) // replaces the token with a fresh one

	// A datagram too short to carry a token is refused even though its
	// address has a live one.
	if _, err := q.Write(statRequest(session, now, 0)[:9]); err != nil {
		t.Fatalf("send a short stat: %v", err)
	}
	if got := queryReply(t, q, 200*time.Millisecond); got != nil {
		t.Errorf("a short stat answered: %q", got)
	}

	if _, err := q.Write(statRequest(session, token, 0)); err != nil {
		t.Fatalf("send a spent token: %v", err)
	}
	if got := queryReply(t, q, 200*time.Millisecond); got != nil {
		t.Errorf("a spent token answered: %q", got)
	}

	if _, err := q.Write(statRequest(session, now, 0)); err != nil {
		t.Fatalf("send the fresh token: %v", err)
	}
	if got := queryReply(t, q, 2*time.Second); got == nil {
		t.Error("the fresh token got no answer")
	}
}
