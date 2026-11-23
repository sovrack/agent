package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type CommandRequest struct {
	ID         string            `json:"id"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	WorkingDir string            `json:"workingDir"`
	Env        map[string]string `json:"env"`
	TimeoutSec int               `json:"timeoutSec"`
}

type CommandResult struct {
	ID         string `json:"id"`
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
}

type WriteFileRequest struct {
	Path        string `json:"path"`
	Content     string `json:"content"`
	Permissions uint32 `json:"permissions"`
}

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Execute(ctx context.Context, req CommandRequest, onOutputChunk func(chunk string)) CommandResult {
	start := time.Now()
	timeout := 10 * time.Minute
	if req.TimeoutSec > 0 {
		timeout = time.Duration(req.TimeoutSec) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if len(req.Args) == 0 {
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(execCtx, "cmd", "/C", req.Command)
		} else {
			cmd = exec.CommandContext(execCtx, "sh", "-c", req.Command)
		}
	} else {
		cmd = exec.CommandContext(execCtx, req.Command, req.Args...)
	}

	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}

	// Environment
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	var stdoutWriter io.Writer = &stdoutBuf
	var stderrWriter io.Writer = &stderrBuf

	if onOutputChunk != nil {
		stdoutWriter = io.MultiWriter(&stdoutBuf, &chunkWriter{callback: onOutputChunk})
		stderrWriter = io.MultiWriter(&stderrBuf, &chunkWriter{callback: onOutputChunk})
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	err := cmd.Run()
	duration := time.Since(start).Milliseconds()

	exitCode := 0
	errMsg := ""
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
			errMsg = err.Error()
		}
	}

	return CommandResult{
		ID:         req.ID,
		ExitCode:   exitCode,
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		DurationMs: duration,
		Error:      errMsg,
	}
}

func (r *Runner) WriteFile(req WriteFileRequest) error {
	dir := filepath.Dir(req.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	perm := os.FileMode(0644)
	if req.Permissions > 0 {
		perm = os.FileMode(req.Permissions)
	}

	if err := os.WriteFile(req.Path, []byte(req.Content), perm); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

type chunkWriter struct {
	callback func(string)
}

func (w *chunkWriter) Write(p []byte) (n int, err error) {
	if len(p) > 0 && w.callback != nil {
		w.callback(string(p))
	}
	return len(p), nil
}
