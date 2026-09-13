package server

import (
	"encoding/json"
	"net/http"
)

// healthzResponse is the GET /healthz response body: a minimal liveness
// signal for load balancers and container probes.
type healthzResponse struct {
	Status string `json:"status"`
}

// registerRoutes attaches the server's HTTP handlers to mux.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealthz)
}

// handleHealthz reports basic liveness. It is unauthenticated by design
// (see docs/ARCHITECTURE.md, "Operational Endpoints").
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(healthzResponse{Status: "ok"}); err != nil {
		s.log.Error("healthz encode failed", "error", err)
	}
}
