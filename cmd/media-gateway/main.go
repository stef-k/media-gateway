// Command media-gateway runs the loopback-only service shell.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/stef-k/media-gateway/internal/config"
)

// main owns process signals and translates runtime failure into an exit status.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run keeps CLI diagnostics sanitized: flag errors can otherwise echo input values.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	flags := flag.NewFlagSet("media-gateway", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	filename := flags.String("config", "", "explicit TOML configuration path")
	version := flags.Bool("version", false, "print build metadata and exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, "Usage: media-gateway -config <file> | -version")
			return 0
		}
		logger.Error("invalid command line", "hint", "use -config <file> or -version")
		return 2
	}
	if flags.NArg() != 0 || (*version && *filename != "") || (!*version && *filename == "") {
		logger.Error("invalid command line", "hint", "use -config <file> or -version")
		return 2
	}
	if *version {
		fmt.Fprintln(stdout, buildMetadata())
		return 0
	}
	logger.Info("starting", "build", buildMetadata())
	cfg, _, err := config.Load(*filename)
	if err != nil {
		// Load guarantees field/operation-only errors without private input values.
		logger.Error("configuration rejected", "error", err)
		return 1
	}
	logger.Info("configuration loaded", "listen", cfg.Server.Listen)
	if err := serve(ctx, cfg.Server.Listen, logger); err != nil {
		logger.Error("service failed", "error", err)
		return 1
	}
	return 0
}
