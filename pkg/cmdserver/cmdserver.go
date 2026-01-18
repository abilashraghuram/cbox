package cmdserver

// RunCmdResponse structure for JSON responses from command execution
type RunCmdResponse struct {
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}
