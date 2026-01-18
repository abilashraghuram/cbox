package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// Default timeout for callback responses
	defaultCallbackTimeout = 30 * time.Second

	// HTTP client timeout for HTTP callbacks
	httpCallbackTimeout = 30 * time.Second
)

// CallbackRequest represents a callback request from the guest VM to the client.
type CallbackRequest struct {
	ID        string          `json:"id"`
	VMName    string          `json:"vmName,omitempty"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	Timestamp int64           `json:"timestamp"`
}

// CallbackResponse represents a response from the client to a callback request.
type CallbackResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *CallbackError  `json:"error,omitempty"`
}

// CallbackError represents an error in a callback response.
type CallbackError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Session represents an HTTP callback session for a VM.
type Session struct {
	ID          string
	VMName      string
	CallbackURL string
	httpClient  *http.Client
}

// SessionManager manages all active callback sessions.
type SessionManager struct {
	lock     sync.RWMutex
	sessions map[string]*Session // keyed by vmName
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// RegisterHTTPCallback registers an HTTP callback URL for a VM.
// This is called when a VM is started with a callbackUrl parameter.
func (m *SessionManager) RegisterHTTPCallback(vmName string, callbackURL string) (*Session, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	// Check if session already exists for this VM
	if existing, ok := m.sessions[vmName]; ok {
		// Close the existing session
		existing.Close()
	}

	session := &Session{
		ID:          fmt.Sprintf("%s-http-%d", vmName, time.Now().UnixNano()),
		VMName:      vmName,
		CallbackURL: callbackURL,
		httpClient: &http.Client{
			Timeout: httpCallbackTimeout,
		},
	}

	m.sessions[vmName] = session

	log.WithFields(log.Fields{
		"sessionId":   session.ID,
		"vmName":      vmName,
		"callbackURL": callbackURL,
	}).Info("HTTP callback session registered")

	return session, nil
}

// GetSession returns the session for the given VM name.
func (m *SessionManager) GetSession(vmName string) *Session {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.sessions[vmName]
}

// HasSession returns true if a session exists for the given VM name.
func (m *SessionManager) HasSession(vmName string) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()
	_, exists := m.sessions[vmName]
	return exists
}

// RemoveSession removes and closes the session for the given VM.
func (m *SessionManager) RemoveSession(vmName string) {
	m.lock.Lock()
	session := m.sessions[vmName]
	delete(m.sessions, vmName)
	m.lock.Unlock()

	if session != nil {
		session.Close()
		log.WithFields(log.Fields{
			"sessionId": session.ID,
			"vmName":    vmName,
		}).Info("Session removed")
	}
}

// RouteCallback routes a callback from a VM to the registered HTTP callback URL.
func (m *SessionManager) RouteCallback(ctx context.Context, vmName string, method string, params json.RawMessage) (json.RawMessage, error) {
	session := m.GetSession(vmName)
	if session == nil {
		return nil, fmt.Errorf("no active callback session for VM: %s", vmName)
	}

	// Set timeout if not already set in context
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultCallbackTimeout)
		defer cancel()
	}

	return session.sendCallback(ctx, vmName, method, params)
}

// Close closes the session and releases resources.
func (s *Session) Close() {
	if s.httpClient != nil {
		s.httpClient.CloseIdleConnections()
	}

	log.WithFields(log.Fields{
		"sessionId": s.ID,
		"vmName":    s.VMName,
	}).Debug("Session closed")
}

// sendCallback sends a callback via HTTP POST to the callback URL.
func (s *Session) sendCallback(ctx context.Context, vmName string, method string, params json.RawMessage) (json.RawMessage, error) {
	// Create the callback request
	req := &CallbackRequest{
		ID:        fmt.Sprintf("%s-%d", vmName, time.Now().UnixNano()),
		VMName:    vmName,
		Method:    method,
		Params:    params,
		Timestamp: time.Now().Unix(),
	}

	// Serialize the request
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal callback request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.CallbackURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	log.WithFields(log.Fields{
		"sessionId":   s.ID,
		"vmName":      vmName,
		"method":      method,
		"callbackURL": s.CallbackURL,
	}).Debug("Sending HTTP callback")

	// Send the request
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP callback request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read callback response: %w", err)
	}

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP callback returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the response
	var callbackResp CallbackResponse
	if err := json.Unmarshal(respBody, &callbackResp); err != nil {
		// If we can't parse as CallbackResponse, return the raw body as result
		log.WithFields(log.Fields{
			"sessionId": s.ID,
			"vmName":    vmName,
			"method":    method,
		}).Debug("Response is not in CallbackResponse format, returning raw body")
		return respBody, nil
	}

	// Check for error in response
	if callbackResp.Error != nil {
		return nil, fmt.Errorf("callback error [%d]: %s", callbackResp.Error.Code, callbackResp.Error.Message)
	}

	log.WithFields(log.Fields{
		"sessionId": s.ID,
		"vmName":    vmName,
		"method":    method,
	}).Debug("HTTP callback completed successfully")

	return callbackResp.Result, nil
}
