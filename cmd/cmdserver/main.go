package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/abilashraghuram/cbox/pkg/cmdserver"
	"github.com/gorilla/mux"
	"github.com/mattn/go-shellwords"
)

const (
	// Define a base directory to prevent path traversal
	baseDir = "/tmp/server_files"
)

// runCommandHandler handles "/cmd" POST requests.
func runCommandHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.WithField("api", "run_cmd").Error("method not allowed")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Cmd      string `json:"cmd"`
		Blocking bool   `json:"blocking,omitempty"`
	}
	// Block by default if not specified in the payload.
	req.Blocking = true

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		log.WithField("api", "run_cmd").Error("invalid json body")
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Cmd) == "" {
		log.WithField("api", "run_cmd").Error("empty command")
		http.Error(w, "Empty Command", http.StatusBadRequest)
		return
	}

	// Parse the command string using shellwords to handle quotes and escaped spaces
	parser := shellwords.NewParser()
	parts, err := parser.Parse(req.Cmd)
	if err != nil {
		log.WithFields(log.Fields{
			"api": "run_cmd",
		}).Errorf("failed to parse command string: %v", err)
		http.Error(w, fmt.Sprintf("failed to parse command string: %v", err), http.StatusBadRequest)
		return
	}

	if len(parts) == 0 {
		log.WithFields(log.Fields{
			"api": "run_cmd",
		}).Error("empty command string")
		http.Error(w, "empty command string", http.StatusBadRequest)
		return
	}

	cmdName := parts[0]
	cmdArgs := parts[1:]

	// Set up environment variables
	env := os.Environ()
	customPath := "/usr/local/bin:/usr/bin:/bin"
	env = append(env, "PATH="+customPath)

	// Create the command
	cmd := exec.Command("bash", "-c", req.Cmd)
	cmd.Env = env
	cmd.Dir = baseDir

	// Log the command execution details
	log.WithFields(log.Fields{
		"api":        "run_cmd",
		"cmd":        cmdName,
		"args":       cmdArgs,
		"workingDir": cmd.Dir,
	}).Info("Executing command")

	// Handle command execution based on blocking mode
	if req.Blocking {
		// Execute the command and capture the combined output in blocking mode
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.WithFields(log.Fields{
				"api":  "run_cmd",
				"cmd":  cmdName,
				"args": cmdArgs,
			}).Errorf("command execution failed output: %s err: %v", string(output), err)
			resp := cmdserver.RunCmdResponse{
				Error:  err.Error(),
				Output: string(output),
			}
			writeJSON(w, resp)
			return
		}

		// Log successful execution
		log.WithFields(log.Fields{
			"api":        "run_cmd",
			"cmd":        cmdName,
			"args":       cmdArgs,
			"output":     string(output),
			"workingDir": cmd.Dir,
		}).Info("command executed successfully")

		// Respond with the command output
		resp := cmdserver.RunCmdResponse{
			Output: string(output),
		}
		writeJSON(w, resp)
	} else {
		// Non-blocking mode: start the command but don't wait for it to complete
		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			log.WithFields(log.Fields{
				"api":  "run_cmd",
				"cmd":  cmdName,
				"args": cmdArgs,
			}).Errorf("failed to create stdout pipe: %v", err)
			resp := cmdserver.RunCmdResponse{
				Error: fmt.Sprintf("failed to create stdout pipe: %v", err),
			}
			writeJSON(w, resp)
			return
		}

		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			log.WithFields(log.Fields{
				"api":  "run_cmd",
				"cmd":  cmdName,
				"args": cmdArgs,
			}).Errorf("failed to create stderr pipe: %v", err)
			resp := cmdserver.RunCmdResponse{
				Error: fmt.Sprintf("failed to create stderr pipe: %v", err),
			}
			writeJSON(w, resp)
			return
		}

		// Start the command
		if err := cmd.Start(); err != nil {
			log.WithFields(log.Fields{
				"api":  "run_cmd",
				"cmd":  cmdName,
				"args": cmdArgs,
			}).Errorf("failed to start command: %v", err)
			resp := cmdserver.RunCmdResponse{
				Error: fmt.Sprintf("failed to start command: %v", err),
			}
			writeJSON(w, resp)
			return
		}

		// Start goroutines to handle stdout and stderr in the background
		go func() {
			scanner := bufio.NewScanner(stdoutPipe)
			for scanner.Scan() {
				log.WithFields(log.Fields{
					"api":    "run_cmd",
					"cmd":    cmdName,
					"stdout": scanner.Text(),
				}).Debug("command stdout")
			}
		}()

		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				log.WithFields(log.Fields{
					"api":    "run_cmd",
					"cmd":    cmdName,
					"stderr": scanner.Text(),
				}).Debug("command stderr")
			}
		}()

		// Start a goroutine to wait for the command to complete
		go func() {
			err := cmd.Wait()
			if err != nil {
				log.WithFields(log.Fields{
					"api":  "run_cmd",
					"cmd":  cmdName,
					"args": cmdArgs,
				}).Errorf("command execution failed: %v", err)
			} else {
				log.WithFields(log.Fields{
					"api":  "run_cmd",
					"cmd":  cmdName,
					"args": cmdArgs,
				}).Info("command completed successfully")
			}
		}()

		// Respond immediately with a success message
		resp := cmdserver.RunCmdResponse{
			Output: fmt.Sprintf("Command '%s' started in background", cmd.String()),
		}
		writeJSON(w, resp)
	}
}

// indexHandler handles "/" GET requests.
func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]string{
		"msg": "Hello from cbox-cmdserver",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Utility function to write JSON response
func writeJSON(w http.ResponseWriter, resp cmdserver.RunCmdResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func main() {
	// Ensure base directory exists.
	err := os.MkdirAll(baseDir, os.ModePerm)
	if err != nil {
		log.Fatalf("Failed to create base directory: %v", err)
	}

	// Initialize Gorilla Mux router.
	router := mux.NewRouter()

	// Register routes with their respective handlers.
	router.HandleFunc("/", indexHandler).Methods(http.MethodGet)
	router.HandleFunc("/cmd", runCommandHandler).Methods(http.MethodPost)

	// Optionally, add logging middleware.
	router.Use(loggingMiddleware)

	port := "4031"
	log.Printf("cbox-cmdserver is running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}

// Optional: Middleware for logging requests.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[%s] %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
