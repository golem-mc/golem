package server

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/metrics"
)

// maxDebugSubscriptions is the most subscriptions one request may carry.
// vanilla: ServerboundDebugSubscriptionRequestPacket.SET_STREAM_CODEC
const maxDebugSubscriptions = 32

// handleDebugSubscriptionRequest records whether the client wants tick-time
// samples; each request replaces the set, kept even for a player not yet
// allowed them. vanilla: ServerPlayer.requestDebugSubscriptions
func (c *client) handleDebugSubscriptionRequest(r *protocol.Reader) error {
	n := r.ReadVarInt()
	if n < 0 || n > maxDebugSubscriptions {
		return c.decodeFailure("debug_subscription_request",
			fmt.Errorf("debug subscription request of %d entries out of range (max %d)", n, maxDebugSubscriptions))
	}
	want := false
	for i := int32(0); i < n && r.Err() == nil; i++ {
		id := r.ReadVarInt()
		if id < 0 || id >= protocol.DebugSubscriptionCount {
			return c.decodeFailure("debug_subscription_request",
				fmt.Errorf("debug subscription %d is not a registry entry", id))
		}
		if id == protocol.DebugSubscriptionTickTime {
			want = true
		}
	}
	if err := r.Err(); err != nil {
		return c.decodeFailure("debug_subscription_request", err)
	}
	c.tickSamples.Store(want)
	return nil
}

// sendTickSamples sends subscribed operators the F3 TPS chart sample of the
// cycle ending now; nothing is built without subscribers.
// vanilla: RemoteSampleLogger.useSample, ServerDebugSubscribers
func (s *Server) sendTickSamples(now time.Time) {
	var to []*client
	s.mu.RLock()
	for _, c := range s.clients {
		if c.tickSamples.Load() {
			to = append(to, c)
		}
	}
	s.mu.RUnlock()
	if len(to) == 0 {
		return
	}
	sample, ok := s.metrics.DebugSample(now)
	if !ok {
		return
	}
	data := debugSamplePayload(sample[:], protocol.DebugSampleTickTime)
	for _, c := range to {
		if s.ops.listed(c.profile.UUID) {
			c.send(protocol.IDPlayDebugSample, data)
		}
	}
}

// debugSamplePayload builds Debug Sample: the sample as a prefixed long
// array, then its type.
func debugSamplePayload(sample []int64, sampleType int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(int32(len(sample)))
	for _, v := range sample {
		b.PutI64(v)
	}
	b.PutVarInt(sampleType)
	return b.Bytes()
}

// ServerDir is the directory the server's files live in.
func (ws worldScope) ServerDir() string { return filepath.Dir(ws.srv.configPath) }

// Metrics hands the commands the main loop's recorder.
func (ws worldScope) Metrics() *metrics.Recorder { return ws.srv.metrics }
