//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	if handled, err := lifecycle.TimerWorkerCommand(args); handled {
		return err
	}
	if handled, err := lifecycle.RestoreWorkerCommand(args); handled {
		return err
	}
	flags := flag.NewFlagSet("factorybox", flag.ContinueOnError)
	var config lifecycle.Config
	var workerRunRoot string
	var expectedStore, expectedEpoch string
	flags.StringVar(&workerRunRoot, "worker-run-root", "", "resolve controller and components from the validated live worker")
	flags.StringVar(&expectedStore, "worker-store-id", "", "require this exact live worker store; paired with --worker-epoch")
	flags.StringVar(&expectedEpoch, "worker-epoch", "", "require this exact live worker epoch; paired with --worker-store-id")
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
	if expectedStore != "" || expectedEpoch != "" {
		if workerRunRoot == "" || !workerIdentityToken(expectedStore) || !workerIdentityToken(expectedEpoch) {
			return fmt.Errorf("worker identity requires --worker-run-root and paired 32-character hexadecimal --worker-store-id/--worker-epoch")
		}
	}
	if workerRunRoot != "" {
		if config != (lifecycle.Config{}) {
			return fmt.Errorf("--worker-run-root cannot be combined with explicit controller/component options")
		}
		status, err := worker.Current(workerRunRoot)
		if err != nil {
			return err
		}
		if expectedStore != "" && (status.StoreID != expectedStore || status.Epoch != expectedEpoch) {
			return fmt.Errorf("live worker store or epoch differs from the requested worker identity")
		}
		config = status.Lifecycle
	}
	if flags.NArg() < 1 {
		return fmt.Errorf("usage: factorybox version|doctor, or --root PATH --runtime PATH --runtime-sha256 SHA prepare-base|start|state|checkpoint|fork|pause|resume|delete")
	}
	command, remaining := flags.Arg(0), flags.Args()[1:]
	checkpointRetirement := command == "retire-checkpoint" || command == "retire-resume"
	bundle := ""
	snapshot := ""
	source := ""
	var recovery lifecycle.RecoveryRequest
	var checkpointRequest lifecycle.CheckpointRequest
	var backingRequest lifecycle.SandboxBackingRequest
	var selectionID string
	var retainedRoot string
	var epochRequest lifecycle.EpochRetirementRequest
	var epochPreparation lifecycle.EpochPreparationInspection
	var importInspection lifecycle.RecoveryImportInspection
	var importTarget string
	inspectImport := command == "inspect-recovery-import"
	cancelImport := command == "cancel-recovery-import"
	epochOperation := command == "retire-epoch" || command == "begin-epoch-retirement" || command == "cancel-epoch-preparation"
	epochInspection := command == "inspect-epoch-preparation"
	if inspectImport {
		operation := flag.NewFlagSet(command, flag.ContinueOnError)
		operation.StringVar(&retainedRoot, "source-root", config.Root, "exact current or fenced earlier controller")
		operation.StringVar(&importTarget, "target", "", "exact unpublished recovery target")
		if err := operation.Parse(remaining); err != nil {
			return err
		}
		remaining = operation.Args()
		if workerRunRoot == "" || importTarget == "" || len(remaining) != 0 {
			return fmt.Errorf("inspect-recovery-import requires --worker-run-root and --target without sandbox arguments")
		}
	} else if epochInspection {
		operation := flag.NewFlagSet(command, flag.ContinueOnError)
		operation.StringVar(&retainedRoot, "source-root", "", "exact old controller with an unaccepted preparation")
		if err := operation.Parse(remaining); err != nil {
			return err
		}
		remaining = operation.Args()
		if workerRunRoot == "" || retainedRoot == "" || len(remaining) != 0 {
			return fmt.Errorf("inspect-epoch-preparation requires --worker-run-root and --source-root without sandbox arguments")
		}
	} else if epochOperation || cancelImport {
		operation := flag.NewFlagSet(command, flag.ContinueOnError)
		requestFile := operation.String("request-file", "", "exact operation request or saved inspection as JSON")
		if err := operation.Parse(remaining); err != nil {
			return err
		}
		remaining = operation.Args()
		if workerRunRoot == "" || *requestFile == "" || len(remaining) != 0 {
			return fmt.Errorf("%s requires --worker-run-root and --request-file, without sandbox arguments", command)
		}
		file, err := os.OpenFile(*requestFile, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
			file.Close()
			return fmt.Errorf("%s request is not a bounded regular JSON file", command)
		}
		limited := &io.LimitedReader{R: file, N: (4 << 20) + 1}
		decoder := json.NewDecoder(limited)
		decoder.DisallowUnknownFields()
		var target any = &epochRequest
		if command == "cancel-epoch-preparation" {
			target = &epochPreparation
		}
		if cancelImport {
			target = &importInspection
		}
		decodeErr := decoder.Decode(target)
		if decodeErr == nil {
			if err := decoder.Decode(new(any)); err != io.EOF || limited.N <= 0 {
				decodeErr = fmt.Errorf("%s request has trailing or excessive data", command)
			}
		}
		if err := errors.Join(decodeErr, file.Close()); err != nil {
			return err
		}
	} else if command == "retire-sandbox" || command == "retire-manager" {
		operation := flag.NewFlagSet(command, flag.ContinueOnError)
		operation.StringVar(&backingRequest.SandboxOwner, "sandbox-owner", "", "exact deleted Sandbox owner")
		operation.StringVar(&backingRequest.InputSHA256, "input-sha256", "", "exact original OCI input SHA-256")
		if err := operation.Parse(remaining); err != nil {
			return err
		}
		remaining = operation.Args()
		if backingRequest.SandboxOwner == "" || backingRequest.InputSHA256 == "" {
			return fmt.Errorf("%s requires exact Sandbox owner and input hash", command)
		}
	} else if command == "start" {
		start := flag.NewFlagSet("start", flag.ContinueOnError)
		start.StringVar(&bundle, "bundle", "", "administrator-prepared OCI bundle")
		if err := start.Parse(remaining); err != nil {
			return err
		}
		remaining = start.Args()
		if bundle == "" {
			return fmt.Errorf("start requires --bundle")
		}
	} else if checkpointRetirement || command == "select-checkpoint" || command == "release-checkpoint" || command == "release-recovery-selection" || command == "inspect-recovery-checkpoint" {
		operation := flag.NewFlagSet(command, flag.ContinueOnError)
		operation.StringVar(&checkpointRequest.SnapshotID, "snapshot-id", "", "exact published checkpoint ID")
		operation.StringVar(&checkpointRequest.SourceOwner, "source-owner", "", "exact saved source owner")
		operation.StringVar(&checkpointRequest.CheckpointSHA256, "checkpoint-sha256", "", "exact complete published checkpoint SHA-256")
		if !checkpointRetirement && command != "inspect-recovery-checkpoint" {
			operation.StringVar(&selectionID, "selection-id", "", "immutable recovery selection ID")
		}
		if command == "release-recovery-selection" || command == "inspect-recovery-checkpoint" {
			operation.StringVar(&retainedRoot, "source-root", "", "exact earlier controller in the current worker's bounded store")
		}
		if err := operation.Parse(remaining); err != nil {
			return err
		}
		remaining = operation.Args()
		if checkpointRequest.SnapshotID == "" || checkpointRequest.SourceOwner == "" || checkpointRequest.CheckpointSHA256 == "" || !checkpointRetirement && command != "inspect-recovery-checkpoint" && selectionID == "" {
			return fmt.Errorf("%s requires exact checkpoint, owner, hash and any selection identity", command)
		}
		if (command == "release-recovery-selection" || command == "inspect-recovery-checkpoint") && (workerRunRoot == "" || retainedRoot == "") {
			return fmt.Errorf("%s requires --worker-run-root and --source-root", command)
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
	if command != "capacity" && !epochOperation && !epochInspection && !inspectImport && !cancelImport && len(remaining) != 1 {
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
	if command == "retire-sandbox" || command == "retire-manager" {
		backingRequest.SandboxID = remaining[0]
		var result lifecycle.SandboxBackingRetirement
		var err error
		if command == "retire-manager" {
			result, err = engine.RetireDeletedManagerBacking(ctx, backingRequest)
		} else {
			result, err = engine.RetireDeletedBacking(ctx, backingRequest)
		}
		if result.Version != 0 {
			err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
		}
		return err
	}
	if inspectImport {
		result, err := engine.InspectRecoveryImport(ctx, retainedRoot, importTarget)
		if result.Version != 0 {
			err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
		}
		return err
	}
	if cancelImport {
		result, err := engine.CancelRecoveryImport(ctx, importInspection)
		if result.Version != 0 {
			err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
		}
		return err
	}
	if epochInspection {
		result, err := engine.InspectEpochPreparation(ctx, retainedRoot)
		if result.Version != 0 {
			err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
		}
		return err
	}
	if command == "cancel-epoch-preparation" {
		result, err := engine.CancelEpochPreparation(ctx, epochPreparation)
		if result.Version != 0 {
			err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
		}
		return err
	}
	if command == "retire-epoch" || command == "begin-epoch-retirement" {
		var result lifecycle.EpochRetirement
		if command == "begin-epoch-retirement" {
			result, err = engine.BeginEpochRetirement(ctx, epochRequest)
		} else {
			result, err = engine.RetireEpoch(ctx, epochRequest)
		}
		if result.Version != 0 {
			err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
		}
		return err
	}
	if checkpointRetirement || command == "select-checkpoint" || command == "release-checkpoint" || command == "release-recovery-selection" || command == "inspect-recovery-checkpoint" {
		checkpointRequest.SourceID = remaining[0]
		if command == "inspect-recovery-checkpoint" {
			result, err := engine.InspectRetainedCheckpoint(ctx, lifecycle.RetainedCheckpointRequest{
				SourceRoot: retainedRoot, CheckpointRequest: checkpointRequest})
			if result.Version != 0 {
				err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
			}
			return err
		}
		if checkpointRetirement {
			var result lifecycle.CheckpointRetirement
			var err error
			if command == "retire-resume" {
				result, err = engine.RetireResumeImages(ctx, checkpointRequest)
			} else {
				result, err = engine.RetireCheckpoint(ctx, checkpointRequest)
			}
			if result.Version != 0 {
				err = errors.Join(err, json.NewEncoder(os.Stdout).Encode(result))
			}
			return err
		}
		var result lifecycle.CheckpointSelection
		if command == "select-checkpoint" {
			result, err = engine.SelectCheckpoint(ctx, checkpointRequest, selectionID)
		} else if command == "release-recovery-selection" {
			result, err = engine.ReleaseRetainedCheckpointSelection(ctx, lifecycle.RetainedCheckpointRequest{
				SourceRoot: retainedRoot, CheckpointRequest: checkpointRequest}, selectionID)
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

func workerIdentityToken(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
