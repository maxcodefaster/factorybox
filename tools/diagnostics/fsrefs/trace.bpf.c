//go:build ignore
// SPDX-License-Identifier: GPL-2.0-only
#include <linux/bpf.h>
#include "bpf_helpers.h"

struct dentry;
struct super_block;
struct vfsmount { struct dentry *mnt_root; struct super_block *mnt_sb; int mnt_flags; } __attribute__((preserve_access_index));
struct path { struct vfsmount *mnt; struct dentry *dentry; } __attribute__((preserve_access_index));
struct mount { struct vfsmount mnt; int mnt_id; } __attribute__((preserve_access_index));
struct ns_common { unsigned int inum; } __attribute__((preserve_access_index));
struct pid_namespace { struct ns_common ns; } __attribute__((preserve_access_index));
struct upid { int nr; struct pid_namespace *ns; } __attribute__((preserve_access_index));
struct pid { unsigned int level; struct upid numbers[1]; } __attribute__((preserve_access_index));
struct task_struct { struct pid *thread_pid; } __attribute__((preserve_access_index));
struct seqcount { unsigned int sequence; } __attribute__((preserve_access_index));
struct seqcount_spinlock { struct seqcount seqcount; } __attribute__((preserve_access_index));
struct seqlock { struct seqcount_spinlock seqcount; } __attribute__((preserve_access_index));
struct fs_struct { struct seqlock seq; struct path root, pwd; } __attribute__((preserve_access_index));
struct kmem_cache { unsigned int object_size; } __attribute__((preserve_access_index));

struct state {
    __u64 fs, root, pwd, generation;
    __u32 root_id, pwd_id;
};
struct event {
    __u64 ns, fs, root, pwd, generation, pid_tgid;
    __u32 local_pid, root_id, pwd_id;
    __s32 operation, result;
    char comm[16];
};
_Static_assert(sizeof(struct event) == 88, "unexpected fs event size");
struct key { __u64 pid_tgid; __u32 operation, pad; };
struct pending { struct state state; __u32 local_pid; };

const volatile __u64 pidns_dev = 0;
const volatile __u64 pidns_ino = 0;
const volatile __u32 fs_struct_bytes = 0;
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 8192); __type(key, __u64); __type(value, struct state); } contexts SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_HASH); __uint(max_entries, 4096); __type(key, struct key); __type(value, struct pending); } calls SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_RINGBUF); __uint(max_entries, 4 << 20); } events SEC(".maps");
struct { __uint(type, BPF_MAP_TYPE_ARRAY); __uint(max_entries, 4); __type(key, __u32); __type(value, __u64); } errors SEC(".maps");
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

static __always_inline int snapshot(struct fs_struct *fs, struct state *out) {
    #pragma unroll
    for (int attempt = 0; attempt < 4; attempt++) {
        unsigned int before = 0, after = 0;
        struct vfsmount *root = 0, *pwd = 0;
        if (bpf_probe_read_kernel(&before,sizeof(before),__builtin_preserve_access_index(&fs->seq.seqcount.seqcount.sequence))) { error(2); return -1; }
        if (before & 1) continue;
        if (bpf_probe_read_kernel(&root,sizeof(root),__builtin_preserve_access_index(&fs->root.mnt)) ||
            bpf_probe_read_kernel(&pwd,sizeof(pwd),__builtin_preserve_access_index(&fs->pwd.mnt)) ||
            bpf_probe_read_kernel(&after,sizeof(after),__builtin_preserve_access_index(&fs->seq.seqcount.seqcount.sequence))) { error(2); return -1; }
        if (before != after || (after & 1)) continue;
        if (!root || !pwd) { error(2); return -1; }
        out->fs = (__u64)fs;
        out->root = (__u64)root;
        out->pwd = (__u64)pwd;
        out->root_id = mount_id(root);
        out->pwd_id = mount_id(pwd);
        return 0;
    }
    error(3);
    return -1;
}

static __always_inline void emit(struct pending *value, int operation, int result) {
    struct event *out = bpf_ringbuf_reserve(&events,sizeof(*out),0);
    if (!out) { error(0); return; }
    __builtin_memset(out,0,sizeof(*out));
    out->ns = bpf_ktime_get_ns();
    out->fs = value->state.fs;
    out->root = value->state.root;
    out->pwd = value->state.pwd;
    out->generation = value->state.generation;
    out->pid_tgid = bpf_get_current_pid_tgid();
    out->local_pid = value->local_pid;
    out->root_id = value->state.root_id;
    out->pwd_id = value->state.pwd_id;
    out->operation = operation;
    out->result = result;
    bpf_get_current_comm(out->comm,sizeof(out->comm));
    bpf_ringbuf_submit(out,0);
}

static __always_inline void remember(int operation, struct pending *value) {
    struct key key = {.pid_tgid=bpf_get_current_pid_tgid(),.operation=operation};
    if (bpf_map_lookup_elem(&calls,&key)) error(1);
    if (bpf_map_update_elem(&calls,&key,value,BPF_ANY)) error(1);
}

static __always_inline int enter_set(__u64 *ctx, int operation) {
    __u32 pid = owned_pid();
    if (!pid) return 0;
    struct pending value = {.state.fs=ctx[14],.local_pid=pid};
    remember(operation,&value);
    return 0;
}

static __always_inline int finish_set(int operation, struct fs_struct *created) {
    struct key key = {.pid_tgid=bpf_get_current_pid_tgid(),.operation=operation};
    struct pending *saved = bpf_map_lookup_elem(&calls,&key);
    if (!saved) return 0;
    struct pending value = *saved;
    bpf_map_delete_elem(&calls,&key);
    struct fs_struct *fs = created ? created : (void *)value.state.fs;
    if (!fs) return 0; /* copy_fs_struct can return NULL on allocation failure. */
    if (snapshot(fs,&value.state)) return 0;
    struct state *previous = bpf_map_lookup_elem(&contexts,&value.state.fs);
    value.state.generation = operation != 1 && previous ? previous->generation : bpf_ktime_get_ns();
    if (bpf_map_update_elem(&contexts,&value.state.fs,&value.state,BPF_ANY)) error(1);
    emit(&value,operation,0);
    return 0;
}

SEC("kprobe/copy_fs_struct")
int copying(__u64 *ctx) {
    __u32 pid = owned_pid();
    if (!pid) return 0;
    struct pending value = {.local_pid=pid};
    remember(1,&value);
    return 0;
}
SEC("kretprobe/copy_fs_struct")
int copied(__u64 *ctx) { return finish_set(1,(void *)ctx[10]); }
SEC("kprobe/set_fs_pwd")
int setting_pwd(__u64 *ctx) { return enter_set(ctx,2); }
SEC("kretprobe/set_fs_pwd")
int set_pwd(__u64 *ctx) { return finish_set(2,0); }
SEC("kprobe/set_fs_root")
int setting_root(__u64 *ctx) { return enter_set(ctx,3); }
SEC("kretprobe/set_fs_root")
int set_root(__u64 *ctx) { return finish_set(3,0); }

SEC("raw_tracepoint/kmem_cache_free")
int freed(struct bpf_raw_tracepoint_args *ctx) {
    __u64 fs = ctx->args[1];
    struct state *state = bpf_map_lookup_elem(&contexts,&fs);
    if (!state) return 0;
    struct kmem_cache *cache = (void *)ctx->args[2];
    __u32 size = 0;
    if (bpf_probe_read_kernel(&size,sizeof(size),__builtin_preserve_access_index(&cache->object_size)) ||
        !fs_struct_bytes || size != fs_struct_bytes) { error(2); return 0; }
    struct pending value = {.state=*state,.local_pid=owned_pid()};
    // The allocator tracepoint survives an inlined free_fs_struct wrapper.
    // It closes the tracked object generation without reading freed memory.
    emit(&value,5,size);
    struct state *active = bpf_map_lookup_elem(&contexts,&value.state.fs);
    if (active && active->generation == value.state.generation) bpf_map_delete_elem(&contexts,&value.state.fs);
    return 0;
}

SEC("kprobe/path_umount")
int unmounting(__u64 *ctx) {
    __u32 pid = owned_pid();
    if (!pid) return 0;
    struct path *path = (void *)ctx[14];
    struct vfsmount *mnt = 0;
    if (bpf_probe_read_kernel(&mnt,sizeof(mnt),__builtin_preserve_access_index(&path->mnt))) { error(2); return 0; }
    struct pending value = {.state.root=(__u64)mnt,.state.root_id=mount_id(mnt),.local_pid=pid};
    remember(6,&value);
    emit(&value,6,(__s32)ctx[13]);
    return 0;
}
SEC("kretprobe/path_umount")
int unmounted(__u64 *ctx) {
    struct key key = {.pid_tgid=bpf_get_current_pid_tgid(),.operation=6};
    struct pending *saved = bpf_map_lookup_elem(&calls,&key);
    if (!saved) return 0;
    struct pending value = *saved;
    emit(&value,7,(__s32)ctx[10]);
    bpf_map_delete_elem(&calls,&key);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
