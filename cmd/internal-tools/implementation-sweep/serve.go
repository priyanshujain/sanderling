package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const (
	// serverStartTimeout covers a cold vite start on a host already running
	// five other implementations.
	serverStartTimeout  = 90 * time.Second
	serverPollInterval  = 250 * time.Millisecond
	serverShutdownGrace = 10 * time.Second
)

// server is one implementation's preview server. It runs in its own process
// group so that stopping it takes the whole vite tree with it: a leaked server
// holds its port, and the next sweep against that implementation would be
// served by the previous build.
type server struct {
	command *exec.Cmd
	logFile *os.File
	exited  chan struct{}
}

func startServer(
	ctx context.Context,
	configuration config,
	target implementation,
	logPath string,
) (*server, error) {
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, configuration.bunPath,
		"run", "preview", "--port", fmt.Sprint(target.Port), "--strictPort")
	command.Dir = target.Directory
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		logFile.Close()
		return nil, err
	}
	running := &server{
		command: command,
		logFile: logFile,
		exited:  make(chan struct{}),
	}
	go func() {
		command.Wait()
		close(running.exited)
	}()
	return running, nil
}

// waitReady polls the served page until it answers. A server that exits first
// is reported as such rather than waited on for the full timeout, because the
// usual cause is a port already taken and that answer is in the log.
func (s *server) waitReady(ctx context.Context, url string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(serverStartTimeout)
	for {
		select {
		case <-s.exited:
			return fmt.Errorf("server exited before it answered %s", url)
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		response, err := client.Get(url)
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"server did not answer %s within %s",
				url,
				serverStartTimeout,
			)
		}
		time.Sleep(serverPollInterval)
	}
}

func (s *server) stop() {
	if s.command.Process != nil {
		group := -s.command.Process.Pid
		syscall.Kill(group, syscall.SIGTERM)
		select {
		case <-s.exited:
		case <-time.After(serverShutdownGrace):
			syscall.Kill(group, syscall.SIGKILL)
			<-s.exited
		}
	}
	s.logFile.Close()
}
