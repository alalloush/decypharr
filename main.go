package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/sirrobot01/decypharr/internal/config"

	"github.com/sirrobot01/decypharr/cmd/decypharr"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("FATAL: Recovered from panic in main: %v\n", r)
			debug.PrintStack()
		}
	}()

	opts := parseFlags(os.Args[1:])
	configPath, pprofAddr := opts.configPath, opts.pprofAddr

	// get enable pprof flag from environment variable if not set via flag
	enablePprof := os.Getenv("ENABLE_PPROF") != ""

	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		configPath = filepath.Join(home, ".decypharr")
	}

	config.SetConfigPath(configPath)
	config.Get()

	// Buffer pools are owned by their subsystems: the DFS cache (vfs.NewCache)
	// and the usenet reader each create a buffer.Pool with their own configured
	// RAM budget and disk limit.

	// Start pprof server if enabled. It has no authentication, so it listens
	// on loopback unless -pprof names another address.
	if pprofAddr != "" && enablePprof {
		go func() {
			log.Printf("Starting pprof server on %s (pass -pprof :6060 to listen on every interface)", pprofAddr)
			if err := http.ListenAndServe(pprofAddr, nil); err != nil {
				log.Printf("pprof server error: %v", err)
			}
		}()
	}

	// Create a context canceled on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := decypharr.Start(ctx); err != nil {
		log.Fatal(err)
	}
}

type options struct {
	configPath string
	pprofAddr  string
}

func parseFlags(args []string) options {
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var opts options
	flags.StringVar(&opts.configPath, "config", "", "path to the data folder")
	// pprof has no authentication, so it listens on loopback by default.
	flags.StringVar(&opts.pprofAddr, "pprof", "127.0.0.1:6060", "pprof server address, used when ENABLE_PPROF is set (\":6060\" listens on every interface; empty disables)")
	_ = flags.Parse(args)
	return opts
}
