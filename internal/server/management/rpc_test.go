package management

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// TestEnvelopeParamsAndHandlerErrors runs requests against a method table of
// the test's own, since the server's is empty.
// vanilla: IncomingRpcMethod.Method.apply, ParameterlessMethod.apply,
// Connection.handleIncomingRequest
func TestEnvelopeParamsAndHandlerErrors(t *testing.T) {
	t.Parallel()
	echo := func(_ *conn, p json.RawMessage) (any, error) { return p, nil }
	s := &Server{methods: map[string]method{
		"minecraft:one":   {param: "value", run: echo},
		"minecraft:none":  {run: func(*conn, json.RawMessage) (any, error) { return "ok", nil }},
		"minecraft:bad":   {run: func(*conn, json.RawMessage) (any, error) { return nil, errInvalidParams("nope") }},
		"minecraft:wrong": {run: func(*conn, json.RawMessage) (any, error) { return nil, errInvalidRequest("no") }},
		"minecraft:boom":  {run: func(*conn, json.RawMessage) (any, error) { panic("boom") }},
		"minecraft:fail":  {run: func(*conn, json.RawMessage) (any, error) { return nil, errors.New("x") }},
		"minecraft:void":  {run: func(*conn, json.RawMessage) (any, error) { return nil, nil }},
	}}
	for _, tc := range []struct{ send, want string }{
		{`{"id":1,"method":"one","params":{"value":5}}`, `{"jsonrpc":"2.0","id":1,"result":5}`},
		{`{"id":1,"method":"minecraft:one","params":[{"a":1}]}`, `{"jsonrpc":"2.0","id":1,"result":{"a":1}}`},
		{`{"id":1,"method":"one","params":{"other":5}}`,
			`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params","data":"Params passed by-name, but expected param [value] does not exist"}}`},
		{`{"id":1,"method":"one","params":[1,2]}`,
			`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params","data":"Expected exactly one element in the params array"}}`},
		{`{"id":1,"method":"one"}`,
			`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params","data":"Expected params as array or named"}}`},
		{`{"id":1,"method":"one","params":7}`,
			`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params","data":"Expected params as array or named"}}`},
		{`{"id":1,"method":"none"}`, `{"jsonrpc":"2.0","id":1,"result":"ok"}`},
		{`{"id":1,"method":"none","params":[ ]}`, `{"jsonrpc":"2.0","id":1,"result":"ok"}`},
		{`{"id":1,"method":"none","params":{}}`,
			`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params","data":"Expected no params, or an empty array"}}`},
		{`{"id":1,"method":"bad"}`, `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params","data":"nope"}}`},
		{`{"id":1,"method":"wrong"}`, `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request","data":"no"}}`},
		{`{"id":1,"method":"boom"}`, `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"Internal error"}}`},
		{`{"id":1,"method":"fail"}`, `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"Internal error"}}`},
		// A number method is its text, as GsonHelper.getAsString reads it.
		{`{"id":1,"method":5}`,
			`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found","data":"Method not found: 5"}}`},
		{`{"id":1,"method":null}`,
			`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"Internal error","data":"Unknown error handling request - check server logs for stack trace"}}`},
		{`{"id":1,"error":5}`,
			`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"Internal error","data":"Unknown error handling request - check server logs for stack trace"}}`},
		// A response without an id is no response.
		{`{"result":1}`, `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"Invalid Request"}}`},
		{`[5]`, `[{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"Invalid Request"}}]`},
	} {
		got := s.handleMessage(nil, []byte(tc.send))
		var g, w any
		if err := json.Unmarshal(got, &g); err != nil {
			t.Errorf("%s: answer %s: %v", tc.send, got, err)
			continue
		}
		_ = json.Unmarshal([]byte(tc.want), &w)
		if !reflect.DeepEqual(g, w) {
			t.Errorf("%s: got %s, want %s", tc.send, got, tc.want)
		}
	}

	// No answer to a notification, a method without a result, an error
	// object or an unasked response.
	for _, send := range []string{
		`{"method":"none"}`, `{"method":"boom"}`, `{"id":1,"method":"void"}`,
		`{"id":1,"error":{"code":1}}`, `{"id":1,"result":1}`,
	} {
		if got := s.handleMessage(nil, []byte(send)); got != nil {
			t.Errorf("%s: answered %s, want nothing", send, got)
		}
	}
}
