package management

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

// rpcCode is a JSON-RPC 2.0 error code with its message.
// vanilla: JsonRPCErrors
type rpcCode struct {
	code    int
	message string
}

var (
	parseError     = rpcCode{-32700, "Parse error"}
	invalidRequest = rpcCode{-32600, "Invalid Request"}
	methodNotFound = rpcCode{-32601, "Method not found"}
	invalidParams  = rpcCode{-32602, "Invalid params"}
	internalError  = rpcCode{-32603, "Internal error"}
)

// rpcError is a handler's or the dispatch's failure answered with code and
// data. vanilla: InvalidParameterJsonRpcException, InvalidRequestJsonRpcException,
// MethodNotFoundJsonRpcException
type rpcError struct {
	code rpcCode
	data string
}

func (e *rpcError) Error() string { return e.data }

// errInvalidParams and errInvalidRequest are the failures a method returns.
func errInvalidParams(data string) error  { return &rpcError{invalidParams, data} }
func errInvalidRequest(data string) error { return &rpcError{invalidRequest, data} }

// nullID is the id an answer carries when the request's is unknown.
var nullID = json.RawMessage("null")

type errorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *errorObject    `json:"error,omitempty"`
}

// errorResponse encodes an error answer; blank data is left out.
// vanilla: JsonRPCUtils.createError
func errorResponse(id json.RawMessage, c rpcCode, data string) json.RawMessage {
	if strings.TrimSpace(data) == "" {
		data = ""
	}
	return encode(response{JSONRPC: "2.0", ID: id, Error: &errorObject{c.code, c.message, data}})
}

// encode marshals what this file builds, which always encodes.
func encode(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// method is an entry in the method table: param is the name of its one
// parameter, empty for none.
type method struct {
	param string
	run   func(c *conn, p json.RawMessage) (any, error)
}

// handleMessage answers one text message, or returns nil for none.
// vanilla: Connection.channelRead0, handleBatchRequest
func (s *Server) handleMessage(c *conn, msg []byte) json.RawMessage {
	var v json.RawMessage
	// The lenient reader also reports Gson's nesting limit, which
	// encoding/json does not have.
	if json.Unmarshal(msg, &v) != nil || nestedDeeper(v, lenientNesting) {
		var err error
		if v, err = parseLenient(msg); err != nil {
			return errorResponse(nullID, parseError, err.Error())
		}
	}
	switch v[0] {
	case '{':
		return s.handleObject(c, v)
	case '[':
		var entries []json.RawMessage
		_ = json.Unmarshal(v, &entries) // valid JSON, so it decodes
		answers := make([]json.RawMessage, 0, len(entries))
		for _, e := range entries {
			// Vanilla drops the connection on an entry that is not an object.
			var a json.RawMessage
			if e[0] == '{' {
				a = s.handleObject(c, e)
			} else {
				a = errorResponse(nullID, invalidRequest, "")
			}
			if a != nil {
				answers = append(answers, a)
			}
		}
		return encode(answers)
	}
	return errorResponse(nullID, invalidRequest, "")
}

// handleObject answers one request object, or returns nil for a notification
// or a response.
// vanilla: Connection.handleJsonObject
func (s *Server) handleObject(c *conn, raw json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(raw, &obj) // a valid JSON object, so it decodes
	id, hasID := obj["id"]
	methodRaw, hasMethod := obj["method"]
	_, hasResult := obj["result"]
	errRaw, hasError := obj["error"]
	params, hasParams := obj["params"]

	name, ok := methodName(methodRaw)
	// A method that is not a primitive, or an error that is not an object,
	// throws in GsonHelper and is answered as vanilla's catch-all.
	if hasMethod && !ok || hasError && errRaw[0] != '{' {
		slog.Error("Error while handling rpc request", "request", string(raw))
		return errorResponse(nullID, internalError, "Unknown error handling request - check server logs for stack trace")
	}
	switch {
	case hasMethod && !hasResult && !hasError:
		if hasID && !validRequestID(id) {
			return errorResponse(nullID, invalidRequest, "Invalid request id - only String, Number and NULL supported")
		}
		if !hasParams {
			params = nil
		}
		return s.handleRequest(c, id, name, params)
	case !hasMethod && hasResult && !hasError && hasID:
		// This server sends no requests, so no response is awaited.
		slog.Warn(fmt.Sprintf("Received unknown response (id: %s): %s", id, obj["result"]))
		return nil
	case !hasMethod && !hasResult && hasError:
		slog.Error(fmt.Sprintf("Received error (id: %s): %s", id, errRaw))
		return nil
	}
	if !hasID {
		id = nullID
	}
	return errorResponse(id, invalidRequest, "")
}

// methodName reads a method the way GsonHelper.getAsString does: a string as
// is, a number or boolean as its text; anything else fails.
func methodName(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", true
	}
	switch raw[0] {
	case '"':
		var s string
		_ = json.Unmarshal(raw, &s)
		return s, true
	case '{', '[', 'n':
		return "", false
	}
	return string(raw), true
}

// validRequestID reports whether id is null, a number or a string.
// vanilla: Connection.isValidRequestId
func validRequestID(id json.RawMessage) bool {
	switch id[0] {
	case '{', '[', 't', 'f':
		return false
	}
	return true
}

// handleRequest runs a request and answers it, or returns nil for a
// notification (id nil) or a method with no result. params is nil when the
// request has none.
// vanilla: Connection.handleIncomingRequest
func (s *Server) handleRequest(c *conn, id json.RawMessage, name string, params json.RawMessage) json.RawMessage {
	result, err := s.dispatch(c, name, params)
	rerr := (*rpcError)(nil)
	if err != nil && !errors.As(err, &rerr) {
		slog.Error("Error while dispatching rpc method", "method", name, "error", err)
	}
	switch {
	case id == nil:
		return nil
	case rerr != nil:
		return errorResponse(id, rerr.code, rerr.data)
	case err != nil:
		return errorResponse(id, internalError, "")
	case result == nil:
		return nil
	}
	b, err := json.Marshal(result)
	if err != nil {
		// vanilla: EncodeJsonRpcException
		slog.Error("Failed to encode json rpc response", "method", name, "error", err)
		return errorResponse(id, internalError, err.Error())
	}
	return encode(response{JSONRPC: "2.0", ID: id, Result: b})
}

// dispatch looks the method up and runs it on params; a panic is recovered
// as an internal error.
// vanilla: Connection.dispatchIncomingRequest
func (s *Server) dispatch(c *conn, name string, params json.RawMessage) (result any, err error) {
	full, ok := qualify(name)
	if !ok {
		return nil, errInvalidRequest("Failed to parse method value: " + name)
	}
	m, ok := s.methods[full]
	if !ok {
		return nil, &rpcError{methodNotFound, "Method not found: " + name}
	}
	arg, err := m.arg(params)
	if err != nil {
		return nil, err
	}
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()
	return m.run(c, arg)
}

// qualify returns the method's full identifier, minecraft: when it names no
// namespace, or false when it is no identifier. vanilla: Identifier.tryParse
func qualify(name string) (string, bool) {
	if !protocol.ValidIdentifier(name) {
		return "", false
	}
	if i := strings.IndexByte(name, ':'); i > 0 {
		return name, true
	} else if i == 0 {
		name = name[1:]
	}
	return "minecraft:" + name, true
}

// arg picks the method's parameter out of params, nil when the request has
// none.
// vanilla: IncomingRpcMethod.Method.apply, IncomingRpcMethod.ParameterlessMethod.apply
func (m method) arg(params json.RawMessage) (json.RawMessage, error) {
	if m.param == "" {
		if params != nil && !emptyArray(params) {
			return nil, errInvalidParams("Expected no params, or an empty array")
		}
		return nil, nil
	}
	if params != nil {
		switch params[0] {
		case '{':
			var named map[string]json.RawMessage
			_ = json.Unmarshal(params, &named)
			v, ok := named[m.param]
			if !ok {
				return nil, errInvalidParams(fmt.Sprintf("Params passed by-name, but expected param [%s] does not exist", m.param))
			}
			return v, nil
		case '[':
			var list []json.RawMessage
			_ = json.Unmarshal(params, &list)
			if len(list) != 1 {
				return nil, errInvalidParams("Expected exactly one element in the params array")
			}
			return list[0], nil
		}
	}
	return nil, errInvalidParams("Expected params as array or named")
}

// emptyArray reports whether v is the JSON array [].
func emptyArray(v json.RawMessage) bool {
	return v[0] == '[' && len(bytes.TrimSpace(v[1:len(v)-1])) == 0
}
