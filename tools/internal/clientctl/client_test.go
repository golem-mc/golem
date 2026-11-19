package clientctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCallReportsWhatIsNotTheAgent(t *testing.T) {
	for _, c := range []struct {
		name    string
		status  int
		body    string
		wantErr string // empty: a Result comes back
	}{
		{"an agent failure", 404, `{"ok": false, "error": "no such endpoint: /x"}`, ""},
		{"an empty error page", 502, ``, "HTTP 502"},
		{"an HTML error page", 500, `<html>oops</html>`, "HTTP 500"},
		{"a success that is not JSON", 200, `<html>`, "invalid character"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, c.body)
		}))
		res, err := (&Client{Base: srv.URL, HTTP: srv.Client()}).Call("x", nil)
		srv.Close()
		switch {
		case c.wantErr == "" && (err != nil || res.OK()):
			t.Errorf("%s: %v, %v; want the agent's failure", c.name, res, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: err = %v, want it to mention %q", c.name, err, c.wantErr)
		}
	}
}

func TestWaitTicksUsesClientTicks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ticks   []int
		ready   int // snapshot index, or -1 if the predicate never holds
		budget  int
		wantErr string
	}{
		{"same tick polls and skipped ticks", []int{80, 80, 80, 82, 85}, 4, 5, ""},
		{"budget exhausted", []int{80, 82, 85}, -1, 5, "within 5 client ticks"},
		{"predicate after budget", []int{80, 86}, 1, 5, "within 5 client ticks"},
		{"immediate success", []int{80}, 0, 0, ""},
		{"zero budget", []int{80}, -1, 0, "within 0 client ticks"},
		{"reset", []int{80, 81, 0}, 2, 5, "counter reset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/state" {
					t.Errorf("path = %q, want /state", r.URL.Path)
				}
				_, _ = fmt.Fprintf(w, `{"connected":true,"ticks":%d,"ready":%t}`, tc.ticks[n], n == tc.ready)
				if n < len(tc.ticks)-1 {
					n++
				}
			}))
			defer srv.Close()
			c := &Client{Base: srv.URL, HTTP: srv.Client()}
			res, err := c.WaitTicks(func(s Result) bool { return s["ready"] == true }, tc.budget, 5*time.Second)
			if tc.wantErr == "" {
				if err != nil || res["ready"] != true {
					t.Fatalf("wait = %v, %v; want ready state", res, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("wait error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestWaitTicksRejectsInvalidState(t *testing.T) {
	for _, tc := range []struct {
		body    string
		wantErr string
	}{
		{`{"connected":false,"ticks":null}`, "disconnected"},
		{`{"connected":true}`, "no valid tick counter"},
		{`{"ok":false,"error":"client not started yet"}`, "client not started yet"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, tc.body)
		}))
		c := &Client{Base: srv.URL, HTTP: srv.Client()}
		_, err := c.WaitTicks(func(Result) bool {
			t.Error("predicate called with invalid state")
			return true
		}, 5, time.Second)
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("state %s: error = %v, want %q", tc.body, err, tc.wantErr)
		}
	}
}

func TestWaitTicksWatchdog(t *testing.T) {
	for _, stalledRequest := range []bool{false, true} {
		t.Run(fmt.Sprintf("stalled-request-%t", stalledRequest), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stalledRequest {
					<-r.Context().Done()
					return
				}
				_, _ = io.WriteString(w, `{"connected":true,"ticks":80}`)
			}))
			defer srv.Close()
			c := &Client{Base: srv.URL, HTTP: srv.Client()}
			_, err := c.WaitTicks(func(Result) bool { return false }, 5, 100*time.Millisecond)
			if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "watchdog") {
				t.Fatalf("wait error = %v, want watchdog deadline", err)
			}
		})
	}
}
