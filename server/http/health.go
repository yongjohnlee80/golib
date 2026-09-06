package httpserver

import "net/http"

// Healthz returns a liveness handler: always 200. Register it wherever the
// deployment expects its liveness probe:
//
//	srv.HandleFunc("GET /healthz", httpserver.Healthz())
func Healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// Readyz returns a readiness handler with a drain gate: 200 while serving,
// 503 from the moment Shutdown begins — load balancers stop routing while
// in-flight work and long-lived sessions drain.
func (s *Server) Readyz() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if s.draining.Load() {
			Error(w, http.StatusServiceUnavailable, "draining")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	}
}
