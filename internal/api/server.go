package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
	"github.com/zditech/i2sarbitor/internal/arbiter"
	"github.com/zditech/i2sarbitor/internal/config"
)

//go:embed web
var webFS embed.FS

// Server handles HTTP requests
type Server struct {
	cfg     *config.Config
	arbiter *arbiter.Arbiter
	server  *http.Server
}

// NewServer creates a new API server
func NewServer(cfg *config.Config, arb *arbiter.Arbiter) *Server {
	return &Server{
		cfg:     cfg,
		arbiter: arb,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/status", s.handleGetStatus)
		r.Get("/services", s.handleGetServices)
		r.Get("/services/{name}", s.handleGetService)
		r.Post("/services/{name}/activate", s.handleActivateService)
		r.Post("/services/{name}/lock", s.handleLockService)
		r.Delete("/services/{name}/lock", s.handleUnlockService)
		r.Post("/deactivate-all", s.handleDeactivateAll)
	})

	// Health check
	r.Get("/health", s.handleHealth)

	// Static files (web UI)
	webContent, _ := fs.Sub(webFS, "web")
	fileServer := http.FileServer(http.FS(webContent))
	r.Handle("/*", fileServer)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.cfg.APIPort),
		Handler: r,
	}

	log.Info().Int("port", s.cfg.APIPort).Msg("API server starting")
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// corsMiddleware adds CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleHealth returns service health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

// handleGetStatus returns overall arbiter status
func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	services := s.arbiter.GetAllStatus()
	active := s.arbiter.GetActiveService()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active_service": active,
		"services":       services,
	})
}

// handleGetServices returns all managed services
func (s *Server) handleGetServices(w http.ResponseWriter, r *http.Request) {
	services := s.arbiter.GetAllStatus()
	writeJSON(w, http.StatusOK, services)
}

// handleGetService returns a specific service status
func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	svc, err := s.arbiter.GetServiceStatus(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, svc)
}

// handleActivateService activates a service
func (s *Server) handleActivateService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.arbiter.ActivateService(name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"active_service": name,
	})
}

// handleLockService locks a specific service
func (s *Server) handleLockService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.arbiter.LockService(name, true); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"service": name,
		"locked":  true,
	})
}

// handleUnlockService unlocks a specific service
func (s *Server) handleUnlockService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.arbiter.LockService(name, false); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"service": name,
		"locked":  false,
	})
}

// handleDeactivateAll locks all services
func (s *Server) handleDeactivateAll(w http.ResponseWriter, r *http.Request) {
	if err := s.arbiter.DeactivateAll(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"active_service": "",
	})
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
