#include "container.h"

#define _GNU_SOURCE

#include <stddef.h>
#include <stdarg.h>
#include <limits.h>
#include <string.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <sched.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/wait.h>
#include <sys/mount.h>

typedef struct {
    const char *name, *value;
} env_var_t;

[[noreturn]] static void fatal(const char *fmt, ...) {
    va_list list;
    va_start(list, fmt);
    vprintf(fmt, list);
    va_end(list);
    printf(": %s\n", strerror(errno));
    exit(EXIT_FAILURE);
}

static void warn(const char *fmt, ...) {
    printf("WARN: ");
    va_list list;
    va_start(list, fmt);
    vprintf(fmt, list);
    va_end(list);
    printf("\n");
}

static void write_file(const char *path, const char *data, size_t data_length) {
    int fd = open(path, O_WRONLY);
    if(fd < 0) fatal("could not open %s", path);
    if(write(fd, data, data_length) != data_length) fatal("could not write to %s", path);
    if(close(fd) < 0) warn("could not close %s", path);
}

static void fork_n_exit() {
    int pid_child = fork();
    if(pid_child == 0) return;

    int exit_code = -1;
    if(waitpid(pid_child, &exit_code, 0) < 0) fatal("waitpid failed for pid %i", pid_child);
    exit(WEXITSTATUS(exit_code));
}

static void rootfs_mount(const char *rootfs, const char *from, const char *to, const char *fstype, unsigned long flags) {
    if(to == NULL) to = from;
    char final_to[PATH_MAX];
    snprintf(final_to, PATH_MAX, "%s%s", rootfs, to);
    if(mount(from, final_to, fstype, flags, NULL) < 0) fatal("failed to mount %s to %s in rootfs", from, to);
}

[[noreturn]] static void exec(const char *rootfs, bool rootfs_read_only, int uid, int gid, int env_size, container_environment_variable_t *env, const char *cwd, char **args) {
    int euid = geteuid(), egid = getegid();

    if(unshare(CLONE_NEWUSER | CLONE_NEWPID) < 0) fatal("unshare (user, pid) failed");

    char map[24];
    write_file("/proc/self/setgroups", "deny", 4);
    write_file("/proc/self/uid_map", map, snprintf(map, 24, "%d %d 1", uid, euid));
    write_file("/proc/self/gid_map", map, snprintf(map, 24, "%d %d 1", gid, egid));
    if(setuid(uid) < 0 || setgid(gid) < 0) fatal("failed to set uid/gid");

    fork_n_exit(); // ----------------------------------------------------------------------

    if(unshare(CLONE_NEWNS) < 0) fatal("unshare (ns) failed");

    int rootfs_mount_flags = MS_REMOUNT | MS_BIND | MS_NOSUID | MS_NODEV;
    if(rootfs_read_only) rootfs_mount_flags |= MS_RDONLY;
    if(mount(rootfs, rootfs, NULL, MS_BIND, NULL) < 0) fatal("rootfs mount failed");
    if(mount(rootfs, rootfs, NULL, rootfs_mount_flags, NULL) < 0) fatal ("rootfs remount failed");

    rootfs_mount(rootfs, "/etc/resolv.conf", NULL, NULL, MS_BIND);
    rootfs_mount(rootfs, "/dev", NULL, NULL, MS_BIND | MS_REC | MS_SLAVE);
    rootfs_mount(rootfs, "/sys", NULL, NULL, MS_BIND | MS_REC | MS_SLAVE);
    rootfs_mount(rootfs, NULL, "/run", "tmpfs", 0);
    rootfs_mount(rootfs, NULL, "/tmp", "tmpfs", 0);
    rootfs_mount(rootfs, NULL, "/var/tmp", "tmpfs", 0);
    rootfs_mount(rootfs, NULL, "/proc", "proc", 0);

    if(chroot(rootfs) < 0) fatal("chroot failed");
    if(chdir(cwd) < 0) fatal("chdir failed");

    fork_n_exit(); // ----------------------------------------------------------------------

    clearenv();
    for(int i = 0; i < env_size; i++) setenv(env[i].name, env[i].value, 1);
    if(execvp(args[0], args) < 0) fatal("exec failed");
    __builtin_unreachable();
}

int container_exec(
    const char *rootfs,
    bool rootfs_read_only,
    int uid,
    int gid,
    int environment_variable_count,
    container_environment_variable_t *environment_variables,
    const char *cwd,
    int arg_count,
    const char **args
) {
    // Arguments
    char **f_args = malloc((arg_count + 1) * sizeof(char *));
    for(int i = 0; i < arg_count; i++) f_args[i] = strdup(args[i]);
    f_args[arg_count] = NULL;

    // Environment Variables
    int f_env_size = environment_variable_count;
    container_environment_variable_t *f_env = malloc(f_env_size * sizeof(container_environment_variable_t));
    memcpy(f_env, environment_variables, f_env_size * sizeof(container_environment_variable_t));

    const char *default_path = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin";

    bool found_home = false, found_lang = false;
    char *new_path = NULL;
    for(int i = 0; i < f_env_size; i++) {
        if(!found_home && strcmp(f_env[i].name, "HOME") == 0) found_home = true;
        if(!found_lang && strcmp(f_env[i].name, "LANG") == 0) found_lang = true;
        if(new_path == NULL && strcmp(f_env[i].name, "PATH") == 0) {
            size_t default_path_length = strlen(default_path);
            const char *other_path = f_env[i].value;
            size_t other_path_length = strlen(other_path);

            new_path = malloc(default_path_length + 1 + other_path_length);
            memcpy(new_path, default_path, default_path_length);
            new_path[default_path_length] = ':';
            memcpy(&new_path[default_path_length + 1], other_path, other_path_length);
            new_path[default_path_length + 1 + other_path_length] = '\0';

            f_env[i].value = new_path;
        }
    }
    if(!found_home) {
        f_env = reallocarray(f_env, ++f_env_size, sizeof(container_environment_variable_t));
        f_env[f_env_size - 1] = (container_environment_variable_t) { .name = "HOME", .value = cwd };
    }
    if(!found_lang) {
        f_env = reallocarray(f_env, ++f_env_size, sizeof(container_environment_variable_t));
        f_env[f_env_size - 1] = (container_environment_variable_t) { .name = "LANG", .value = "C" };
    }
    if(new_path == NULL) {
        f_env = reallocarray(f_env, ++f_env_size, sizeof(container_environment_variable_t));
        f_env[f_env_size - 1] = (container_environment_variable_t) { .name = "PATH", .value = default_path };
    }

    // Execution
    int pid_child = fork();
    if(pid_child == 0) {
        exec(rootfs, rootfs_read_only, uid, gid, f_env_size, f_env, cwd, f_args);
        __builtin_unreachable();
    }

    int exit_code = -1;
    if(waitpid(pid_child, &exit_code, 0) < 0) warn("waitpid failed for pid %i", pid_child);

    for(int i = 0; i < arg_count; i++) free(f_args[i]);
    free(f_args);
    if(new_path != NULL) free(new_path);
    free(f_env);

    return exit_code;
}

int container_context_exec(container_context_t *context, int arg_count, const char **args) {
    return container_exec(
        context->rootfs.path,
        context->rootfs.read_only,
        context->uid,
        context->gid,
        context->environment.variable_count,
        context->environment.variables,
        context->cwd,
        arg_count,
        args
    );
}

int container_context_exec_shell(container_context_t *context, const char *arg) {
    return container_context_exec(context, 3, (const char *[]) { "bash", "-c", arg });
}