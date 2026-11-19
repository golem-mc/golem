package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// rcon is a minimal Source RCON client: little-endian length, request ID and
// type, then a NUL-terminated body and one more NUL.
type rcon struct {
	conn net.Conn
	id   int32
}

const (
	rconCommand = 2
	rconLogin   = 3
)

func dialRcon(addr, password string) (*rcon, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	r := &rcon{conn: conn}
	id, _, err := r.roundTrip(rconLogin, password)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if id == -1 {
		conn.Close()
		return nil, fmt.Errorf("rcon login refused")
	}
	return r, nil
}

// Command runs one command and returns its output.
func (r *rcon) Command(cmd string) (string, error) {
	_, body, err := r.roundTrip(rconCommand, cmd)
	return body, err
}

func (r *rcon) Close() error { return r.conn.Close() }

func (r *rcon) roundTrip(kind int32, body string) (int32, string, error) {
	r.id++
	pkt := make([]byte, 0, 14+len(body))
	pkt = binary.LittleEndian.AppendUint32(pkt, uint32(10+len(body)))
	pkt = binary.LittleEndian.AppendUint32(pkt, uint32(r.id))
	pkt = binary.LittleEndian.AppendUint32(pkt, uint32(kind))
	pkt = append(pkt, body...)
	pkt = append(pkt, 0, 0)
	// Generating a chunk on a loaded machine can hold a command for a while.
	if err := r.conn.SetDeadline(time.Now().Add(5 * time.Minute)); err != nil {
		return 0, "", err
	}
	if _, err := r.conn.Write(pkt); err != nil {
		return 0, "", err
	}
	var head [12]byte
	if _, err := io.ReadFull(r.conn, head[:]); err != nil {
		return 0, "", err
	}
	length := int(binary.LittleEndian.Uint32(head[:4]))
	if length < 10 || length > 1<<20 {
		return 0, "", fmt.Errorf("rcon reply of %d bytes", length)
	}
	rest := make([]byte, length-8)
	if _, err := io.ReadFull(r.conn, rest); err != nil {
		return 0, "", err
	}
	id := int32(binary.LittleEndian.Uint32(head[4:8]))
	return id, string(rest[:len(rest)-2]), nil
}
