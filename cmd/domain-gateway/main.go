package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ivan100-ivoop/cloudns-socket/gateway"
	"github.com/ivan100-ivoop/cloudns-socket/provider"
)

func main() {
	configDir, args := parseArgs()
	cfg, err := gateway.LoadConfig(configDir)
	if err != nil {
		fatal(err)
	}
	registry, err := provider.LoadRegistry(cfg.ProvidersDir(configDir))
	if err != nil {
		fatal(err)
	}
	defer registry.Close()

	if len(args) > 0 && args[0] == "check" {
		runCheck(registry, args[1:])
		return
	}
	if len(args) != 0 {
		usage()
	}
	runServer(configDir, cfg, registry)
}

func parseArgs() (string, []string) {
	flags := flag.NewFlagSet("domain-gateway", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configDir := flags.String("p", "", "config directory path")
	if err := flags.Parse(os.Args[1:]); err != nil || *configDir == "" {
		usage()
	}
	return *configDir, flags.Args()
}

func runCheck(registry *provider.Registry, args []string) {
	if len(args) != 1 {
		usage()
	}
	result, err := registry.Check(context.Background(), args[0])
	if err != nil {
		fmt.Printf("provider=%s\ndomain=%s\nresult=%s\nerror=%v\n", result.ProviderID, result.Domain, result.Result, err)
		os.Exit(1)
	}
	fmt.Printf("provider=%s\ndomain=%s\nresult=%s\n", result.ProviderID, result.Domain, result.Result)
}

func runServer(configDir string, cfg gateway.Config, registry *provider.Registry) {
	logger := log.New(os.Stdout, "", 0)
	server, err := gateway.NewServer(cfg, registry, logger)
	if err != nil {
		fatal(err)
	}
	address := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fatal(err)
	}
	defer listener.Close()
	logger.Printf("config_path=%s providers_loaded=%d listening=%s", configDir, registry.ProviderCount(), listener.Addr())

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serveErr:
		if err != nil && err != gateway.ErrServerClosed {
			fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-serveErr
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  domain-gateway -p <config-path>")
	fmt.Fprintln(os.Stderr, "  domain-gateway -p <config-path> check <domain>")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "ERROR:", err)
	os.Exit(1)
}
