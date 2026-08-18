package api

import (
	"net/http"
	"strings"
)

const maxPublicRequestBodyBytes int64 = 1 << 20

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

		if requestMayHaveBody(r.Method) && r.Body != nil {
			if r.ContentLength > maxPublicRequestBodyBytes {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxPublicRequestBodyBytes)
		}

		if strings.HasPrefix(r.URL.Path, "/v1/internal/") && requestSourceNode(r) == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "internal endpoint requires a signed peer request"})
			return
		}

		s.mux.ServeHTTP(w, r)
	})
}

func requestMayHaveBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}
