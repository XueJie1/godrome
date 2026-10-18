//go:build !linux

package mpris

import "time"

// Server is absent away from Linux, where no session-bus media interface
// is claimed. The API stays the same so the app compiles everywhere.
type Server struct{}

func Start(Controls) (*Server, error) { return nil, nil }

func (*Server) Update(Snapshot)      {}
func (*Server) Seeked(time.Duration) {}
func (*Server) Close()               {}
