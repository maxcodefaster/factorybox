//go:build ignore
// SPDX-License-Identifier: GPL-2.0-only
#include <linux/bpf.h>
#include "bpf_helpers.h"

struct dentry;
struct super_block;
struct vfsmount { struct dentry *mnt_root; struct super_block *mnt_sb; int mnt_flags; } __attribute__((preserve_access_index));
struct path { struct vfsmount *mnt; struct dentry *dentry; } __attribute__((preserve_access_index));
struct file { struct path f_path; } __attribute__((preserve_access_index));
struct mount { struct vfsmount mnt; int mnt_id; } __attribute__((preserve_access_index));
struct ns_common { unsigned int inum; } __attribute__((preserve_access_index));
struct pid_namespace { struct ns_common ns; } __attribute__((preserve_access_index));
struct upid { int nr; struct pid_namespace *ns; } __attribute__((preserve_access_index));
struct pid { unsigned int level; struct upid numbers[1]; } __attribute__((preserve_access_index));
struct task_struct { struct pid *thread_pid; } __attribute__((preserve_access_index));

struct event {
    __u64 ns, file, mount, pid_tgid, generation;
    __u32 local_pid, mount_id;
    __s32 operation, result;
    char comm[16];
};

const volatile __u64 pidns_dev = 0;
const volatile __u64 pidns_ino = 0;

struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 65536); __type(key, __u64); __type(value, struct event); } files SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 65536); __type(key, __u64); __type(value, struct event); } closing SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 1024); __type(key, __u64); __type(value, struct event); } unmounts SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_RINGBUF); __uint(max_entries, 4 << 20); } events SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_ARRAY); __uint(max_entries, 3); __type(key, __u32); __type(value, __u64); } errors SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_ARRAY); __uint(max_entries, 1); __type(key, __u32); __type(value, __u32); } enabled SEC(".maps");

static __always_inline void error(__u32 key) {
    __u64 *n = bpf_map_lookup_elem(&errors,&key);
    if (n) __sync_fetch_and_add(n,1);
}

static __always_inline __u32 owned_pid(void) {
    __u32 key = 0;
    __u32 *active = bpf_map_lookup_elem(&enabled,&key);
    if (!active || !*active || !pidns_dev || !pidns_ino) return 0;
    struct task_struct *task = (void *)bpf_get_current_task();
    struct pid *pid = 0;
    unsigned int level = 0;
    if (bpf_probe_read_kernel(&pid,sizeof(pid),__builtin_preserve_access_index(&task->thread_pid)) || !pid ||
        bpf_probe_read_kernel(&level,sizeof(level),__builtin_preserve_access_index(&pid->level)) || level >= 32) {
        error(2); return 0;
    }
    void *numbers = __builtin_preserve_access_index(&pid->numbers[0]);
    // The namespace helper matches only the active PID namespace. Find the
    // exact owned ancestor instead, including its descendant processes.
    // The loader validates the upid size and offsets against kernel BTF.
    #pragma unroll
    for (int index = 0; index < 32; index++) {
        if (index > level) break;
        struct upid at = {};
        unsigned int inode = 0;
        if (bpf_probe_read_kernel(&at,sizeof(at),numbers + index * sizeof(struct upid)) ||
            bpf_probe_read_kernel(&inode,sizeof(inode),__builtin_preserve_access_index(&at.ns->ns.inum))) {
            error(2); return 0;
        }
        if (inode == pidns_ino) return at.nr;
    }
    return 0;
}

static __always_inline __u32 mount_id(struct vfsmount *vfs) {
    struct mount *m = (void *)((char *)vfs - __builtin_preserve_field_info(((struct mount *)0)->mnt,0));
    __u32 id = 0;
    if (bpf_probe_read_kernel(&id,sizeof(id),__builtin_preserve_access_index(&m->mnt_id))) error(2);
    return id;
}

static __always_inline void emit(struct event *value, int operation, int result) {
    struct event *out = bpf_ringbuf_reserve(&events,sizeof(*out),0);
    if (!out) { error(0); return; }
    *out = *value;
    out->ns = bpf_ktime_get_ns();
    out->pid_tgid = bpf_get_current_pid_tgid();
    out->operation = operation;
    out->result = result;
    bpf_get_current_comm(out->comm,sizeof(out->comm));
    bpf_ringbuf_submit(out,0);
}

SEC("kretprobe/path_openat")
int opened(__u64 *ctx) {
    struct file *file = (void *)ctx[10]; /* x86-64 pt_regs.ax, verified against BTF */
    /* O_PATH bypasses security_file_open. Observe completed path opens and
     * exclude ERR_PTR returns rather than treating a failed open as a hold. */
    if (!file || (__u64)file >= (__u64)-4095) return 0;
    __u32 pid = owned_pid();
    if (!pid) return 0;
    struct vfsmount *mnt = 0;
    if (bpf_probe_read_kernel(&mnt,sizeof(mnt),__builtin_preserve_access_index(&file->f_path.mnt))) { error(2); return 0; }
    struct event value = {.ns=bpf_ktime_get_ns(),.file=(__u64)file,.mount=(__u64)mnt,.local_pid=pid,.mount_id=mount_id(mnt)};
    value.generation = value.ns;
    __u64 key = (__u64)file;
    if (bpf_map_update_elem(&files,&key,&value,BPF_ANY)) error(1);
    emit(&value,1,0);
    return 0;
}

SEC("kprobe/__fput")
int closing_file(__u64 *ctx) {
    __u64 key = ctx[14];
    struct event *value = bpf_map_lookup_elem(&files,&key);
    if (!value) return 0;
    struct event copy = *value;
    key = bpf_get_current_pid_tgid();
    if (bpf_map_lookup_elem(&closing,&key)) error(1); /* Never silently accept recursion. */
    if (bpf_map_update_elem(&closing,&key,&copy,BPF_ANY)) error(1);
    emit(&copy,2,0);
    return 0;
}

SEC("kretprobe/__fput")
int closed(__u64 *ctx) {
    __u64 key = bpf_get_current_pid_tgid();
    struct event *value = bpf_map_lookup_elem(&closing,&key);
    if (!value) return 0;
    struct event copy = *value;
    emit(&copy,3,0);
    struct event *active = bpf_map_lookup_elem(&files,&copy.file);
    if (active && active->ns == copy.ns) bpf_map_delete_elem(&files,&copy.file);
    bpf_map_delete_elem(&closing,&key);
    return 0;
}

SEC("kprobe/path_umount")
int unmounting(__u64 *ctx) {
    __u32 pid = owned_pid();
    if (!pid) return 0;
    struct path *path = (void *)ctx[14];
    struct vfsmount *mnt = 0;
    if (bpf_probe_read_kernel(&mnt,sizeof(mnt),__builtin_preserve_access_index(&path->mnt))) { error(2); return 0; }
    struct event value = {.mount=(__u64)mnt,.local_pid=pid,.mount_id=mount_id(mnt)};
    __u64 key = bpf_get_current_pid_tgid();
    if (bpf_map_update_elem(&unmounts,&key,&value,BPF_ANY)) error(1);
    emit(&value,4,(__s32)ctx[13]); /* pt_regs.si */
    return 0;
}

SEC("kretprobe/path_umount")
int unmounted(__u64 *ctx) {
    __u64 key = bpf_get_current_pid_tgid();
    struct event *value = bpf_map_lookup_elem(&unmounts,&key);
    if (!value) return 0;
    struct event copy = *value;
    emit(&copy,5,(__s32)ctx[10]); /* pt_regs.ax */
    bpf_map_delete_elem(&unmounts,&key);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
