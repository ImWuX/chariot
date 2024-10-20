#pragma once

typedef struct {
    const char *name, *value;
} container_environment_variable_t;

typedef struct {
    struct {
        const char *path;
        bool read_only;
    } rootfs;
    const char *cwd;
    int uid, gid;
    struct {
        container_environment_variable_t *variables;
        int variable_count;
    } environment;
} container_context_t;

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
);

int container_context_exec(container_context_t *context, int arg_count, const char **args);