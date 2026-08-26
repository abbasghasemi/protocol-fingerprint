package server

import (
	"strings"
	"sync"
	"time"

	"github.com/pagpeter/trackme/pkg/types"
)

// State holds all the global state previously scattered across the application
type State struct {
	Config             *types.Config
	TCPFingerprints    sync.Map
	TCPSynFingerprints sync.Map
	Local              bool
}

type storedTCPSyn struct {
	details    types.TCPSynDetails
	p0f        string
	capturedAt time.Time
}

// Server provides access to shared state and functionality
type Server struct {
	State *State
}

// NewServer creates a new server instance with initialized state
func NewServer() *Server {
	return &Server{
		State: &State{
			Config:             &types.Config{},
			TCPFingerprints:    sync.Map{},
			TCPSynFingerprints: sync.Map{},
		},
	}
}

// GetConfig returns the loaded configuration
func (s *Server) GetConfig() *types.Config {
	return s.State.Config
}

// GetTCPFingerprints returns the TCP fingerprints map
func (s *Server) GetTCPFingerprints() *sync.Map {
	return &s.State.TCPFingerprints
}

// StoreTCPSyn records an inbound SYN under the same remote-address string that
// net.Conn.RemoteAddr returns, allowing the HTTP handler to claim it later.
func (s *Server) StoreTCPSyn(remoteAddr string, details types.TCPSynDetails, p0f string) {
	// Preserve the first SYN when the client retransmits the same connection.
	s.State.TCPSynFingerprints.LoadOrStore(remoteAddr, &storedTCPSyn{
		details:    details,
		p0f:        p0f,
		capturedAt: time.Now(),
	})
}

// TakeTCPSyn returns and removes the SYN for one accepted connection.
func (s *Server) TakeTCPSyn(remoteAddr string) (types.TCPSynDetails, string, bool) {
	v, ok := s.State.TCPSynFingerprints.LoadAndDelete(remoteAddr)
	if !ok {
		return types.TCPSynDetails{}, "", false
	}
	stored, ok := v.(*storedTCPSyn)
	if !ok {
		return types.TCPSynDetails{}, "", false
	}
	return stored.details, stored.p0f, true
}

// PruneTCPSyn removes SYNs that never reached the HTTP handler (failed TLS
// handshakes, scans, and abandoned connections).
func (s *Server) PruneTCPSyn(before time.Time) {
	s.State.TCPSynFingerprints.Range(func(key, value any) bool {
		stored, ok := value.(*storedTCPSyn)
		if !ok || stored.capturedAt.Before(before) {
			s.State.TCPSynFingerprints.CompareAndDelete(key, value)
		}
		return true
	})
}

// GetAdmin returns the CORS key configuration
func (s *Server) GetAdmin() (string, bool) {
	return s.State.Config.CorsKey, s.State.Config.CorsKey != ""
}

// GetUserAgent extracts the user agent from a response
func GetUserAgent(res types.Response) string {
	var headers []string
	var ua string

	if res.HTTPVersion == "h2" {
		return res.UserAgent
	} else {
		if res.Http1 == nil {
			return ""
		}
		headers = res.Http1.Headers
	}

	for _, header := range headers {
		lower := strings.ToLower(header)
		if strings.HasPrefix(lower, "user-agent: ") {
			ua = strings.Split(header, ": ")[1]
		}
	}

	return ua
}

// SetLocal sets the local development flag
func (s *Server) SetLocal(local bool) {
	s.State.Local = local
}

// IsLocal returns whether we're running in local development mode
func (s *Server) IsLocal() bool {
	return s.State.Local
}
