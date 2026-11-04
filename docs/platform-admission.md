# Platform admission

A worker runs on any Linux node that provides the kernel facilities the runtime
uses. It does not compare the release string with a list. `factorybox doctor`,
worker start and every lifecycle command apply the same gate and report every
failing capability with a remediation.

## Requirements

| Requirement | Why the runtime needs it |
| --- | --- |
| Linux on amd64 or arm64, 4096-byte pages | The page-map, CRIU image and snapshot code assume 4 KiB pages. |
| Kernel 5.15 or newer | The task timer reader loads a BPF task iterator that calls `bpf_task_pt_regs` (helper 175, Linux 5.15). Time namespaces (5.6), `openat2`, `pidfd_getfd` (5.6), `LOOP_CONFIGURE` (5.8) and `cgroup.freeze` (5.2) are older. A release without a parsable `major.minor` is not refused. The capabilities decide. |
| cgroup v2 with the memory controller | The worker cgroup needs a finite `memory.max`; each sandbox gets a child cgroup. |
| Every namespace type incl. time, user namespaces enabled | CRIU checkpoints PID, NET, IPC, UTS, USER, mount, cgroup and time namespaces. |
| `/proc/sys/kernel/ns_last_pid` (`CONFIG_CHECKPOINT_RESTORE`) | CRIU restores process IDs through it. |
| `/sys/kernel/btf/vmlinux` containing `bpf_iter__task` | The timer reader reads kernel BTF and attaches a task iterator (`CONFIG_BPF_SYSCALL`, `CONFIG_DEBUG_INFO_BTF`). |
| Seccomp filters | Sysbox emulates system calls through seccomp user notification. |
| `/dev/loop-control`, ext4 | The bounded store attaches an ext4 image through a loop device. |
| `/dev/fuse` | sysbox-fs mounts its emulation tree through FUSE. |
| veth, conntrack, NAT, writable `/proc/sys/net/ipv4/ip_forward` in the worker network namespace | Outbound sandbox networking uses an owned veth pair and three iptables rules. |

Not required: userfaultfd (no lazy restore), soft-dirty tracking (no incremental
dumps), shiftfs (the worker starts Sysbox with `--disable-shiftfs`), KVM or a
custom kernel.

## Probing

Probes read `/proc`, `/sys` and `/dev`. When the kernel config is visible
(`/proc/config.gz`, `/boot/config-<release>`, `/lib/modules/<release>/build/.config`),
an option that the config explicitly disables fails its capability. Worker
containers usually do not see a config, so those checks are not evidence there.
Evidence that this process cannot see, such as an unloaded module or an
unregistered filesystem that mount may autoload, is reported as `unknown` and
does not refuse. The first real use reports the exact kernel error.

Each capability has the status `ok`, `failed` or `unknown`. Only `failed` refuses.

## Qualification

`doctor` and `worker-status` report `qualification: verified|unverified`.
`verified` means the project ran its native suites on that exact architecture
and kernel release. The list lives in `internal/platform/execution.go`.
Any other kernel is admitted and reported `unverified`. Operators decide
whether to run on it. The label never refuses.

## Identity

Worker status keeps the exact kernel release and the kernel boot ID. A consumer
pins a retained worker to its boot ID, so a node reboot is never mistaken for
the same worker. The capability gate does not change that identity.

## Running the gate

```sh
factorybox doctor | jq '.qualification, .error, (.capabilities[] | select(.status != "ok"))'
```

The exit status is nonzero only when a capability failed or the tuple is
unsupported. Check an exact node with a privileged pod before installing a worker
there. The result still does not replace the lifecycle suites on that node.
