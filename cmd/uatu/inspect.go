package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/priyanshujain/uatu/internal/inspect"
)

type inspectOptions struct {
	port      int
	noOpen    bool
	dev       bool
	directory string
}

func parseInspectArgs(args []string, stderr io.Writer) (inspectOptions, error) {
	flagSet := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	var options inspectOptions
	flagSet.IntVar(&options.port, "port", 0, "TCP port to listen on (0 = ephemeral)")
	flagSet.BoolVar(&options.noOpen, "no-open", false, "do not open the default browser on startup")
	flagSet.BoolVar(&options.dev, "dev", false, "reverse-proxy non-API requests to "+inspect.DevTarget)
	if err := flagSet.Parse(args); err != nil {
		return inspectOptions{}, err
	}
	rest := flagSet.Args()
	if len(rest) > 1 {
		return inspectOptions{}, errors.New("inspect takes at most one positional argument (run or runs directory)")
	}
	if len(rest) == 1 {
		options.directory = rest[0]
	}
	return options, nil
}

func runInspect(options inspectOptions, stdout io.Writer) error {
	runsDirectory, deepLinkID, err := inspect.ResolveRunsDirectory(options.directory)
	if err != nil {
		return err
	}
	devTarget := ""
	if options.dev {
		devTarget = inspect.DevTarget
	}
	server, err := inspect.NewServer(inspect.ServerOptions{
		RunsDirectory: runsDirectory,
		DevTarget:     devTarget,
	})
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(options.port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	address := listener.Addr().(*net.TCPAddr)
	browseURL := buildBrowseURL(address, deepLinkID)
	fmt.Fprintf(stdout, "uatu inspect listening on %s (runs=%s)\n", browseURL, runsDirectory)

	context, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcherDone := make(chan struct{})
	go func() {
		_ = server.Watcher().Run(context)
		close(watcherDone)
	}()

	httpServer := &http.Server{Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second}
	serverDone := make(chan error, 1)
	go func() { serverDone <- httpServer.Serve(listener) }()

	if !options.noOpen {
		if err := openBrowser(browseURL); err != nil {
			fmt.Fprintf(stdout, "warning: could not open browser: %v\n", err)
		}
	}

	err = <-serverDone
	cancel()
	<-watcherDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func buildBrowseURL(address *net.TCPAddr, deepLinkID string) string {
	target := url.URL{Scheme: "http", Host: address.String(), Path: "/"}
	if deepLinkID != "" {
		target.Path = "/runs/" + deepLinkID
	}
	return target.String()
}

func openBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("cmd", "/c", "start", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}
