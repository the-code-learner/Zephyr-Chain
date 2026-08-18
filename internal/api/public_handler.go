package api

import (
	"net/http"
	"strings"
)

// PublicHandlerOptions controls which development-only surfaces are exposed by
// the process-facing HTTP handler. Development endpoints are disabled unless
// explicitly enabled by the caller.
type PublicHandlerOptions struct {
	EnableDevEndpoints bool
}

// PublicHandler wraps the internal mux with process-boundary protections that
// should apply to externally exposed node HTTP traffic.
func (s *Server) PublicHandler(options PublicHandlerOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !options.EnableDevEndpoints && strings.HasPrefix(r.URL.Path, "/v1/dev/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "development endpoint disabled"})
			return
		}

		if r.URL.Path == "/v1/internal/snapshot" {
			if requestSourceNode(r) == "" {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "internal snapshot requires a peer source"})
				return
			}
			if err := s.validatePeerRequest(r); err != nil {
				writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
				return
			}
		}

		s.mux.ServeHTTP(w, r)
	})
}
