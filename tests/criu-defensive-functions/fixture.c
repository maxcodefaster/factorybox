/* Test services for extracted, patched CRIU functions. No procfs writes. */
#define _GNU_SOURCE
#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>

#define pr_err(...) ((void)0)
#define pr_warn(...) ((void)0)
#define BUG_ON(x) assert(!(x))
#define xrealloc realloc
#define xfree free
#define strstartswith(s, p) (!strncmp((s), (p), strlen(p)))
#define PROC_SELF 0
#define TASK_THREAD 1
typedef struct { size_t n_cnames; char **cnames; } CgControllerEntry;
struct pid { int real, state; struct { int virt; } ns[1]; };
struct dirent { char d_name[16]; };
typedef struct { int unused; } DIR;
static DIR directory;
static struct dirent names[] = {{"."}, {".."}, {"11"}, {"12"}, {"13"}};
static int cursor, directory_closes, open_error;
static DIR *opendir_proc(int pid, const char *path)
{
    assert(pid == 11 && !strcmp(path, "task"));
    cursor = 0;
    return open_error ? NULL : &directory;
}
static struct dirent *readdir(DIR *dir)
{
    assert(dir == &directory);
    return cursor == 5 ? NULL : &names[cursor++];
}
static int closedir(DIR *dir)
{
    assert(dir == &directory);
    directory_closes++;
    return 0;
}

static int proc_error, write_error, proc_closes;
static size_t written_size;
static char written[32];
static int open_proc_rw(int pid, const char *name)
{
    assert(pid == PROC_SELF && !strcmp(name, "loginuid"));
    return proc_error ? -1 : 77;
}
static ssize_t test_write(int fd, const void *buf, size_t count)
{
    assert(fd == 77 && count < sizeof(written));
    if (write_error) return -1;
    memcpy(written, buf, count);
    written[count] = 0;
    written_size = count;
    return (ssize_t)count;
}
static int test_close(int fd)
{
    assert(fd == 77);
    proc_closes++;
    return 0;
}
#define write test_write
#define close test_close

enum { FL_UNKNOWN, FL_POSIX, FL_FLOCK, FL_OFD, FL_LEASE };
enum { LEASE_BREAKING = 16 };
struct file_lock {
    long long fl_id, start;
    long i_no;
    unsigned int maj, min;
    int fl_owner, fl_kind, fl_ltype;
    char end[32];
};

/* @CRIU_FUNCTIONS@ */

int main(void)
{
    char dir[64], opt[64];
    char *ordinary[] = {"cpu", "memory"};
    CgControllerEntry ctl = {2, ordinary};
    assert(ctrl_dir_and_opt(&ctl, dir, sizeof(dir), opt, sizeof(opt)) == 10);
    assert(!strcmp(dir, "cpu,memory") && !strcmp(opt, "cpu,memory"));
    char *named[] = {"name=systemd"};
    ctl = (CgControllerEntry){1, named};
    assert(ctrl_dir_and_opt(&ctl, dir, sizeof(dir), opt, sizeof(opt)) == 7);
    assert(!strcmp(dir, "systemd") && !strcmp(opt, "none,name=systemd"));
    char *unified[] = {""};
    ctl = (CgControllerEntry){1, unified};
    assert(ctrl_dir_and_opt(&ctl, dir, 9, NULL, 0) == 7);
    assert(!strcmp(dir, "unified"));
    /* Rejections execute only source with the bounds guards verified by run.py. */
    assert(ctrl_dir_and_opt(&ctl, dir, 0, NULL, 0) == -1);
    assert(ctrl_dir_and_opt(&ctl, dir, sizeof(dir), opt, 0) == -1);
    ctl.n_cnames = 0;
    assert(ctrl_dir_and_opt(&ctl, dir, sizeof(dir), NULL, 0) == -1);

    struct pid *threads = NULL;
    int nr = 0;
    assert(parse_threads(11, &threads, &nr) == 0 && nr == 3);
    assert(directory_closes == 1);
    for (int i = 0; i < nr; i++) {
        assert(threads[i].real == 11 + i && threads[i].state == TASK_THREAD);
        assert(threads[i].ns[0].virt == -1);
    }
    struct pid *same = threads;
    assert(parse_threads(11, &threads, &nr) == 0 && threads == same && nr == 3);
    assert(directory_closes == 2);
    /* Physical storage still has three slots; advertise only two. */
    nr = 2;
    threads[2].real = 991;
    assert(parse_threads(11, &threads, &nr) == -1 && threads == same && nr == 2);
    assert(threads[2].real == 991 && directory_closes == 3);
    open_error = 1;
    assert(parse_threads(11, &threads, &nr) == -1 && directory_closes == 3);
    free(threads);

    unsigned int values[] = {0, 1000, UINT_MAX};
    for (size_t i = 0; i < sizeof(values) / sizeof(values[0]); i++) {
        char expected[32];
        int len = snprintf(expected, sizeof(expected), "%u", values[i]);
        assert(prepare_loginuid(values[i]) == 0);
        assert(written_size == (size_t)len && !memcmp(written, expected, len));
        assert(proc_closes == (int)i + 1);
    }
    proc_error = 1;
    assert(prepare_loginuid(1000) == -1 && proc_closes == 3);
    proc_error = 0;
    write_error = 1;
    assert(prepare_loginuid(1000) == -1 && proc_closes == 4);

    struct file_lock lock = {0};
    char normal[] = "1: POSIX ADVISORY WRITE 11 8:1:42 0 EOF";
    assert(parse_file_lock_buf(normal, &lock, false) == 0);
    assert(lock.fl_kind == FL_POSIX && lock.fl_ltype == F_WRLCK);
    assert(lock.fl_owner == 11 && lock.i_no == 42 && !strcmp(lock.end, "EOF"));
    lock = (struct file_lock){0};
    char blocked[] = "2: -> FLOCK ADVISORY READ 12 8:1:43 1 255";
    assert(parse_file_lock_buf(blocked, &lock, true) == 0);
    assert(lock.fl_kind == FL_FLOCK && lock.fl_ltype == F_RDLCK);
    assert(lock.fl_owner == 12 && lock.start == 1 && !strcmp(lock.end, "255"));
    char invalid[] = "not a file lock";
    assert(parse_file_lock_buf(invalid, &lock, false) == -1);

    puts("patched CRIU function checks passed");
    return 0;
}
