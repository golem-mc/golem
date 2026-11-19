package server

// withDims gives a hand-built Server its dimension list.
func withDims(s *Server, dims []*dimension) *Server {
	s.dimsList.Store(&dims)
	return s
}
