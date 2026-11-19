package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// rcon is a remote-console client: both servers run its commands as the
// server console.
type rcon struct {
	mu   sync.Mutex
	conn net.Conn
	r    *bufio.Reader
	id   int32
}

const (
	rconResponse = 0
	rconCommand  = 2
	rconLogin    = 3
)

func dialRCON(addr, password string) (*rcon, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c := &rcon{conn: conn, r: bufio.NewReader(conn)}
	if err := c.write(rconLogin, password); err != nil {
		conn.Close()
		return nil, err
	}
	id, _, err := c.read()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if id == -1 {
		conn.Close()
		return nil, errors.New("rcon: wrong password")
	}
	return c, nil
}

func (c *rcon) write(kind int32, body string) error {
	c.id++
	buf := make([]byte, 12, 14+len(body))
	binary.LittleEndian.PutUint32(buf[0:], uint32(10+len(body)))
	binary.LittleEndian.PutUint32(buf[4:], uint32(c.id))
	binary.LittleEndian.PutUint32(buf[8:], uint32(kind))
	buf = append(buf, body...)
	buf = append(buf, 0, 0)
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Minute))
	_, err := c.conn.Write(buf)
	return err
}

func (c *rcon) read() (int32, string, error) {
	var head [12]byte
	if _, err := io.ReadFull(c.r, head[:]); err != nil {
		return 0, "", err
	}
	n := int32(binary.LittleEndian.Uint32(head[0:]))
	if n < 10 || n > 1<<20 {
		return 0, "", fmt.Errorf("rcon: bad length %d", n)
	}
	body := make([]byte, n-8)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return 0, "", err
	}
	return int32(binary.LittleEndian.Uint32(head[4:])), string(body[:len(body)-2]), nil
}

// Run runs one command and returns the console's reply.
func (c *rcon) Run(command string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.write(rconCommand, command); err != nil {
		return "", err
	}
	_, body, err := c.read()
	return body, err
}

// MustRun runs commands and panics on a broken connection.
func (c *rcon) MustRun(commands ...string) {
	for _, cmd := range commands {
		if _, err := c.Run(cmd); err != nil {
			panic(fmt.Sprintf("rcon %q: %v", cmd, err))
		}
	}
}

func (c *rcon) Close() { c.conn.Close() }
