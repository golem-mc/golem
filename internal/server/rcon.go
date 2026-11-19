package server

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
)

// Source RCON packet types, as vanilla serves on rcon.port.
// vanilla: net.minecraft.server.rcon.thread.RconClient
const (
	rconTypeAuth       = 3 // SERVERDATA_AUTH
	rconTypeCommand    = 2 // SERVERDATA_EXECCOMMAND
	rconTypeOutput     = 0 // SERVERDATA_RESPONSE_VALUE
	rconTypeAuthResult = 2 // SERVERDATA_AUTH_RESPONSE; the id -1 means refused
)

// rconMinRequest is the smallest accepted frame: vanilla reads the 4-byte
// length and tail in one read of at least 10 bytes, so a length of 6 is its
// floor. vanilla: RconClient.run ("10 > read")
const rconMinRequest = 6

// rconMaxRequest is the largest accepted frame, vanilla's single request
// buffer size; bigger ones are dropped there and here.
// vanilla: PktUtils.MAX_PACKET_SIZE
const rconMaxRequest = 1460 - 4

// rconMaxOutput is the characters of output per response packet; the rest
// follow in more packets. vanilla: RconClient.sendCmdResponse
const rconMaxOutput = 4096

// ServeRcon serves one RCON connection until the peer hangs up: login, then
// one request at a time, each answered with RunRconLine's lines in packets
// of at most 4096 characters. vanilla: RconClient.run
func (s *Server) ServeRcon(conn net.Conn) {
	log := slog.With("remote", s.loggableAddr(conn.RemoteAddr()))
	defer conn.Close()
	in := bufio.NewReader(conn)

	// A failed reply write means the peer is gone; every branch ends the
	// connection alike.
	send := func(id, typ int32, payload string) bool {
		if err := writeRcon(conn, id, typ, payload); err != nil {
			log.Debug("rcon connection closed", "error", err)
			return false
		}
		return true
	}

	var authed bool
	for {
		req, ok := readRconRequest(in)
		if !ok {
			log.Debug("rcon connection closed")
			return
		}
		switch req.typ {
		case rconTypeAuth:
			// A failed login also revokes an authenticated connection. An empty
			// password never logs in, even if server.properties' is empty too.
			authed = req.payload != "" &&
				subtle.ConstantTimeCompare([]byte(req.payload), []byte(s.Config.RconPassword)) == 1
			id := int32(-1)
			if authed {
				id = req.id
			}
			if !send(id, rconTypeAuthResult, "") {
				return
			}
		case rconTypeCommand:
			// A command before login is answered as a failed login.
			if !authed {
				if !send(-1, rconTypeAuthResult, "") {
					return
				}
				continue
			}
			var out strings.Builder
			for _, text := range s.runPluginLine(s.rconSource(), req.payload, conn.RemoteAddr(), pluginruntime.RemoteServerCommand) {
				out.WriteString(text.Plain())
			}
			if err := writeRconOutput(conn, req.id, out.String()); err != nil {
				log.Debug("rcon connection closed", "error", err)
				return
			}
		default:
			// An unknown request type is answered, not dropped, with the type in hex as
			// Java prints ints.
			answer := "Unknown request " + strconv.FormatUint(uint64(uint32(req.typ)), 16)
			if !send(req.id, rconTypeOutput, answer) {
				return
			}
		}
	}
}

// rconRequest is one request off the wire.
type rconRequest struct {
	id      int32
	typ     int32
	payload string
}

// readRconRequest reads one whole request: a little-endian length, then
// that many bytes. A length outside the window (below vanilla's minimum
// read or above its buffer) returns ok=false and the caller hangs up. More
// lenient than vanilla's single read: two requests in one write, or one
// split across writes, are served here, where vanilla hangs up.
func readRconRequest(in *bufio.Reader) (rconRequest, bool) {
	var head [4]byte
	if _, err := io.ReadFull(in, head[:]); err != nil {
		return rconRequest{}, false
	}
	length := int32(binary.LittleEndian.Uint32(head[:]))
	if length < rconMinRequest || length > rconMaxRequest {
		return rconRequest{}, false
	}
	frame := make([]byte, length)
	if _, err := io.ReadFull(in, frame); err != nil {
		return rconRequest{}, false
	}
	req := rconRequest{
		id:  rconInt32(frame),
		typ: rconInt32(frame[4:]),
	}
	// The payload ends at its null terminator; a second closes the frame. A
	// frame too short for id and type has no payload.
	payload := frame[min(8, len(frame)):]
	if end := bytes.IndexByte(payload, 0); end >= 0 {
		payload = payload[:end]
	}
	req.payload = string(payload)
	return req, true
}

// rconInt32 reads a little-endian int32 from b's front, zero if fewer than
// four bytes remain, as vanilla's bounded reader answers a short tail.
// vanilla: PktUtils.intFromByteArray
func rconInt32(b []byte) int32 {
	if len(b) < 4 {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(b[:4]))
}

// writeRcon writes one packet: the tail's little-endian length, then id,
// type, payload and the two terminators.
func writeRcon(w io.Writer, id, typ int32, payload string) error {
	length := 8 + len(payload) + 2
	frame := make([]byte, 0, 4+length)
	var head [4]byte
	binary.LittleEndian.PutUint32(head[:], uint32(length))
	frame = append(frame, head[:]...)
	binary.LittleEndian.PutUint32(head[:], uint32(id))
	frame = append(frame, head[:]...)
	binary.LittleEndian.PutUint32(head[:], uint32(typ))
	frame = append(frame, head[:]...)
	frame = append(frame, payload...)
	frame = append(frame, 0, 0)
	_, err := w.Write(frame)
	return err
}

// writeRconOutput sends output as response packets of at most 4096
// characters, or one empty packet for no output.
// vanilla: RconClient.sendCmdResponse
func writeRconOutput(w io.Writer, id int32, out string) error {
	runes := []rune(out)
	for {
		n := min(len(runes), rconMaxOutput)
		if err := writeRcon(w, id, rconTypeOutput, string(runes[:n])); err != nil {
			return err
		}
		runes = runes[n:]
		if len(runes) == 0 {
			return nil
		}
	}
}

// rconListenAddr is cfg's RCON bind address, ok false when RCON is off:
// enable-rcon unset, empty password, or port outside 1-65535, as vanilla's
// RconThread.create declines. The host is server-ip, empty meaning all.
// vanilla: RconThread.create
func rconListenAddr(cfg *config.Config) (string, bool) {
	if !cfg.EnableRcon {
		return "", false
	}
	if cfg.RconPassword == "" {
		slog.Warn("rcon disabled: no password is set in server.properties")
		return "", false
	}
	if cfg.RconPort <= 0 || cfg.RconPort > 65535 {
		slog.Warn("rcon disabled: invalid port in server.properties", "port", cfg.RconPort)
		return "", false
	}
	withHost := *cfg
	if withHost.ServerIP == "" {
		withHost.ServerIP = "0.0.0.0"
	}
	return withHost.JoinAddr(cfg.RconPort), true
}

// listenRcon accepts RCON connections until the server stops. A refused
// configuration (see rconListenAddr) or failed bind is logged, not fatal.
// vanilla: RconThread.create, RconThread.run
func (s *Server) listenRcon() {
	addr, ok := rconListenAddr(s.Config)
	if !ok {
		return
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Warn("could not start rcon", "addr", addr, "error", err)
		return
	}
	slog.Info("rcon listening", "addr", addr)

	// clients holds open connections so a stop can close them, as
	// RconThread.stop stops each RconClient.
	var mu sync.Mutex
	clients := make(map[net.Conn]struct{})

	watching := make(chan struct{})
	defer close(watching)
	go func() {
		select {
		case <-watching:
		case <-s.stopReq:
			listener.Close()
			mu.Lock()
			for conn := range clients {
				conn.Close()
			}
			mu.Unlock()
		}
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				slog.Warn("rcon accept failed", "error", err)
			}
			return
		}
		// Registration and the stop check share the lock, so a connection accepted
		// as the stop fires is closed here or seen by the watcher, never missed.
		mu.Lock()
		select {
		case <-s.stopReq:
			mu.Unlock()
			conn.Close()
			return
		default:
		}
		clients[conn] = struct{}{}
		mu.Unlock()
		go func() {
			defer func() {
				mu.Lock()
				delete(clients, conn)
				mu.Unlock()
			}()
			s.ServeRcon(conn)
		}()
	}
}
