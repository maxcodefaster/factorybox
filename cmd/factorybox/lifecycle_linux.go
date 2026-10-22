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
	"factorybox/internal/worker"
)

func runLifecycle(args []string) error {
	if handled, err := runWorker(args); handled {
		return err
	}
	if handled, err := lifecycle.CaptureGuardCommand(args); handled {
		return err
	}
	if handled, err := lifecycle.CaptureWorkerCommand(args); handled {
		return err
	}
	flags := flag.NewFlagSet("factorybox", flag.ContinueOnError)
	var config lifecycle.Config
	var workerRunRoot string
	flags.StringVar(&workerRunRoot, "worker-run-root", "", "resolve controller and components from the validated live worker")
	flags.StringVar(&config.Root, "root", "", "private worker state directory")
	flags.StringVar(&config.Runtime, "runtime", "", "exact Sysbox executable")
	flags.StringVar(&config.RuntimeSHA256, "runtime-sha256", "", "required executable SHA-256")
	flags.StringVar(&config.Criu, "criu", "", "exact CRIU executable for checkpoint")
	flags.StringVar(&config.CriuSHA256, "criu-sha256", "", "required CRIU SHA-256 for checkpoint")
	flags.StringVar(&config.ManagerRoot, "manager-root", "", "private Sysbox manager data root")
	flags.Uint64Var(&config.MinFreeBytes, "min-free-bytes", 0, "explicit controller filesystem admission floor")
	flags.Uint64Var(&config.WriteReserve, "write-reserve-bytes", 0, "additional preflight headroom for new backing writes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if workerRunRoot != "" {
		if config != (lifecycle.Config{}) {
			return fmt.Errorf("--worker-run-root cannot be combined with explicit controller/component options")
		}
		status, err := worker.Current(workerRunRoot)
		if err != nil {
			return err
		}
		config = status.Lifecycle
	}
	if flags.NArg() < 1 {
		return fmt.Errorf("usage: factorybox version|doctor, or --root PATH --runtime PATH --runtime-sha256 SHA prepare-base|start|state|checkpoint|fork|pause|resume|delete")
	}
	command, remaining := flags.Arg(0), flags.Args()[1:]
	bundle := ""
	snapshot := ""
	source := ""
	var recovery lifecycle.RecoveryRequest
	var checkpointRequest lifecycle.CheckpointRequest
	var selectionID string
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
	} else if command == "retire-checkpoint" || command == "select-checkpoint" || command == "release-checkpoint" {
		operation := flag.NewFlagSet(command, flag.ContinueOnError)
		operation.StringVar(&checkpointRequest.SnapshotID, "snapshot-id", "", "exact published checkpoint ID")
		operation.StringVar(&checkpointRequest.SourceOwner, "source-owner", "", "exact saved source owner")
		operation.StringVar(&checkpointRequest.CheckpointSHA256, "checkpoint-sha256", "", "exact complete published checkpoint SHA-256")
		if command != "retire-checkpoint" {
			operation.StringVar(&selectionID, "selection-id", "", "immutable recovery selection ID")
		}
		if err := operation.Parse(remaining); err != nil {
			return err
		}
		remaining = operation.Args()
		if checkpointRequest.SnapshotID == "" || checkpointRequest.SourceOwner == "" || checkpointRequest.CheckpointSHA256 == "" || command != "retire-checkpoint" && selectionID == "" {
			return fmt.Errorf("%s requires exact checkpoint, owner, hash and any selection identity", command)
		}
	} else if command == "checkpoint" || command == "pause" || command == "resume" {
		checkpoint := flag.NewFlagSet(command, flag.ContinueOnError)
		checkpoint.StringVar(&snapshot, "snapshot-id", "", "immutable 32-character hexadecimal checkpoint identity")
		if err := checkpoint.Parse(remaining); err != nil {
			return err
		}
		remaining = checkpoint.Args()
		if snapshot == "" {
			return fmt.Errorf("%s requires --snapshot-id", command)
		}
	} else if command == "prepare-base" {
		base := flag.NewFlagSet(command, flag.ContinueOnError)
		base.StringVar(&source, "source", "", "complete administrator-prepared base directory")
		if err := base.Parse(remaining); err != nil {
			return err
		}
		remaining = base.Args()
		if source == "" {
			return fmt.Errorf("prepare-base requires --source")
		}
	} else if command == "fork" || command == "recover-paused" {
		fork := flag.NewFlagSet("fork", flag.ContinueOnError)
		fork.StringVar(&source, "source", "", "sandbox that owns the published checkpoint")
		fork.StringVar(&snapshot, "snapshot-id", "", "immutable checkpoint identity")
		if command == "recover-paused" {
			fork.StringVar(&recovery.SourceRoot, "source-root", "", "fenced old worker controller directory on the same persistent store")
			fork.StringVar(&recovery.SourceOwner, "source-owner", "", "exact saved sandbox owner")
			fork.StringVar(&recovery.CheckpointSHA256, "checkpoint-sha256", "", "exact complete saved checkpoint SHA-256")
		}
		if err := fork.Parse(remaining); err != nil {
			return err
		}
		remaining = fork.Args()
		if source == "" || snapshot == "" {
			return fmt.Errorf("fork requires --source and --snapshot-id")
		}
	} else if command != "state" && command != "delete" && command != "capacity" {
		return fmt.Errorf("unknown lifecycle command %q", command)
	}
	if command == "capacity" && len(remaining) != 0 {
		return fmt.Errorf("capacity requires no sandbox ID")
	}
	if command != "capacity" && len(remaining) != 1 {
		return fmt.Errorf("%s requires one sandbox ID", command)
	}
	engine, err := lifecycle.Open(config)
	if err != nil {
		return err
	}
	defer engine.Close()
	if command == "capacity" {
		report, err := engine.Capacity()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if command == "retire-checkpoint" || command == "select-checkpoint" || command == "release-checkpoint" {
		checkpointRequest.SourceID = remaining[0]
		if command == "retire-checkpoint" {
			result, err := engine.RetireCheckpoint(ctx, checkpointRequest)
			if result.Version != 0 {
				err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
			}
			return err
		}
		var result lifecycle.CheckpointSelection
		if command == "select-checkpoint" {
			result, err = engine.SelectCheckpoint(ctx, checkpointRequest, selectionID)
		} else {
			result, err = engine.ReleaseCheckpointSelection(ctx, checkpointRequest, selectionID)
		}
		if result.ID != "" {
			err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
		}
		return err
	}
	if command == "prepare-base" {
		result, err := engine.PrepareBase(ctx, remaining[0], source)
		if result.ID != "" {
			err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
		}
		return err
	}
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
	case "fork":
		view, err = engine.Fork(ctx, source, snapshot, remaining[0])
	case "recover-paused":
		recovery.SourceID, recovery.SnapshotID = source, snapshot
		view, err = engine.RecoverPaused(ctx, recovery, remaining[0])
	case "pause":
		view, err = engine.Pause(ctx, remaining[0], snapshot)
	case "resume":
		view, err = engine.Resume(ctx, remaining[0], snapshot)
	}
	if view.Record.ID != "" {
		err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(view))
	}
	return err
}
