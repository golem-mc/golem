package server

// TickEnded returns a channel closed when the main loop's tick in progress,
// or its next one if none is, has finished; nil while the loop is stopped.
// A tick's packets are queued before the channel closes, so a Ping Request
// sent after it is answered behind them.
func (s *Server) TickEnded() <-chan struct{} {
	s.backstopMu.RLock()
	running := s.tickRunning
	s.backstopMu.RUnlock()
	ended := s.tickEnded.Load()
	if !running || ended == nil {
		return nil
	}
	return *ended
}
