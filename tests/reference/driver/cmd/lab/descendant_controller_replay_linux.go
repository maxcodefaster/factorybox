package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func controllerCaptureReplayProof(capture *controllerLossCapture, guard *descendantGuard,
	network *captureNetwork, completed *controllerCaptureResult) (_ map[string]any, resultErr error) {
	if os.Getenv("RUNTIME_FORK_CONTROLLER_REPLAY_PROOF") != "1" {
		return nil, fmt.Errorf("completed capture replay proof was not explicitly requested")
	}
	images, group := capture.journal.Images, capture.group.Name()
	for _, path := range []string{images, group} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return nil, fmt.Errorf("replacement fixture requires its retired owned path to be absent: %s %v", path, err)
		}
	}
	if err := os.Mkdir(images, 0700); err != nil {
		return nil, err
	}
	imageIdentity, err := os.Lstat(images)
	if err != nil {
		return nil, err
	}
	sentinel := filepath.Join(images, "owned-replay-successor")
	var imageCreated, groupCreated bool
	defer func() {
		if imageCreated {
			current, err := os.Lstat(images)
			if err == nil && os.SameFile(current, imageIdentity) {
				resultErr = errors.Join(resultErr, os.Remove(sentinel), os.Remove(images))
			} else {
				resultErr = errors.Join(resultErr, fmt.Errorf("replacement image fixture lost exact ownership: %v", err))
			}
		}
	}()
	file, err := os.OpenFile(sentinel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	imageCreated = true
	_, writeErr := file.WriteString("only-this-replay-fixture-owns-the-successor\n")
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return nil, err
	}
	sentinelSHA, err := captureFileSHA(sentinel)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(group, 0700); err != nil {
		return nil, err
	}
	groupIdentity, err := os.Lstat(group)
	if err != nil {
		return nil, err
	}
	groupCreated = true
	defer func() {
		if groupCreated {
			current, err := os.Lstat(group)
			if err == nil && os.SameFile(current, groupIdentity) {
				resultErr = errors.Join(resultErr, os.Remove(group))
			} else {
				resultErr = errors.Join(resultErr, fmt.Errorf("replacement cgroup fixture lost exact ownership: %v", err))
			}
		}
	}()
	oldImages, err := capture.images.Stat()
	if err != nil {
		return nil, err
	}
	oldGroup, err := capture.group.Stat()
	if err != nil || os.SameFile(oldImages, imageIdentity) || os.SameFile(oldGroup, groupIdentity) {
		return nil, fmt.Errorf("replacement fixtures did not create new kernel objects: %v", err)
	}
	before, err := descendantGuardEvents(guard.anchor.group)
	if err != nil {
		return nil, err
	}
	outcomeSHA, err := captureFileSHA(guard.config.Evidence + "/worker-capture-reconciled.json")
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		replayed, err := capture.abortAndRecover(guard, network)
		if err != nil || asJSON(replayed) != asJSON(completed) {
			return nil, fmt.Errorf("completed capture replay did not return its exact original result: %v", err)
		}
	}
	var rejected []string
	for _, name := range []string{"source_birth", "guard_token", "image_generation", "capture_command_birth"} {
		foreign := *capture
		switch name {
		case "source_birth":
			foreign.journal.Init.Start += "0"
		case "guard_token":
			foreign.journal.Token = "00000000000000000000000000000000"
		case "image_generation":
			foreign.journal.Images += "-foreign"
		case "capture_command_birth":
			foreign.journal.Command.Start += "0"
		}
		if _, err := foreign.abortAndRecover(guard, network); err == nil {
			return nil, fmt.Errorf("completed replay accepted a foreign operation: %s", name)
		}
		rejected = append(rejected, name)
	}
	after, err := descendantGuardEvents(guard.anchor.group)
	if err != nil || asJSON(before) != asJSON(after) {
		return nil, fmt.Errorf("completed replay changed the original source freeze state: %v", err)
	}
	currentImages, imageErr := os.Lstat(images)
	currentGroup, groupErr := os.Lstat(group)
	currentSentinel, sentinelErr := captureFileSHA(sentinel)
	currentOutcome, outcomeErr := captureFileSHA(guard.config.Evidence + "/worker-capture-reconciled.json")
	if imageErr != nil || groupErr != nil || sentinelErr != nil || outcomeErr != nil ||
		!os.SameFile(currentImages, imageIdentity) || !os.SameFile(currentGroup, groupIdentity) ||
		currentSentinel != sentinelSHA || currentOutcome != outcomeSHA {
		return nil, fmt.Errorf("completed replay changed a replacement object or immutable result: %v %v %v %v",
			imageErr, groupErr, sentinelErr, outcomeErr)
	}
	result := map[string]any{
		"passed": true, "successful_completed_replays": 2, "rejected_foreign_operations": rejected,
		"original_source_events_before": before, "original_source_events_after": after,
		"replacement_image_directory_preserved": true, "replacement_holder_cgroup_preserved": true,
		"new_kernel_objects_differ_from_retired_anchors": true,
		"replacement_sentinel_sha256_before":             sentinelSHA, "replacement_sentinel_sha256_after": currentSentinel,
		"completed_result_sha256_before": outcomeSHA, "completed_result_sha256_after": currentOutcome,
	}
	if err := descendantGuardWrite(guard.anchor.evidence, "worker-capture-replay-proof.json", result); err != nil {
		return nil, err
	}
	return result, nil
}
