//go:build linux

package main

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// This fixture uses the retained, pinned Docker executable inside the system
// container. It imports only this static workload into that private daemon.
func dockerParent() error {
	if os.Getuid() != 0 {
		return fmt.Errorf("Docker fixture parent requires guest root")
	}
	if err := os.Chown("/workspace", 1000, 1000); err != nil {
		return err
	}
	if err := os.MkdirAll("/run/private-docker", 0700); err != nil {
		return err
	}
	log, err := os.OpenFile("/workspace/docker.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	if err := os.WriteFile("/run/daemon.json", []byte("{}\n"), 0600); err != nil {
		return err
	}
	daemon := exec.Command("dockerd", "--config-file=/run/daemon.json", "--host=unix:///var/run/docker.sock",
		"--data-root=/var/lib/docker", "--exec-root=/run/private-docker", "--pidfile=/run/private-docker.pid", "--ip6tables=false")
	daemon.Dir = "/"
	daemon.Stdout, daemon.Stderr = log, log
	if err := daemon.Start(); err != nil {
		return err
	}
	go func() { _ = daemon.Wait() }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		command := exec.Command("docker", "info")
		if command.Run() == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("private Docker did not become ready; see /workspace/docker.log")
		}
		time.Sleep(100 * time.Millisecond)
	}
	version := exec.Command("docker", "version")
	version.Stdout, version.Stderr = log, log
	if err := version.Run(); err != nil {
		return err
	}
	image := "factorybox-shared-source-probe:owned"
	importer := exec.Command("docker", "import", "-", image)
	input, err := importer.StdinPipe()
	if err != nil {
		return err
	}
	importer.Stdout, importer.Stderr = log, log
	if err := importer.Start(); err != nil {
		return err
	}
	archive := tar.NewWriter(input)
	for _, name := range []string{"workspace", "workspace/project", "tmp", "dev"} {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0755, Typeflag: tar.TypeDir, Uid: 1000, Gid: 1000}); err != nil {
			return err
		}
	}
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		return err
	}
	stat, err := self.Stat()
	if err != nil {
		self.Close()
		return err
	}
	if err := archive.WriteHeader(&tar.Header{Name: "actor", Mode: 0555, Typeflag: tar.TypeReg, Size: stat.Size()}); err != nil {
		self.Close()
		return err
	}
	_, copyErr := io.Copy(archive, self)
	self.Close()
	if copyErr != nil {
		return copyErr
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := input.Close(); err != nil {
		return err
	}
	if err := importer.Wait(); err != nil {
		return err
	}
	arguments := []string{"run", "--name", "source-probe", "--user", "1000:1000", "--workdir", "/workspace/project",
		"--mount", "type=bind,source=/workspace,target=/workspace",
		"--mount", "type=bind,source=/home/coder/project,target=/workspace/project",
		"--env", "FACTORYBOX_SHARED_SOURCE_ROOT=/workspace/project"}
	for _, name := range []string{"FACTORYBOX_NETWORK_URL", "FACTORYBOX_SHARED_SOURCE_SUBDIRECTORY"} {
		if value := os.Getenv(name); value != "" {
			arguments = append(arguments, "--env", name+"="+value)
		}
	}
	arguments = append(arguments, image, "/actor")
	child := exec.Command("docker", arguments...)
	child.Stdout, child.Stderr = log, log
	return child.Run()
}
