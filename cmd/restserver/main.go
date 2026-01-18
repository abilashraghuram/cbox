package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"github.com/abilashraghuram/cbox/out/gen/serverapi"
	"github.com/abilashraghuram/cbox/pkg/callback"
	"github.com/abilashraghuram/cbox/pkg/config"
	"github.com/abilashraghuram/cbox/pkg/server"
)

const (
	API_VERSION = "v1"
)

// sendErrorResponse sends a standardized error response to the client.
func sendErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	resp := serverapi.ErrorResponse{
		Error: &serverapi.ErrorResponseError{
			Message: &message,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(resp)
}

type restServer struct {
	vmServer       *server.Server
	sessionManager *callback.SessionManager
}

// Health check endpoint for load balancer monitoring
func (s *restServer) healthCheck(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// startVM handles POST /v1/vms
func (s *restServer) startVM(w http.ResponseWriter, r *http.Request) {
	logger := log.WithField("api", "startVM")
	startTime := time.Now()

	var req serverapi.StartVMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.WithError(err).Error("Invalid request body")
		sendErrorResponse(
			w,
			http.StatusBadRequest,
			fmt.Sprintf("Invalid request format: %v", err))
		return
	}

	if req.GetVmName() == "" {
		logger.Error("Empty vm name")
		sendErrorResponse(
			w,
			http.StatusBadRequest,
			"Empty vm name")
		return
	}

	vmName := req.GetVmName()
	callbackUrl := req.GetCallbackUrl()

	resp, err := s.vmServer.StartVM(r.Context(), &req)
	if err != nil {
		logger.WithField("vmName", vmName).WithError(err).Error("Failed to start VM")
		sendErrorResponse(
			w,
			http.StatusInternalServerError,
			fmt.Sprintf("Failed to start VM: %v", err))
		return
	}

	// If callbackUrl is provided, register it with the session manager
	if callbackUrl != "" {
		_, err := s.sessionManager.RegisterHTTPCallback(vmName, callbackUrl)
		if err != nil {
			logger.WithFields(log.Fields{
				"vmName":      vmName,
				"callbackUrl": callbackUrl,
			}).WithError(err).Warn("Failed to register HTTP callback, callbacks will not work")
		} else {
			logger.WithFields(log.Fields{
				"vmName":      vmName,
				"callbackUrl": callbackUrl,
			}).Info("Registered HTTP callback for VM")
		}
	}

	elapsedTime := time.Since(startTime)
	logger.WithFields(log.Fields{
		"vmName":      vmName,
		"startupTime": elapsedTime.String(),
	}).Info("VM started successfully")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// destroyVM handles DELETE /v1/vms/{name}
func (s *restServer) destroyVM(w http.ResponseWriter, r *http.Request) {
	logger := log.WithField("api", "destroyVM")
	vars := mux.Vars(r)
	vmName := vars["name"]

	logger.WithField("vmName", vmName).Info("Destroying VM")

	// Remove callback session if exists
	s.sessionManager.RemoveSession(vmName)

	resp, err := s.vmServer.DestroyVM(r.Context(), vmName)
	if err != nil {
		logger.WithField("vmName", vmName).WithError(err).Error("Failed to destroy VM")
		sendErrorResponse(
			w,
			http.StatusInternalServerError,
			fmt.Sprintf("Failed to destroy VM: %v", err))
		return
	}

	logger.WithField("vmName", vmName).Info("VM destroyed successfully")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// destroyAllVMs handles DELETE /v1/vms
func (s *restServer) destroyAllVMs(w http.ResponseWriter, r *http.Request) {
	logger := log.WithField("api", "destroyAllVMs")
	logger.Info("Destroying all VMs")

	resp, err := s.vmServer.DestroyAllVMs(r.Context())
	if err != nil {
		logger.WithError(err).Error("Failed to destroy all VMs")
		sendErrorResponse(
			w,
			http.StatusInternalServerError,
			fmt.Sprintf("Failed to destroy all VMs: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// listAllVMs handles GET /v1/vms
func (s *restServer) listAllVMs(w http.ResponseWriter, r *http.Request) {
	logger := log.WithField("api", "listAllVMs")

	resp, err := s.vmServer.ListAllVMs(r.Context())
	if err != nil {
		logger.WithError(err).Error("Failed to list VMs")
		sendErrorResponse(
			w,
			http.StatusInternalServerError,
			fmt.Sprintf("Failed to list VMs: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// listVM handles GET /v1/vms/{name}
func (s *restServer) listVM(w http.ResponseWriter, r *http.Request) {
	logger := log.WithField("api", "listVM")
	vars := mux.Vars(r)
	vmName := vars["name"]

	resp, err := s.vmServer.ListVM(r.Context(), vmName)
	if err != nil {
		logger.WithField("vmName", vmName).WithError(err).Error("Failed to get VM info")
		sendErrorResponse(
			w,
			http.StatusInternalServerError,
			fmt.Sprintf("Failed to get VM info: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// vmExec handles POST /v1/vms/{name}/exec
func (s *restServer) vmExec(w http.ResponseWriter, r *http.Request) {
	logger := log.WithField("api", "vmExec")
	vars := mux.Vars(r)
	vmName := vars["name"]

	var req serverapi.VmExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.WithField("vmName", vmName).WithError(err).Error("Invalid request body")
		sendErrorResponse(
			w,
			http.StatusBadRequest,
			fmt.Sprintf("Invalid request format: %v", err))
		return
	}

	if req.GetCmd() == "" {
		logger.WithField("vmName", vmName).Error("Command cannot be empty")
		sendErrorResponse(
			w,
			http.StatusBadRequest,
			"Command cannot be empty")
		return
	}

	cmd := req.GetCmd()
	// Default to blocking if not specified
	blocking := true
	if req.Blocking != nil {
		blocking = *req.Blocking
	}

	resp, err := s.vmServer.VMExec(r.Context(), vmName, cmd, blocking)
	if err != nil {
		logger.WithFields(log.Fields{
			"vmName":   vmName,
			"cmd":      cmd,
			"blocking": blocking,
			"success":  false,
		}).Error("Failed to execute command")
		sendErrorResponse(
			w,
			http.StatusInternalServerError,
			fmt.Sprintf("Failed to execute command: %v", err))
		return
	}

	logger.WithFields(log.Fields{
		"vmName":   vmName,
		"cmd":      cmd,
		"blocking": blocking,
		"success":  true,
	}).Info("Successfully executed command")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// InternalCallbackRequest represents a callback request from a VM
type InternalCallbackRequest struct {
	VMName string          `json:"vmName"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// InternalCallbackResponse represents the response to an internal callback
type InternalCallbackResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// handleInternalCallback handles callback requests from VMs.
// This endpoint is called by the vsockserver running inside guest VMs.
func (s *restServer) handleInternalCallback(w http.ResponseWriter, r *http.Request) {
	logger := log.WithField("api", "handleInternalCallback")

	var req InternalCallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.WithError(err).Error("Invalid callback request body")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InternalCallbackResponse{
			Error: fmt.Sprintf("Invalid request format: %v", err),
		})
		return
	}

	if req.VMName == "" || req.Method == "" {
		logger.Error("Missing vmName or method in callback request")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(InternalCallbackResponse{
			Error: "vmName and method are required",
		})
		return
	}

	logger.WithFields(log.Fields{
		"vmName": req.VMName,
		"method": req.Method,
	}).Info("Processing callback from VM")

	// Route the callback to the registered HTTP callback URL
	result, err := s.sessionManager.RouteCallback(r.Context(), req.VMName, req.Method, req.Params)
	if err != nil {
		logger.WithFields(log.Fields{
			"vmName": req.VMName,
			"method": req.Method,
		}).WithError(err).Error("Failed to route callback")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(InternalCallbackResponse{
			Error: fmt.Sprintf("Callback failed: %v", err),
		})
		return
	}

	logger.WithFields(log.Fields{
		"vmName": req.VMName,
		"method": req.Method,
	}).Info("Callback completed successfully")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(InternalCallbackResponse{
		Result: result,
	})
}

func main() {
	var serverConfig *config.ServerConfig
	var configFile string

	app := &cli.App{
		Name:  "cbox-restserver",
		Usage: "A lightweight daemon for spawning and managing cloud-hypervisor based microVMs with exec and callback support.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Aliases:     []string{"c"},
				Usage:       "Path to config file",
				Destination: &configFile,
				Value:       "./config.yaml",
			},
		},
		Action: func(ctx *cli.Context) error {
			var err error
			serverConfig, err = config.GetServerConfig(configFile)
			if err != nil {
				return fmt.Errorf("server config not found: %v", err)
			}
			log.Infof("server config: %v", serverConfig)
			return nil
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.WithError(err).Fatal("server exited with error")
	}

	// Create the session manager for handling HTTP callback sessions
	sessionManager := callback.NewSessionManager()

	// Create the VM server
	vmServer, err := server.NewServer(*serverConfig, sessionManager)
	if err != nil {
		log.Fatalf("failed to create VM server: %v", err)
	}

	// Create REST server
	s := &restServer{
		vmServer:       vmServer,
		sessionManager: sessionManager,
	}
	r := mux.NewRouter()

	// Register routes
	r.HandleFunc("/"+API_VERSION+"/vms", s.startVM).Methods("POST")
	r.HandleFunc("/"+API_VERSION+"/vms/{name}", s.destroyVM).Methods("DELETE")
	r.HandleFunc("/"+API_VERSION+"/vms", s.destroyAllVMs).Methods("DELETE")
	r.HandleFunc("/"+API_VERSION+"/vms", s.listAllVMs).Methods("GET")
	r.HandleFunc("/"+API_VERSION+"/vms/{name}", s.listVM).Methods("GET")
	r.HandleFunc("/"+API_VERSION+"/vms/{name}/exec", s.vmExec).Methods("POST")
	r.HandleFunc("/"+API_VERSION+"/health", s.healthCheck).Methods("GET")

	// Internal endpoint for VM callbacks (called by vsockserver in guest)
	r.HandleFunc("/"+API_VERSION+"/internal/callback", s.handleInternalCallback).Methods("POST")

	// Start HTTP server
	srv := &http.Server{
		Addr:    serverConfig.Host + ":" + serverConfig.Port,
		Handler: r,
	}

	go func() {
		log.Printf("cbox-restserver listening on: %s:%s", serverConfig.Host, serverConfig.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down server...")
	if err := srv.Shutdown(context.Background()); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
	vmServer.DestroyAllVMs(context.Background())
	log.Println("Server stopped")
}
