package server

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// queryChallengeLife is how long a handshake token is valid: the 30-second
// prune vanilla intends. Vanilla's never fires (it compares an epoch date
// to a monotonic clock), so its tokens live forever; this is a deliberate
// difference. vanilla: QueryThreadGs4.pruneChallenges
const queryChallengeLife = 30 * time.Second

// maxQueryChallenges bounds live tokens, so spoofed handshake floods within
// one token life cannot grow the book unboundedly. Past it the oldest goes;
// a real client's stat follows its handshake at once, so its token is
// among the newest.
const maxQueryChallenges = 1 << 16

// queryChallenge is one remote address's token, with the handshake's
// session ID the answers echo (not the stat's), its mint time, and seq, its
// mint order in the book.
type queryChallenge struct {
	ident  [4]byte
	token  int32
	minted time.Time
	seq    uint64
}

// queryMint is one handshake in the order the book took them.
type queryMint struct {
	addr   netip.AddrPort
	minted time.Time
	seq    uint64
}

// queryChallengeBook is the live tokens, one per address, and their mint
// order, oldest first. A token replaced by the address's next handshake
// leaves a stale entry, skipped when reached and compacted once the order
// is twice the bound. The caller serialises all calls.
type queryChallengeBook struct {
	byAddr map[netip.AddrPort]queryChallenge
	order  []queryMint
	mints  uint64
	// now is the clock tokens age by, time.Now when nil.
	now func() time.Time
}

func newQueryChallengeBook() *queryChallengeBook {
	return &queryChallengeBook{byAddr: make(map[netip.AddrPort]queryChallenge)}
}

func (b *queryChallengeBook) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// mint files a token for addr, replacing the one it had.
func (b *queryChallengeBook) mint(addr netip.AddrPort, ident [4]byte, token int32) {
	now := b.clock()
	b.expire(now)
	if _, ok := b.byAddr[addr]; !ok && len(b.byAddr) >= maxQueryChallenges {
		b.dropOldest()
	}
	b.mints++
	b.byAddr[addr] = queryChallenge{ident: ident, token: token, minted: now, seq: b.mints}
	b.order = append(b.order, queryMint{addr: addr, minted: now, seq: b.mints})
	if len(b.order) >= 2*maxQueryChallenges {
		live := make([]queryMint, 0, len(b.byAddr))
		for _, m := range b.order {
			if b.current(m) {
				live = append(live, m)
			}
		}
		b.order = live
	}
}

// lookup is addr's live token, if it has one.
func (b *queryChallengeBook) lookup(addr netip.AddrPort) (queryChallenge, bool) {
	challenge, ok := b.byAddr[addr]
	if !ok || b.clock().Sub(challenge.minted) >= queryChallengeLife {
		return queryChallenge{}, false
	}
	return challenge, true
}

// current reports whether m is the mint its address's token still is.
func (b *queryChallengeBook) current(m queryMint) bool {
	challenge, ok := b.byAddr[m.addr]
	return ok && challenge.seq == m.seq
}

// expire drops the tokens a life old at now, from the front of the order.
func (b *queryChallengeBook) expire(now time.Time) {
	for len(b.order) > 0 && now.Sub(b.order[0].minted) >= queryChallengeLife {
		if m := b.order[0]; b.current(m) {
			delete(b.byAddr, m.addr)
		}
		b.order = b.order[1:]
	}
}

// dropOldest drops the oldest live token.
func (b *queryChallengeBook) dropOldest() {
	for len(b.order) > 0 {
		m := b.order[0]
		b.order = b.order[1:]
		if b.current(m) {
			delete(b.byAddr, m.addr)
			return
		}
	}
}

// startQuery opens the query listener on query.port and serves it on its
// own goroutine. A port outside 1-65535, an unresolvable address or a taken
// one disables it with a warning (vanilla: QueryThreadGs4.initSocket)
// rather than failing the server.
func (s *Server) startQuery() {
	if !s.Config.EnableQuery {
		return
	}
	port := s.Config.QueryPort
	if port < 1 || port > 65535 { // vanilla: QueryThreadGs4.create
		slog.Warn("invalid query port, queries disabled", "port", port)
		return
	}
	// Bind like the game listener, so server-ip applies to query too.
	addr, err := net.ResolveUDPAddr("udp", s.Config.JoinAddr(port))
	if err != nil {
		slog.Warn("could not start the query listener", "error", err)
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		slog.Warn("could not start the query listener", "error", err)
		return
	}
	s.queryConn = conn
	slog.Info("query running", "addr", conn.LocalAddr())
	go s.ServeQuery(conn)
}

// stopQuery closes the query socket, whose closed read ends ServeQuery.
func (s *Server) stopQuery() {
	if s.queryConn != nil {
		_ = s.queryConn.Close()
	}
}

// ServeQuery answers the GS4 query protocol on conn until it closes. Listen
// opens it with enable-query on; a test can pass its own socket.
func (s *Server) ServeQuery(conn *net.UDPConn) {
	hostIP := s.queryHostIP()
	// vanilla reads into one 1460-byte buffer: PktUtils.MAX_PACKET_SIZE.
	buf := make([]byte, 1460)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			// stopQuery's close ends the loop; any other error is logged.
			if !errors.Is(err, net.ErrClosed) {
				slog.Warn("query listener read failed", "error", err)
			}
			return
		}
		s.answerQuery(conn, addr, buf[:n], hostIP)
	}
}

// queryHostIP is the host address stat answers report: server-ip as
// configured, else the machine's address (vanilla:
// InetAddress.getLocalHost), else 0.0.0.0. A server-ip of 0.0.0.0 binds all
// and is no host (vanilla's getLocalIp answers null), so it falls through.
func (s *Server) queryHostIP() string {
	if ip := s.Config.ServerIP; ip != "" && ip != "0.0.0.0" {
		return ip
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "0.0.0.0"
	}
	addrs, err := net.LookupHost(hostname)
	if err != nil || len(addrs) == 0 {
		return "0.0.0.0"
	}
	return addrs[0]
}

// answerQuery answers one query datagram, if it asks for anything.
func (s *Server) answerQuery(conn *net.UDPConn, addr *net.UDPAddr, data []byte, hostIP string) {
	req, ok := protocol.ParseQueryRequest(data)
	if !ok {
		return
	}
	switch req.Type {
	case protocol.QueryHandshake:
		token := queryToken()
		s.queryMu.Lock()
		s.queryChallenges.mint(addr.AddrPort(), req.Session, token)
		s.queryMu.Unlock()
		_, _ = conn.WriteToUDP(protocol.QueryHandshakeReply(req.Session, token), addr)
	case protocol.QueryStat:
		s.queryMu.Lock()
		challenge, ok := s.queryChallenges.lookup(addr.AddrPort())
		s.queryMu.Unlock()
		if !ok || challenge.token != req.Challenge {
			return
		}
		// One snapshot gives both the count and names, so they agree.
		profiles := s.onlineProfiles()
		names := make([]string, len(profiles))
		for i, profile := range profiles {
			names[i] = profile.Name
		}
		status := protocol.QueryStatus{
			MOTD:    s.motd.Load().Plain(),
			Map:     s.Config.LevelName,
			Online:  len(profiles),
			Max:     int(s.maxPlayers.Load()),
			Port:    s.Config.ServerPort,
			HostIP:  hostIP,
			Players: names,
		}
		var reply []byte
		if req.Full {
			reply = protocol.QueryFullStatReply(challenge.ident, status)
		} else {
			reply = protocol.QueryBasicStatReply(challenge.ident, status)
		}
		_, _ = conn.WriteToUDP(reply, addr)
	}
}

// queryToken mints a token in vanilla's 24-bit range (vanilla:
// QueryThreadGs4's RandomSource.nextInt(0x1000000)); crypto/rand keeps it
// unguessable, unlike the level's shared rng.
func queryToken() int32 {
	var b [4]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error
	return int32(binary.BigEndian.Uint32(b[:]) & 0xFFFFFF)
}
