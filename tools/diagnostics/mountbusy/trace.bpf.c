//go:build ignore
// SPDX-License-Identifier: GPL-2.0-only
#include <linux/bpf.h>
#include "bpf_helpers.h"

struct dentry;
struct list_head { struct list_head *next, *prev; } __attribute__((preserve_access_index));
struct ns_common { unsigned int inum; } __attribute__((preserve_access_index));
struct pid_namespace { struct ns_common ns; } __attribute__((preserve_access_index));
struct mnt_namespace { struct ns_common ns; } __attribute__((preserve_access_index));
struct vfsmount { int mnt_flags; } __attribute__((preserve_access_index));
struct path { struct vfsmount *mnt; struct dentry *dentry; } __attribute__((preserve_access_index));
struct mount {
    struct mount *mnt_parent;
    struct vfsmount mnt;
    int mnt_id;
    struct list_head mnt_mounts;
    struct mnt_namespace *mnt_ns;
} __attribute__((preserve_access_index));
struct upid { int nr; struct pid_namespace *ns; } __attribute__((preserve_access_index));
struct pid { unsigned int level; struct upid numbers[1]; } __attribute__((preserve_access_index));
struct task_struct { struct pid *thread_pid; } __attribute__((preserve_access_index));

struct event {
    __u64 ns, call, pid_tgid, root, candidate, parent;
    __u32 local_pid, root_id, candidate_id, namespace;
    __s32 operation, result, refcnt;
    __u32 flags, has_children, reserved0;
    __u64 reserved1;
    char comm[16];
};

const volatile __u64 pidns_dev = 0;
const volatile __u64 pidns_ino = 0;
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 1024); __type(key, __u64); __type(value, struct event); } unmounts SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 1024); __type(key, __u64); __type(value, struct event); } decisions SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 1024); __type(key, __u64); __type(value, struct event); } queries SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_RINGBUF); __uint(max_entries, 1 << 20); } events SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_ARRAY); __uint(max_entries, 3); __type(key, __u32); __type(value, __u64); } errors SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_ARRAY); __uint(max_entries, 1); __type(key, __u32); __type(value, __u32); } enabled SEC(".maps");

static __always_inline void error(__u32 key) {
    __u64 *n = bpf_map_lookup_elem(&errors, &key);
    if (n) __sync_fetch_and_add(n, 1);
}

static __always_inline __u32 owned_pid(void) {
    __u32 key = 0;
    __u32 *active = bpf_map_lookup_elem(&enabled, &key);
    if (!active || !*active || !pidns_dev || !pidns_ino) return 0;
    struct task_struct *task = (void *)bpf_get_current_task();
    struct pid *pid = 0;
    unsigned int level = 0;
    if (bpf_probe_read_kernel(&pid, sizeof(pid), __builtin_preserve_access_index(&task->thread_pid)) || !pid ||
        bpf_probe_read_kernel(&level, sizeof(level), __builtin_preserve_access_index(&pid->level)) || level >= 32) {
        error(2); return 0;
    }
    void *numbers = __builtin_preserve_access_index(&pid->numbers[0]);
    #pragma unroll
    for (int index = 0; index < 32; index++) {
        if (index > level) break;
        struct upid at = {};
        unsigned int inode = 0;
        if (bpf_probe_read_kernel(&at, sizeof(at), numbers + index * sizeof(struct upid)) ||
            bpf_probe_read_kernel(&inode, sizeof(inode), __builtin_preserve_access_index(&at.ns->ns.inum))) {
            error(2); return 0;
        }
        if (inode == pidns_ino) return at.nr;
    }
    return 0;
}

static __always_inline int metadata(struct event *value, struct mount *m) {
    struct list_head *next = 0;
    struct mnt_namespace *ns = 0;
    value->candidate = (__u64)m;
    value->namespace = 0;
    if (!m || bpf_probe_read_kernel(&value->candidate_id, sizeof(value->candidate_id), __builtin_preserve_access_index(&m->mnt_id)) ||
        bpf_probe_read_kernel(&value->parent, sizeof(value->parent), __builtin_preserve_access_index(&m->mnt_parent)) ||
        bpf_probe_read_kernel(&value->flags, sizeof(value->flags), __builtin_preserve_access_index(&m->mnt.mnt_flags)) ||
        bpf_probe_read_kernel(&next, sizeof(next), __builtin_preserve_access_index(&m->mnt_mounts.next)) ||
        bpf_probe_read_kernel(&ns, sizeof(ns), __builtin_preserve_access_index(&m->mnt_ns))) {
        error(2); return -1;
    }
    value->has_children = next != __builtin_preserve_access_index(&m->mnt_mounts);
    if (ns && (__u64)ns < (__u64)-4095 &&
        bpf_probe_read_kernel(&value->namespace, sizeof(value->namespace), __builtin_preserve_access_index(&ns->ns.inum))) {
        error(2); return -1;
    }
    return 0;
}

static __always_inline void emit(struct event *value, int operation, int result) {
    struct event *out = bpf_ringbuf_reserve(&events, sizeof(*out), 0);
    if (!out) { error(0); return; }
    *out = *value;
    out->ns = bpf_ktime_get_ns();
    out->pid_tgid = bpf_get_current_pid_tgid();
    out->operation = operation;
    out->result = result;
    bpf_get_current_comm(out->comm, sizeof(out->comm));
    bpf_ringbuf_submit(out, 0);
}

SEC("kprobe/path_umount")
int unmount_enter(__u64 *ctx) {
    __u32 pid = owned_pid();
    if (!pid) return 0;
    struct path *path = (void *)ctx[14];
    struct vfsmount *vfs = 0;
    if (bpf_probe_read_kernel(&vfs, sizeof(vfs), __builtin_preserve_access_index(&path->mnt))) { error(2); return 0; }
    struct mount *m = (void *)((char *)vfs - __builtin_preserve_field_info(((struct mount *)0)->mnt, 0));
    struct event value = {.call = bpf_ktime_get_ns(), .root = (__u64)m, .local_pid = pid};
    if (metadata(&value, m)) return 0;
    value.root_id = value.candidate_id;
    __u64 key = bpf_get_current_pid_tgid();
    if (bpf_map_lookup_elem(&unmounts, &key)) error(1);
    if (bpf_map_update_elem(&unmounts, &key, &value, BPF_ANY)) error(1);
    emit(&value, 1, (__s32)ctx[13]);
    return 0;
}

SEC("kretprobe/path_umount")
int unmount_exit(__u64 *ctx) {
    __u64 key = bpf_get_current_pid_tgid();
    struct event *value = bpf_map_lookup_elem(&unmounts, &key);
    if (!value) return 0;
    struct event copy = *value;
    emit(&copy, 2, (__s32)ctx[10]);
    if (bpf_map_lookup_elem(&decisions, &key) || bpf_map_lookup_elem(&queries, &key)) error(1);
    bpf_map_delete_elem(&unmounts, &key);
    return 0;
}

SEC("kprobe/propagate_mount_busy")
int busy_enter(__u64 *ctx) {
    __u64 key = bpf_get_current_pid_tgid();
    struct event *original = bpf_map_lookup_elem(&unmounts, &key);
    if (!original) return 0;
    struct event value = *original;
    if (value.root != ctx[14]) { error(1); return 0; }
    if (metadata(&value, (void *)ctx[14])) return 0;
    value.refcnt = (__s32)ctx[13];
    if (bpf_map_lookup_elem(&decisions, &key)) error(1);
    if (bpf_map_update_elem(&decisions, &key, &value, BPF_ANY)) error(1);
    emit(&value, 3, 0);
    return 0;
}

SEC("kretprobe/propagate_mount_busy")
int busy_exit(__u64 *ctx) {
    __u64 key = bpf_get_current_pid_tgid();
    struct event *value = bpf_map_lookup_elem(&decisions, &key);
    if (!value) return 0;
    struct event copy = *value;
    emit(&copy, 4, (__s32)ctx[10]);
    if (bpf_map_lookup_elem(&queries, &key)) error(1);
    bpf_map_delete_elem(&decisions, &key);
    return 0;
}

SEC("kprobe/mnt_get_count")
int count_enter(__u64 *ctx) {
    __u64 key = bpf_get_current_pid_tgid();
    struct event *original = bpf_map_lookup_elem(&decisions, &key);
    if (!original) return 0;
    struct event value = *original;
    if (metadata(&value, (void *)ctx[14])) return 0;
    if (bpf_map_lookup_elem(&queries, &key)) error(1);
    if (bpf_map_update_elem(&queries, &key, &value, BPF_ANY)) error(1);
    return 0;
}

SEC("kretprobe/mnt_get_count")
int count_exit(__u64 *ctx) {
    __u64 key = bpf_get_current_pid_tgid();
    struct event *value = bpf_map_lookup_elem(&queries, &key);
    if (!value) return 0;
    struct event copy = *value;
    emit(&copy, 5, (__s32)ctx[10]);
    bpf_map_delete_elem(&queries, &key);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
