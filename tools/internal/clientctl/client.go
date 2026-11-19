// Package clientctl drives a Minecraft client running the clientctl agent.
//
// The agent (tools/clientctl/) answers on 127.0.0.1 with the client's state (screen,
// vitals, inventory, crosshair target, chat) and takes instructions (chat, held
// movement, container clicks, screenshots), so a test can drive the real client and
// read its state.
package clientctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client talks to one agent.
type Client struct {
	// Base is the agent's address, such as http://127.0.0.1:25700.
	Base string
	HTTP *http.Client
}

// FromEnv is the client for the agent CLIENTCTL_HOST and CLIENTCTL_PORT name
// (127.0.0.1 and 25700 by default).
func FromEnv() *Client {
	host, port := os.Getenv("CLIENTCTL_HOST"), os.Getenv("CLIENTCTL_PORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "25700"
	}
	return &Client{Base: "http://" + host + ":" + port, HTTP: &http.Client{Timeout: time.Minute}}
}

// Result is what the agent answers: JSON with ok, and the rest of the state or
// the action's outcome. A failure is {"ok": false, "error": "..."}.
type Result map[string]any

// OK reports whether the agent says the call worked.
func (r Result) OK() bool {
	ok, present := r["ok"].(bool)
	return !present || ok
}

// Call makes one HTTP call to the agent: a GET without a body, a POST with.
func (c *Client) Call(path string, body any) (Result, error) {
	return c.call(context.Background(), path, body)
}

func (c *Client) call(ctx context.Context, path string, body any) (Result, error) {
	method, reader := http.MethodGet, io.Reader(nil)
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		method, reader = http.MethodPost, bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+"/"+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out Result
	// The agent explains its own failures in JSON, with a 404 for an unknown
	// endpoint; anything else that is not success and not JSON is not the agent.
	if err := json.Unmarshal(data, &out); err != nil {
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("%s: HTTP %s: %.200s", path, resp.Status, bytes.TrimSpace(data))
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// State is the whole snapshot the agent answers GET /state with.
func (c *Client) State() (Result, error) { return c.Call("state", nil) }

// Chat says a chat line, or runs a command when the text begins with a slash.
func (c *Client) Chat(text string) (Result, error) {
	return c.Call("chat", map[string]any{"text": text})
}

// Keys is what Input holds down.
type Keys struct {
	Forward, Back, Left, Right, Jump, Sneak, Sprint bool
	Ticks                                           int
	// Yaw and Pitch turn the view when set.
	Yaw, Pitch *float64
}

// Input holds movement keys for a number of ticks, and may set yaw/pitch.
func (c *Client) Input(k Keys) (Result, error) {
	body := map[string]any{
		"forward": k.Forward, "back": k.Back, "left": k.Left, "right": k.Right,
		"jump": k.Jump, "sneak": k.Sneak, "sprint": k.Sprint, "ticks": k.Ticks,
	}
	if k.Yaw != nil {
		body["yaw"] = *k.Yaw
	}
	if k.Pitch != nil {
		body["pitch"] = *k.Pitch
	}
	return c.Call("input", body)
}

// Use right-clicks on what the crosshair points at, or uses the held item.
func (c *Client) Use() (Result, error) { return c.Call("use", map[string]any{}) }

// Attack attacks what the crosshair points at, or starts digging a block.
func (c *Client) Attack() (Result, error) { return c.Call("attack", map[string]any{}) }

// Slot selects a hotbar slot, 0 to 8.
func (c *Client) Slot(n int) (Result, error) { return c.Call("slot", map[string]any{"slot": n}) }

// Drop throws the held stack, or one item of it.
func (c *Client) Drop(all bool) (Result, error) { return c.Call("drop", map[string]any{"all": all}) }

// Click clicks a slot of the open container: pickup, quick_move, swap or throw.
func (c *Client) Click(slot, button int, mode string) (Result, error) {
	return c.Call("click", map[string]any{"slot": slot, "button": button, "mode": mode})
}

// Respawn asks the server for a respawn.
func (c *Client) Respawn() (Result, error) { return c.Call("respawn", map[string]any{}) }

// Disconnect leaves the server.
func (c *Client) Disconnect() (Result, error) { return c.Call("disconnect", map[string]any{}) }

// Connect starts a connection to address.
func (c *Client) Connect(address string) (Result, error) {
	return c.Call("connect", map[string]any{"address": address})
}

// Screenshot takes a screenshot through the client's own grab and writes it to
// path on the machine running the client.
func (c *Client) Screenshot(path string) (Result, error) {
	return c.Call("screenshot", map[string]any{"path": path})
}

// Wait polls /state until pred holds, or the timeout runs out.
func (c *Client) Wait(pred func(Result) bool, timeout, interval time.Duration) (Result, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := c.State()
		if err != nil {
			return nil, err
		}
		if pred(state) {
			return state, nil
		}
		time.Sleep(interval)
	}
	return nil, fmt.Errorf("condition not met within %s", timeout)
}

// WaitTicks polls until pred holds within ticks local player ticks of the first
// snapshot. A disconnect or counter reset ends the wait; watchdog bounds even a
// stalled state request independently of the gameplay budget.
func (c *Client) WaitTicks(pred func(Result) bool, ticks int, watchdog time.Duration) (Result, error) {
	if ticks < 0 {
		return nil, fmt.Errorf("tick budget must be nonnegative")
	}
	ctx, cancel := context.WithTimeout(context.Background(), watchdog)
	defer cancel()
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	start, previous := -1.0, -1.0
	for {
		state, err := c.call(ctx, "state", nil)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("client tick wait watchdog expired after %s: %w", watchdog, ctx.Err())
		}
		if err != nil {
			return nil, err
		}
		if !state.OK() {
			return nil, fmt.Errorf("client state failed: %v", state["error"])
		}
		if connected, _ := state["connected"].(bool); !connected {
			return nil, fmt.Errorf("client disconnected during tick wait")
		}
		tick, ok := state["ticks"].(float64)
		if !ok || tick < 0 {
			return nil, fmt.Errorf("client state has no valid tick counter")
		}
		if tick < previous {
			return nil, fmt.Errorf("client tick counter reset during wait")
		}
		if start < 0 {
			start = tick
		}
		elapsed := tick - start
		if elapsed <= float64(ticks) && pred(state) {
			return state, nil
		}
		if elapsed >= float64(ticks) {
			return nil, fmt.Errorf("condition not met within %d client ticks", ticks)
		}
		previous = tick
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("client tick wait watchdog expired after %s: %w", watchdog, ctx.Err())
		case <-poll.C:
		}
	}
}

// WaitScreen waits for the screen's kind to become the given one.
func (c *Client) WaitScreen(kind string, timeout time.Duration) (Result, error) {
	return c.Wait(func(s Result) bool {
		screen, _ := s["screen"].(map[string]any)
		return screen["kind"] == kind
	}, timeout, 250*time.Millisecond)
}

// WaitWorld waits until the client is in a world (joined and loading done).
func (c *Client) WaitWorld(timeout time.Duration) (Result, error) {
	return c.Wait(func(s Result) bool { connected, _ := s["connected"].(bool); return connected }, timeout, 250*time.Millisecond)
}
