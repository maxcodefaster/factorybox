//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"factorybox/internal/lifecycle"
)

func runLifecycle(args []string) error {
	if handled, err := lifecycle.CaptureGuardCommand(args); handled {
		return err
	}
	if handled, err := lifecycle.CaptureWorkerCommand(args); handled {
		return err
	}
	flags := flag.NewFlagSet("factorybox", flag.ContinueOnError)
	var config lifecycle.Config
	flags.StringVar(&config.Root, "root", "", "private worker state directory")
	flags.StringVar(&config.Runtime, "runtime", "", "exact Sysbox executable")
	flags.StringVar(&config.RuntimeSHA256, "runtime-sha256", "", "required executable SHA-256")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 {
		return fmt.Errorf("usage: factorybox version|doctor, or --root PATH --runtime PATH --runtime-sha256 SHA start|state|delete")
	}
	command, remaining := flags.Arg(0), flags.Args()[1:]
	bundle := ""
	if command == "start" {
		start := flag.NewFlagSet("start", flag.ContinueOnError)
		start.StringVar(&bundle, "bundle", "", "administrator-prepared OCI bundle")
		if err := start.Parse(remaining); err != nil {
			return err
		}
		remaining = start.Args()
		if bundle == "" {
			return fmt.Errorf("start requires --bundle")
		}
	} else if command != "state" && command != "delete" {
		return fmt.Errorf("unknown lifecycle command %q", command)
	}
	if len(remaining) != 1 {
		return fmt.Errorf("%s requires one sandbox ID", command)
	}
	engine, err := lifecycle.Open(config)
	if err != nil {
		return err
	}
	defer engine.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	var view lifecycle.View
	switch command {
	case "start":
		view, err = engine.Start(ctx, remaining[0], bundle)
	case "state":
		view, err = engine.Inspect(ctx, remaining[0])
	case "delete":
		view, err = engine.Delete(ctx, remaining[0])
	}
	if view.Record.ID != "" {
		err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(view))
	}
	return err
}
