//go:build linux

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"factorybox/internal/worker"
)

func runWorker(args []string) (bool, error) {
	if len(args) == 0 || (args[0] != "worker" && args[0] != "worker-status") {
		return false, nil
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	var config worker.Config
	flags.StringVar(&config.RunRoot, "run-root", "/run/factorybox", "private ephemeral worker readiness directory")
	if args[0] == "worker" {
		flags.StringVar(&config.Store, "store", "", "exclusively owned persistent store (default /var/lib/sysbox/store, or bounded-root/data/store)")
		flags.StringVar(&config.BoundedRoot, "bounded-root", "", "experimental private directory owning a fixed-size store image")
		flags.Int64Var(&config.BoundedBytes, "bounded-bytes", 0, "fixed image size in bytes; required with bounded-root")
		flags.StringVar(&config.StoreID, "store-id", "", "required immutable 32-character hexadecimal persistent store identity")
		flags.StringVar(&config.HealthListen, "health-listen", "", "optional numeric IP:port serving only live GET /readyz; no administrative API")
		flags.StringVar(&config.Components, "components", "/opt/factorybox/components", "private pinned components directory")
		flags.StringVar(&config.Manifest, "manifest", "/opt/factorybox/components.json", "exact pinned component manifest")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return true, err
	}
	if flags.NArg() != 0 {
		return true, fmt.Errorf("unexpected worker arguments")
	}
	if args[0] == "worker-status" {
		status, err := worker.Current(config.RunRoot)
		if err != nil {
			return true, err
		}
		return true, json.NewEncoder(os.Stdout).Encode(status)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return true, worker.Run(ctx, config)
}
