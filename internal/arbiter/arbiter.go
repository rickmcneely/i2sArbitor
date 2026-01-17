package arbiter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zditech/i2sarbitor/internal/config"
)

// ServiceStatus represents the current state of a managed service
type ServiceStatus struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	BaseURL     string `json:"base_url"`
	Online      bool   `json:"online"`
	Locked      bool   `json:"locked"`
	Active      bool   `json:"active"`
	Priority    int    `json:"priority"`
	LastCheck   string `json:"last_check"`
	Error       string `json:"error,omitempty"`
}

// Arbiter manages I2S service arbitration
type Arbiter struct {
	cfg            *config.Config
	services       map[string]*ServiceStatus
	activeService  string
	mu             sync.RWMutex
	stopChan       chan struct{}
	client         *http.Client
}

// New creates a new Arbiter instance
func New(cfg *config.Config) *Arbiter {
	a := &Arbiter{
		cfg:      cfg,
		services: make(map[string]*ServiceStatus),
		stopChan: make(chan struct{}),
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}

	// Initialize service status entries
	for _, svc := range cfg.Services {
		a.services[svc.Name] = &ServiceStatus{
			Name:        svc.Name,
			DisplayName: svc.DisplayName,
			BaseURL:     svc.BaseURL,
			Priority:    svc.Priority,
			Online:      false,
			Locked:      false,
			Active:      false,
		}
	}

	return a
}

// StartMonitoring begins polling services for status
func (a *Arbiter) StartMonitoring() {
	go func() {
		ticker := time.NewTicker(time.Duration(a.cfg.PollIntervalMs) * time.Millisecond)
		defer ticker.Stop()

		// Initial check
		a.pollAllServices()

		for {
			select {
			case <-ticker.C:
				a.pollAllServices()
			case <-a.stopChan:
				return
			}
		}
	}()
	log.Info().Msg("service monitoring started")
}

// StopMonitoring stops the polling loop
func (a *Arbiter) StopMonitoring() {
	close(a.stopChan)
	log.Info().Msg("service monitoring stopped")
}

// GetAllStatus returns status of all managed services
func (a *Arbiter) GetAllStatus() []ServiceStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()

	statuses := make([]ServiceStatus, 0, len(a.services))
	for _, svc := range a.services {
		statuses = append(statuses, *svc)
	}
	return statuses
}

// GetServiceStatus returns status of a specific service
func (a *Arbiter) GetServiceStatus(name string) (*ServiceStatus, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	svc, ok := a.services[name]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", name)
	}
	return svc, nil
}

// GetActiveService returns the name of the currently active service
func (a *Arbiter) GetActiveService() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeService
}

// ActivateService activates a service by unlocking it and locking all others
func (a *Arbiter) ActivateService(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Verify service exists
	target, ok := a.services[name]
	if !ok {
		return fmt.Errorf("service not found: %s", name)
	}

	if !target.Online {
		return fmt.Errorf("service is offline: %s", name)
	}

	log.Info().Str("service", name).Msg("activating service")

	// Lock all other services first
	for svcName, svc := range a.services {
		if svcName != name && svc.Online {
			if err := a.lockServiceInternal(svcName, true); err != nil {
				log.Warn().Err(err).Str("service", svcName).Msg("failed to lock service")
			}
		}
	}

	// Unlock the target service
	if err := a.lockServiceInternal(name, false); err != nil {
		return fmt.Errorf("failed to unlock service: %w", err)
	}

	a.activeService = name
	log.Info().Str("service", name).Msg("service activated")
	return nil
}

// DeactivateAll locks all services
func (a *Arbiter) DeactivateAll() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	log.Info().Msg("deactivating all services")

	var lastErr error
	for name, svc := range a.services {
		if svc.Online {
			if err := a.lockServiceInternal(name, true); err != nil {
				log.Warn().Err(err).Str("service", name).Msg("failed to lock service")
				lastErr = err
			}
		}
	}

	a.activeService = ""
	return lastErr
}

// LockService locks or unlocks a specific service
// When unlocking, all other services are locked first to ensure only one is unlocked
func (a *Arbiter) LockService(name string, lock bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, ok := a.services[name]; !ok {
		return fmt.Errorf("service not found: %s", name)
	}

	// If unlocking, lock all other services first
	if !lock {
		for svcName, svc := range a.services {
			if svcName != name && svc.Online && !svc.Locked {
				if err := a.lockServiceInternal(svcName, true); err != nil {
					log.Warn().Err(err).Str("service", svcName).Msg("failed to lock service")
				}
			}
		}
	}

	return a.lockServiceInternal(name, lock)
}

// lockServiceInternal performs the actual lock/unlock API call
// Must be called with mutex held
func (a *Arbiter) lockServiceInternal(name string, lock bool) error {
	svc := a.services[name]

	// Different services have different lock APIs
	var err error
	switch name {
	case "usboveri2s":
		err = a.lockUSBOverI2S(svc.BaseURL, lock)
	case "usbaudio":
		err = a.lockUSBAudio(svc.BaseURL, lock)
	default:
		// Generic lock API (try usbAudio style first)
		err = a.lockUSBAudio(svc.BaseURL, lock)
	}

	if err != nil {
		svc.Error = err.Error()
		return err
	}

	svc.Locked = lock
	svc.Error = ""

	if lock && a.activeService == name {
		a.activeService = ""
	}

	return nil
}

// lockUSBOverI2S handles lock API for usbOverI2S service
func (a *Arbiter) lockUSBOverI2S(baseURL string, lock bool) error {
	var method string
	if lock {
		method = http.MethodPost
	} else {
		method = http.MethodDelete
	}

	req, err := http.NewRequest(method, baseURL+"/api/v1/lock", nil)
	if err != nil {
		return err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("lock request failed: %s", string(body))
	}

	return nil
}

// lockUSBAudio handles lock API for usbAudio service
func (a *Arbiter) lockUSBAudio(baseURL string, lock bool) error {
	payload := map[string]bool{"locked": lock}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/lock", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("lock request failed: %s", string(respBody))
	}

	return nil
}

// pollAllServices checks the status of all registered services
func (a *Arbiter) pollAllServices() {
	a.mu.Lock()
	defer a.mu.Unlock()

	for name := range a.services {
		a.pollServiceInternal(name)
	}

	// Enforce single unlocked service constraint
	a.enforceSingleUnlocked()
}

// enforceSingleUnlocked ensures only one service is unlocked at a time
// Must be called with mutex held
func (a *Arbiter) enforceSingleUnlocked() {
	var unlockedServices []string
	for name, svc := range a.services {
		if svc.Online && !svc.Locked {
			unlockedServices = append(unlockedServices, name)
		}
	}

	// If more than one service is unlocked, lock all but the active one (or first by priority)
	if len(unlockedServices) > 1 {
		log.Warn().Int("count", len(unlockedServices)).Msg("multiple services unlocked, enforcing constraint")

		// Determine which one to keep unlocked (prefer active, then lowest priority number)
		keepUnlocked := unlockedServices[0]
		if a.activeService != "" {
			for _, name := range unlockedServices {
				if name == a.activeService {
					keepUnlocked = name
					break
				}
			}
		} else {
			// Find lowest priority (highest precedence)
			lowestPriority := a.services[keepUnlocked].Priority
			for _, name := range unlockedServices {
				if a.services[name].Priority < lowestPriority {
					lowestPriority = a.services[name].Priority
					keepUnlocked = name
				}
			}
		}

		// Lock all others
		for _, name := range unlockedServices {
			if name != keepUnlocked {
				log.Info().Str("service", name).Msg("auto-locking service to enforce single unlock constraint")
				if err := a.lockServiceInternal(name, true); err != nil {
					log.Error().Err(err).Str("service", name).Msg("failed to auto-lock service")
				}
			}
		}
	}
}

// pollServiceInternal checks a single service status
// Must be called with mutex held
func (a *Arbiter) pollServiceInternal(name string) {
	svc := a.services[name]
	svc.LastCheck = time.Now().Format(time.RFC3339)

	// Different services have different status endpoints
	var statusURL string
	switch name {
	case "usboveri2s":
		statusURL = svc.BaseURL + "/api/v1/player/status"
	default:
		statusURL = svc.BaseURL + "/api/v1/status"
	}

	resp, err := a.client.Get(statusURL)
	if err != nil {
		svc.Online = false
		svc.Active = false
		svc.Error = err.Error()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		svc.Online = false
		svc.Active = false
		svc.Error = fmt.Sprintf("status code: %d", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		svc.Online = false
		svc.Error = err.Error()
		return
	}

	// Parse status response
	var status map[string]interface{}
	if err := json.Unmarshal(body, &status); err != nil {
		svc.Online = false
		svc.Error = err.Error()
		return
	}

	svc.Online = true
	svc.Error = ""

	// Handle usbOverI2S response format: {"success":true,"data":{...}}
	if data, ok := status["data"].(map[string]interface{}); ok {
		status = data
	}

	// Check locked status
	if locked, ok := status["locked"].(bool); ok {
		svc.Locked = locked
	}

	// Check if service is actively doing something
	// For usbOverI2S: check player state
	if state, ok := status["state"].(string); ok {
		svc.Active = state == "playing"
	}
	// For usbAudio: check active field
	if active, ok := status["active"].(bool); ok {
		svc.Active = active
	}

	// Update active service tracking
	if svc.Active && !svc.Locked {
		a.activeService = name
	} else if a.activeService == name && !svc.Active {
		a.activeService = ""
	}
}
