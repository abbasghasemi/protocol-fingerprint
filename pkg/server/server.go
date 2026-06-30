package server

import (
	"log"
	"strings"
	"sync"

	"github.com/pagpeter/trackme/pkg/parquet"
	"github.com/pagpeter/trackme/pkg/types"
)

// State holds all the global state previously scattered across the application
type State struct {
	Config          *types.Config
	TCPFingerprints sync.Map
	Local           bool
	Parquet         *parquet.Logger
}

// Server provides access to shared state and functionality
type Server struct {
	State *State
}

// NewServer creates a new server instance with initialized state
func NewServer() *Server {
	return &Server{
		State: &State{
			Config:          &types.Config{},
			TCPFingerprints: sync.Map{},
		},
	}
}

// GetConfig returns the loaded configuration
func (s *Server) GetConfig() *types.Config {
	return s.State.Config
}

// InitParquet starts the Parquet logger if enabled in the config. It must be
// called after the config has been loaded.
func (s *Server) InitParquet() {
	if !s.State.Config.LogToParquet {
		return
	}
	l, err := parquet.New(s.State.Config.ParquetDir, s.State.Config.ParquetLogIPs)
	if err != nil {
		log.Println("Failed to start parquet logger:", err)
		return
	}
	s.State.Parquet = l
	log.Println("Logging captured fingerprints to parquet dir:", s.State.Config.ParquetDir)
}

// LogParquet persists a captured response if the Parquet logger is enabled.
func (s *Server) LogParquet(res types.Response) {
	s.State.Parquet.Log(res)
}

// CloseParquet flushes and stops the Parquet logger.
func (s *Server) CloseParquet() {
	s.State.Parquet.Close()
}

// GetTCPFingerprints returns the TCP fingerprints map
func (s *Server) GetTCPFingerprints() *sync.Map {
	return &s.State.TCPFingerprints
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
