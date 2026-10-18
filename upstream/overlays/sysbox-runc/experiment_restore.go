//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	criu "github.com/checkpoint-restore/go-criu/v4/rpc"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/urfave/cli"
)

// This first probe keeps the original manager/fs registrations and all
// Sysbox metadata loaded from state.json. It does not create fork identities.
var experimentRestoreCommand = cli.Command{
	Name:  "experiment-restore-existing",
	Usage: "restore an owned experiment with retained Sysbox metadata",
	Flags: []cli.Flag{cli.StringFlag{Name: "image-path"}},
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 1, exactArgs); err != nil {
			return err
		}
		images := context.String("image-path")
		for _, path := range []string{context.GlobalString("root"), images} {
			if !strings.HasPrefix(path, "/experiment/") || filepath.Clean(path) != path {
				return fmt.Errorf("path is outside the owned experiment: %q", path)
			}
		}
		container, err := getContainer(context)
		if err != nil {
			return err
		}
		status, err := container.Status()
		if err != nil || status != libcontainer.Stopped {
			return fmt.Errorf("owned source must be stopped before restore: %v %v", status, err)
		}
		if err := libcontainer.ExperimentRestoreRoot(container.Config(), images); err != nil {
			return err
		}
		options := &libcontainer.CriuOpts{
			SysboxSeccompRestore: true,
			ImagesDirectory:      images, WorkDirectory: images,
			ShellJob: true, FileLocks: true, EmptyNs: 0,
			TcpEstablished:    os.Getenv("CRIU_SYSBOX_TOOLS") == "1",
			ManageCgroupsMode: criu.CriuCgMode_FULL,
		}
		process := &libcontainer.Process{Init: true, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
		if err := container.Restore(process, options); err != nil {
			return err
		}
		state, err := container.State()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(state)
	},
}
