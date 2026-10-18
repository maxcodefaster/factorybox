package state

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Only the owned prototype may fork a live source. Its original pidfd is
// checked by preRegister; the kernel must report the entire cgroup frozen.
func experimentFrozenForkSource(original *container, target string) error {
	branch := regexp.MustCompile(`^branch-[0-9a-f]{32}$`)
	sourceCgroup := ""
	switch original.id {
	case "source-sysbox":
		sourceCgroup = "/criu-probe-sysbox"
	case "copy-sysbox":
		sourceCgroup = "/criu-probe-sysbox-copy"
	default:
		if branch.MatchString(original.id) {
			sourceCgroup = "/criu-probe-" + original.id
		}
	}
	validTarget := branch.MatchString(target) || (original.id == "source-sysbox" && target == "copy-sysbox")
	if os.Getenv("CRIU_SYSBOX_RETAIN_SOURCE") != "1" || sourceCgroup == "" || !validTarget ||
		target == original.id || original.initPid == 0 {
		return fmt.Errorf("live source is outside the frozen owned fork")
	}
	membership, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", original.initPid))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(membership)) != "0::"+sourceCgroup+"/init.scope" {
		return fmt.Errorf("original PID does not belong to the owned source cgroup")
	}
	freeze, err := os.ReadFile("/sys/fs/cgroup" + sourceCgroup + "/cgroup.freeze")
	if err != nil || strings.TrimSpace(string(freeze)) != "1" {
		return fmt.Errorf("source freeze was not requested: %v", err)
	}
	events, err := os.ReadFile("/sys/fs/cgroup" + sourceCgroup + "/cgroup.events")
	if err != nil {
		return err
	}
	frozen, populated := false, false
	for _, line := range strings.Split(strings.TrimSpace(string(events)), "\n") {
		switch line {
		case "frozen 1":
			frozen = true
		case "populated 1":
			populated = true
		}
	}
	if !frozen || !populated {
		return fmt.Errorf("kernel has not frozen the populated source cgroup")
	}
	return nil
}
