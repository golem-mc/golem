package chatsecurity

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func keyResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}
func serviceResponse(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/signed.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Service []byte }
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return `{"profilePropertyKeys":[{"publicKey":"not a key"}],"playerCertificateKeys":[{"publicKey":"` + base64.StdEncoding.EncodeToString(f.Service) + `"}]}`
}

func TestServiceKeysRefresh(t *testing.T) {
	body := serviceResponse(t)
	var calls atomic.Int32
	fail := false
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != serviceKeysURL || r.Method != http.MethodGet {
			t.Error("wrong trust endpoint")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Error("missing timeout")
		}
		if fail {
			return nil, errors.New("offline")
		}
		return keyResponse(body), nil
	})}
	cache := NewServiceKeys(t.Context(), client)
	now := time.Now()
	cache.now = func() time.Time { return now }
	keys, err := cache.Keys(t.Context())
	if err != nil || keys == nil || len(keys.keys) != 1 {
		t.Fatalf("fetch: %v", err)
	}
	if got, err := cache.Keys(t.Context()); got != keys || err != nil || calls.Load() != 1 {
		t.Fatalf("cache: %v", err)
	}
	now = now.Add(24 * time.Hour)
	fail = true
	for i := range 9 {
		got, err := cache.Keys(t.Context())
		if got != keys || err == nil {
			t.Fatalf("refresh discarded keys: %v", err)
		}
		want := 5 * time.Minute * time.Duration(1<<min(i, 6))
		if cache.next.Sub(now) != want {
			t.Fatalf("retry %d: %v", i, cache.next.Sub(now))
		}
		before := calls.Load()
		if _, err := cache.Keys(t.Context()); err == nil || calls.Load() != before {
			t.Fatal("backoff bypassed")
		}
		now = cache.next
	}
	fail = false
	renewed, err := cache.Keys(t.Context())
	if err != nil || renewed == keys || cache.failures != 0 || cache.next.Sub(now) != 24*time.Hour {
		t.Fatalf("recovery: %v", err)
	}
}

func TestServiceKeysRejectResponses(t *testing.T) {
	for name, body := range map[string]string{
		"property keys only": `{"profilePropertyKeys":[{"publicKey":"AA=="}]}`,
		"empty":              `{}`, "invalid JSON": `{`, "invalid base64": `{"playerCertificateKeys":[{"publicKey":"!"}]}`,
		"invalid DER": `{"playerCertificateKeys":[{"publicKey":"AA=="}]}`, "too big": strings.Repeat(" ", (1<<20)+1),
		"trailing JSON": `{} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			cache := NewServiceKeys(t.Context(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return keyResponse(body), nil })})
			if keys, err := cache.Keys(t.Context()); keys != nil || err == nil {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
	t.Run("redirect", func(t *testing.T) {
		var calls int
		cache := NewServiceKeys(t.Context(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			response := keyResponse("")
			response.StatusCode = http.StatusFound
			response.Header = http.Header{"Location": []string{"http://untrusted.invalid/"}}
			return response, nil
		})})
		if _, err := cache.Keys(t.Context()); err == nil || calls != 1 {
			t.Fatal("followed trust redirect")
		}
	})
}
