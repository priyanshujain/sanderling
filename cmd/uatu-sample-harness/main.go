package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/priyanshujain/uatu/internal/agent"
)

func main() {
	port := flag.Int("port", 0, "TCP port to listen on (0 = ephemeral)")
	socketName := flag.String("socket-name", "uatu-agent", "abstract socket name on the device")
	cycles := flag.Int("cycles", 3, "number of snapshot cycles to run")
	serial := flag.String("serial", "", "adb device serial (empty = default)")
	cycleInterval := flag.Duration("interval", 500*time.Millisecond, "delay between snapshot cycles")
	flag.Parse()

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		die("listen: %v", err)
	}
	defer listener.Close()
	listenerAddr := listener.Addr().(*net.TCPAddr)
	fmt.Printf("listening on 127.0.0.1:%d\n", listenerAddr.Port)

	if err := adbReverse(*serial, *socketName, listenerAddr.Port); err != nil {
		die("adb reverse: %v", err)
	}
	defer func() {
		if err := adbReverseRemove(*serial, *socketName); err != nil {
			fmt.Fprintf(os.Stderr, "adb reverse cleanup: %v\n", err)
		}
	}()
	fmt.Printf("forwarded localabstract:%s -> tcp:%d on device\n", *socketName, listenerAddr.Port)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	server := agent.NewServer(listener)
	fmt.Println("waiting for SDK to connect (launch the app under test on the device)...")
	conn, err := server.Accept(ctx)
	if err != nil {
		die("accept: %v", err)
	}
	defer conn.Close()
	hello := conn.Hello()
	fmt.Printf("HELLO from platform=%s app=%s sdk=%s\n", hello.Platform, hello.AppPackage, hello.Version)

	for index := 1; index <= *cycles; index++ {
		snapshotCtx, snapshotCancel := context.WithTimeout(ctx, 5*time.Second)
		state, err := conn.Snapshot(snapshotCtx)
		snapshotCancel()
		if err != nil {
			die("snapshot #%d: %v", index, err)
		}
		marshalled, _ := json.Marshal(state.Snapshots)
		fmt.Printf("STATE %d: %s\n", index, marshalled)
		if err := conn.Release(ctx); err != nil {
			die("release #%d: %v", index, err)
		}
		if index < *cycles {
			time.Sleep(*cycleInterval)
		}
	}
	fmt.Println("round-trip complete")
}

func adbReverse(serial, socketName string, port int) error {
	commandArguments := []string{"reverse", fmt.Sprintf("localabstract:%s", socketName), fmt.Sprintf("tcp:%d", port)}
	if serial != "" {
		commandArguments = append([]string{"-s", serial}, commandArguments...)
	}
	command := exec.Command("adb", commandArguments...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func adbReverseRemove(serial, socketName string) error {
	commandArguments := []string{"reverse", "--remove", fmt.Sprintf("localabstract:%s", socketName)}
	if serial != "" {
		commandArguments = append([]string{"-s", serial}, commandArguments...)
	}
	return exec.Command("adb", commandArguments...).Run()
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
