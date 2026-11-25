package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/tools/internal/clientctl"
)

// agent stands in for the clientctl agent: it records each call and answers
// from a script.
type agent struct {
	mu    sync.Mutex
	calls []string
	state []string // JSON answers to GET /state, in turn
}

func (a *agent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, r.Method+" "+r.URL.Path+" "+string(body))
	switch {
	case r.URL.Path == "/state" && len(a.state) > 0:
		_, _ = io.WriteString(w, a.state[0])
		if len(a.state) > 1 {
			a.state = a.state[1:]
		}
	case r.URL.Path == "/fail":
		_, _ = io.WriteString(w, `{"ok": false, "error": "no"}`)
	default:
		_, _ = io.WriteString(w, `{"ok": true}`)
	}
}

func drive(t *testing.T, a *agent, args ...string) (int, string, string) {
	t.Helper()
	srv := httptest.NewServer(a)
	defer srv.Close()
	var out, errb bytes.Buffer
	code := run(args, &clientctl.Client{Base: srv.URL, HTTP: srv.Client()}, &out, &errb)
	return code, out.String(), errb.String()
}

func TestCommandsBecomeTheAgentsCalls(t *testing.T) {
	a := &agent{}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"state"}, "GET /state "},
		{[]string{"chat", "/time set noon"}, `POST /chat {"text":"/time set noon"}`},
		{[]string{"slot", "3"}, `POST /slot {"slot":3}`},
		{[]string{"click", "5", "-button", "1", "-mode", "quick_move"}, `POST /click {"button":1,"mode":"quick_move","slot":5}`},
		{[]string{"click", "-button", "1", "5"}, `POST /click {"button":1,"mode":"pickup","slot":5}`},
		{[]string{"click", "-999"}, `POST /click {"button":0,"mode":"pickup","slot":-999}`},
		{[]string{"click", "-button", "1", "-999"}, `POST /click {"button":1,"mode":"pickup","slot":-999}`},
		{[]string{"click", "-button=1", "--", "-999"}, `POST /click {"button":1,"mode":"pickup","slot":-999}`},
		{[]string{"chat", "--", "-hello"}, `POST /chat {"text":"-hello"}`},
		{[]string{"drop", "-all"}, `POST /drop {"all":true}`},
		{[]string{"connect", "127.0.0.1:25565"}, `POST /connect {"address":"127.0.0.1:25565"}`},
		{[]string{"screenshot", "/tmp/s.png"}, `POST /screenshot {"path":"/tmp/s.png"}`},
		{[]string{"use"}, `POST /use {}`},
		{[]string{"use", "-ticks", "30"}, `POST /use {"ticks":30}`},
		{[]string{"use", "-ticks", "-1"}, `POST /use {"ticks":-1}`},
		{[]string{"use", "-ticks", "0"}, `POST /use {"ticks":0}`},
		{[]string{"use", "-release"}, `POST /use {"ticks":0}`},
		{[]string{"attack", "-ticks", "40"}, `POST /attack {"ticks":40}`},
		{[]string{"input", "-forward", "-ticks", "30", "-pitch", "70"}, `POST /input {"back":false,"forward":true,"jump":false,"left":false,"pitch":70,"right":false,"sneak":false,"sprint":false,"ticks":30}`},
	} {
		a.calls = nil
		if code, _, stderr := drive(t, a, c.args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", c.args, code, stderr)
		}
		if len(a.calls) != 1 || a.calls[0] != c.want {
			t.Errorf("%v made %q, want %q", c.args, a.calls, c.want)
		}
	}
}

func TestWrongArgumentsAreRefusedBeforeAnyCall(t *testing.T) {
	a := &agent{}
	for _, args := range [][]string{
		{"chat", "hello", "world"}, {"chat"}, {"slot"}, {"slot", "1", "2"},
		{"state", "extra"}, {"drop", "now"}, {"click", "-button", "1"}, {"click", "x"},
		{"use", "-ticks", "x"}, {"use", "-ticks", "5", "-release"}, {"use", "now"}, {"click", "-nope", "5"}, {"click", "5", "-button"}, {"click", "--", "-x", "-y"},
	} {
		if code, _, _ := drive(t, a, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if len(a.calls) != 0 {
		t.Errorf("calls were made: %q", a.calls)
	}
}

func TestAFailureAnswerExitsOne(t *testing.T) {
	srv := httptest.NewServer(&agent{})
	defer srv.Close()
	c := &clientctl.Client{Base: srv.URL, HTTP: srv.Client()}
	res, err := c.Call("fail", map[string]any{})
	if err != nil || res.OK() {
		t.Fatalf("res = %v, err = %v", res, err)
	}
	if code, _, _ := drive(t, &agent{}, "chat"); code != 2 {
		t.Errorf("chat without text: exit %d", code)
	}
}

func TestWaitPollsUntilTheStateHolds(t *testing.T) {
	a := &agent{state: []string{`{"screen": {"kind": "none"}}`, `{"screen": {"kind": "death"}, "connected": true}`}}
	code, out, stderr := drive(t, a, "wait", "-kind", "death", "-timeout", "5")
	if code != 0 || !strings.Contains(out, `"death"`) {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Errorf("the output is not JSON: %v", err)
	}
	if code, _, stderr := drive(t, &agent{state: []string{`{"connected": false}`}}, "wait", "-world", "-timeout", "0.3"); code != 2 || !strings.Contains(stderr, "not met within") {
		t.Errorf("a wait that times out: exit %d, %q", code, stderr)
	}
}
