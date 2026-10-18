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
	flags.StringVar(&config.Criu, "criu", "", "exact CRIU executable for checkpoint")
	flags.StringVar(&config.CriuSHA256, "criu-sha256", "", "required CRIU SHA-256 for checkpoint")
	flags.StringVar(&config.ManagerRoot, "manager-root", "", "private Sysbox manager data root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 {
		return fmt.Errorf("usage: factorybox version|doctor, or --root PATH --runtime PATH --runtime-sha256 SHA start|state|checkpoint|delete")
	}
	command, remaining := flags.Arg(0), flags.Args()[1:]
	bundle := ""
	snapshot := ""
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
	} else if command == "checkpoint" {
		checkpoint := flag.NewFlagSet("checkpoint", flag.ContinueOnError)
		checkpoint.StringVar(&snapshot, "snapshot-id", "", "immutable 32-character hexadecimal checkpoint identity")
		if err := checkpoint.Parse(remaining); err != nil {
			return err
		}
		remaining = checkpoint.Args()
		if snapshot == "" {
			return fmt.Errorf("checkpoint requires --snapshot-id")
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
	if command == "checkpoint" {
		result, err := engine.Checkpoint(ctx, remaining[0], snapshot)
		if result.ID != "" {
			err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
		}
		return err
	}
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
