package api

import (
	"context"
	"net/http"
	"time"
)

// handleHealthz implements GET /api/healthz: 200 when the server can reach
// Postgres, 503 otherwise. Unauthenticated and reveals nothing else, so
// container health checks and load balancers can call it.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Pool().Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
