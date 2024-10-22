#include "config.h"
#include "lib.h"
#include "container.h"

#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <fts.h>

#define PATH_CACHE ".chariot-cache"

typedef struct {
    const char *name, *value;
} embed_variable_t;

static char *embed_variables(const char *original, size_t variable_count, embed_variable_t *variables) {
    char *str = strdup(original);
    size_t str_length = strlen(str);

    bool embed = false;
    size_t embed_start = 0;
    for(size_t i = 0; i < str_length; i++) {
        if(embed) {
            if(str[i] == ')') {
                size_t embed_length = i - embed_start + 1;

                assert(embed_length >= 3);
                if(embed_length == 3) continue;

                const char *insert = NULL;
                for(size_t j = 0; j < variable_count; j++) {
                    if(embed_length - 3 != strlen(variables[j].name)) continue;
                    if(strncmp(&str[embed_start + 2], variables[j].name, embed_length - 3) != 0) continue;
                    insert = variables[j].value;
                    break;
                }
                if(insert == NULL) {
                    LIB_ERROR(0, "unknown embed `%.*s`", embed_length - 3, &str[embed_start + 2]);
                    free(str);
                    return NULL;
                }
                size_t insert_length = strlen(insert);

                size_t new_str_length = str_length - embed_length + insert_length;
                if(new_str_length > str_length) str = realloc(str, new_str_length + 1);
                memmove(&str[embed_start + insert_length], &str[embed_start + embed_length], str_length - (embed_start + embed_length) + 1);
                if(new_str_length < str_length) str = realloc(str, new_str_length + 1);
                memcpy(&str[embed_start], insert, insert_length);

                str_length = new_str_length;
                embed = false;
            }
            continue;
        }
        if(str[i] != '@') continue;
        embed_start = i;
        if(i < str_length && str[++i] == '(') embed = true;
        continue;
    }

    return str;
}

static int install_rootfs(const char *rootfs_path) {
    if(lib_path_make(rootfs_path, LIB_DEFAULT_MODE) < 0) return -1;

    char *download_cmd = strdup("wget -qO- https://archive.archlinux.org/iso/2024.08.01/archlinux-bootstrap-x86_64.tar.zst | tar --strip-components 1 -x --zstd -C ");
    size_t cmd_len = strlen(download_cmd);
    size_t rootfs_len = strlen(rootfs_path);
    download_cmd = realloc(download_cmd, cmd_len + rootfs_len + 1);
    memcpy(&download_cmd[cmd_len], rootfs_path, rootfs_len);
    download_cmd[cmd_len + rootfs_len] = '\0';
    if(system(download_cmd) != 0) return -1;

    container_context_t *cc = container_context_make(rootfs_path, "/root");
    if(container_context_exec_shell(cc, "echo 'Server = https://archive.archlinux.org/repos/2024/08/01/$repo/os/$arch' > /etc/pacman.d/mirrorlist") != 0) return -1;
    if(container_context_exec_shell(cc, "echo 'en_US.UTF-8 UTF-8' > /etc/locale.gen") != 0) return -1;
    if(container_context_exec_shell(cc, "locale-gen") != 0) return -1;
    if(container_context_exec_shell(cc, "pacman-key --init") != 0) return -1;
    if(container_context_exec_shell(cc, "pacman-key --populate archlinux") != 0) return -1;
    if(container_context_exec_shell(cc, "pacman --noconfirm -Sy archlinux-keyring") != 0) return -1;
    if(container_context_exec_shell(cc, "pacman --noconfirm -S pacman pacman-mirrorlist") != 0) return -1;
    if(container_context_exec_shell(cc, "pacman --noconfirm -Syu") != 0) return -1;
    if(container_context_exec_shell(cc, "pacman --noconfirm -S ninja meson git wget perl diffutils inetutils python help2man bison flex gettext libtool m4 make patch texinfo which binutils gcc gcc-fortran nasm rsync") != 0) return -1;

    return 0;
}

static int install_deps(recipe_t *recipe, bool runtime, const char *source_deps_dir, const char *host_deps_dir, const char *target_deps_dir, recipe_list_t *installed) {
    for(size_t i = 0; i < recipe->dependency_count; i++) {
        if(runtime && !recipe->dependencies[i].runtime) continue;

        recipe_t *dependency = recipe->dependencies[i].resolved;
        if(recipe_list_find(installed, dependency)) continue;

        LIB_CLEANUP_FREE char *dependency_dir = LIB_PATH_JOIN(PATH_CACHE, recipe_namespace_stringify(dependency->namespace), dependency->name);
        LIB_CLEANUP_FREE char *source_src_dir = LIB_PATH_JOIN(dependency_dir, "src");
        LIB_CLEANUP_FREE char *host_install_dir = LIB_PATH_JOIN(dependency_dir, "install", "usr", "local");
        LIB_CLEANUP_FREE char *target_install_dir = LIB_PATH_JOIN(dependency_dir, "install");

        LIB_CLEANUP_FREE char *source_dep_dir = LIB_PATH_JOIN(source_deps_dir, dependency->name);

        switch(dependency->namespace) {
            case RECIPE_NAMESPACE_SOURCE: if(lib_path_make(source_dep_dir, LIB_DEFAULT_MODE) < 0 || lib_path_copy(source_dep_dir, source_src_dir) < 0) goto error; break;
            case RECIPE_NAMESPACE_HOST: if(lib_path_copy(host_deps_dir, host_install_dir) < 0) goto error; break;
            case RECIPE_NAMESPACE_TARGET: if(lib_path_copy(target_deps_dir, target_install_dir) < 0) goto error; break;
            error:
                LIB_ERROR(0, "failed to install dependency `%s/%s` for recipe `%s/%s`", recipe_namespace_stringify(dependency->namespace), dependency->name, recipe_namespace_stringify(recipe->namespace), recipe->name);
                return -1;
        }

        recipe_list_add(installed, dependency);
        if(install_deps(dependency, true, source_deps_dir, host_deps_dir, target_deps_dir, installed) < 0) return -1;
    }
    return 0;
}

static int process_recipe(recipe_t *recipe) {
    if(recipe->namespace == RECIPE_NAMESPACE_HOST || recipe->namespace == RECIPE_NAMESPACE_TARGET) {
        assert(recipe->host_target.source.resolved != NULL);
        if(process_recipe(recipe->host_target.source.resolved) < 0) return -1;
    }
    for(size_t i = 0; i < recipe->dependency_count; i++) {
        assert(recipe->dependencies[i].resolved != NULL);
        if(process_recipe(recipe->dependencies[i].resolved) < 0) return -1;
    }

    LIB_CLEANUP_FREE char *recipe_dir = LIB_PATH_JOIN(PATH_CACHE, recipe_namespace_stringify(recipe->namespace), recipe->name);
    bool recipe_dir_exists = lib_path_exists(recipe_dir) == 0;

    if(recipe->status.built || (recipe_dir_exists && !recipe->status.invalidated)) return 0;
    printf("> %s/%s\n", recipe_namespace_stringify(recipe->namespace), recipe->name);

    container_context_t *cc = container_context_make(PATH_CACHE "/rootfs", "/root");

    // Generate dependency directories
    LIB_CLEANUP_FREE char *source_deps_dir = LIB_PATH_JOIN(PATH_CACHE, "deps", "source");
    LIB_CLEANUP_FREE char *host_deps_dir = LIB_PATH_JOIN(PATH_CACHE, "deps", "host");
    LIB_CLEANUP_FREE char *target_deps_dir = LIB_PATH_JOIN(PATH_CACHE, "deps", "target");
    if(lib_path_clean(source_deps_dir) < 0 || lib_path_clean(host_deps_dir) < 0 || lib_path_clean(target_deps_dir) < 0) {
        LIB_ERROR(0, "failed to clean deps directories");
        goto terminate;
    }

    recipe_list_t installed = RECIPE_LIST_INIT;
    if(install_deps(recipe, false, source_deps_dir, host_deps_dir, target_deps_dir, &installed) < 0) {
        LIB_ERROR(0, "failed to install dependencies");
        goto terminate;
    }
    recipe_list_free(&installed);

    // Process recipe
    if(lib_path_clean(recipe_dir) < 0) {
        LIB_ERROR(0, "failed to clean recipe directory for recipe `%s/%s`", recipe_namespace_stringify(recipe->namespace), recipe->name);
        goto terminate;
    }

    container_mount_t source_deps_mount = { .dest_path = "/chariot/sources", .src_path = source_deps_dir };
    container_mount_t host_deps_mount = { .dest_path = "/usr/local", .src_path = host_deps_dir };
    container_mount_t target_deps_mount = { .dest_path = "/chariot/sysroot", .src_path = target_deps_dir };

    switch(recipe->namespace) {
        case RECIPE_NAMESPACE_SOURCE: {
            LIB_CLEANUP_FREE char *sums_path = LIB_PATH_JOIN(recipe_dir, "b2sums.txt");
            LIB_CLEANUP_FREE char *archive_path = LIB_PATH_JOIN(recipe_dir, "archive");
            LIB_CLEANUP_FREE char *src_path = LIB_PATH_JOIN(recipe_dir, "src");

            container_context_mounts_add(cc, recipe_dir, "/chariot/source", false);


            if(lib_path_make(src_path, LIB_DEFAULT_MODE) < 0) {
                LIB_ERROR(0, "failed to create src directory for source `%s`", recipe->name);
                goto terminate;
            }

            switch(recipe->source.type) {
                const char *tar_format;
                case RECIPE_SOURCE_TYPE_TAR_GZ: tar_format = "--gzip"; goto tar;
                case RECIPE_SOURCE_TYPE_TAR_XZ: tar_format = "--zstd"; goto tar;
                case RECIPE_SOURCE_TYPE_LOCAL:
                    if(lib_path_exists(recipe->source.url) != 0) {
                        LIB_ERROR(0, "local directory not found `%s` for recipe `%s`", recipe->source.url, recipe->name);
                        goto terminate;
                    }

                    if(lib_path_copy(src_path, recipe->source.url) < 0) {
                        LIB_ERROR(0, "local copy failed for source `%s`", recipe->name);
                        goto terminate;
                    }
                    break;
                tar:
                    if(lib_path_exists(sums_path) != 0 && (lib_path_write(sums_path, recipe->source.b2sum, "w") < 0 || lib_path_write(sums_path, " /chariot/source/archive", "a") < 0)) {
                        LIB_ERROR(0, "failed to write sums for source `%s`", recipe->name);
                        goto terminate;
                    }

                    if(lib_path_exists(archive_path) != 0 && container_context_exec(cc, 4, (const char *[]) { "wget", "-qO", "/chariot/source/archive", recipe->source.url }) != 0) {
                        LIB_ERROR(0, "source download failed for source `%s`", recipe->name);
                        goto terminate;
                    }

                    if(container_context_exec(cc, 3, (const char *[]) { "b2sum", "--check", "/chariot/source/b2sums.txt" }) != 0) {
                        LIB_ERROR(0, "b2sum failed for source `%s`", recipe->name);
                        goto terminate;
                    }

                    if(container_context_exec(cc, 11, (const char *[]) { "tar", "--no-same-owner", "--no-same-permissions", "--strip-components", "1", "-x", tar_format, "-C", "/chariot/source/src", "-f", "/chariot/source/archive" }) != 0) {
                        LIB_ERROR(0, "extraction failed for source `%s`", recipe->name);
                        goto terminate;
                    }
                    break;
            }

            container_mount_t src_mount = { .dest_path = "/chariot/source", .src_path = src_path };

            container_context_set_cwd(cc, "/chariot/source");
            container_context_mounts_clear(cc);
            container_context_mounts_addm(cc, src_mount);

            if(recipe->source.patch != NULL) {
                LIB_CLEANUP_FREE char *patches_path = LIB_PATH_JOIN(PATH_CACHE, "patches");
                LIB_CLEANUP_FREE char *patch_path = LIB_PATH_JOIN(patches_path, recipe->source.patch);
                if(lib_path_exists(patch_path) != 0) {
                    LIB_ERROR(0, "could not locate patch `%s`", recipe->source.patch);
                    goto terminate;
                }

                container_context_mounts_add(cc, patches_path, "/chariot/patches", false);

                LIB_CLEANUP_FREE char *local_patch_path = LIB_PATH_JOIN("/chariot/patches", recipe->source.patch);
                if(container_context_exec(cc, 4, (const char *[]) { "patch", "-p1", "-i", local_patch_path }) != 0) {
                    LIB_ERROR(0, "patch failed for source `%s`", recipe->name);
                    goto terminate;
                }
            }

            container_context_mounts_clear(cc);
            container_context_mounts_addm(cc, source_deps_mount);
            container_context_mounts_addm(cc, host_deps_mount);
            container_context_mounts_addm(cc, target_deps_mount);
            container_context_mounts_addm(cc, src_mount);

            const char *strap = recipe->source.strap;
            if(strap != NULL) {
                strap = embed_variables(strap, 1, (embed_variable_t []) { { .name = "sources_dir", .value = "/chariot/sysroot" } });
                if(strap == NULL) goto terminate;
                if(container_context_exec_shell(cc, strap) != 0) {
                    LIB_ERROR(0, "shell command failed for `%s/%s`", recipe_namespace_stringify(recipe->namespace), recipe->name);
                    goto terminate;
                }
                free((char *) strap);
            }
        } break;
        const char *prefix;
        case RECIPE_NAMESPACE_HOST: prefix = "/usr/local"; goto host_target;
        case RECIPE_NAMESPACE_TARGET: prefix = "/usr"; goto host_target;
        host_target: {
            LIB_CLEANUP_FREE char *build_path = LIB_PATH_JOIN(recipe_dir, "build");
            LIB_CLEANUP_FREE char *install_path = LIB_PATH_JOIN(recipe_dir, "install");
            LIB_CLEANUP_FREE char *source_path = LIB_PATH_JOIN(PATH_CACHE, recipe_namespace_stringify(RECIPE_NAMESPACE_SOURCE), recipe->host_target.source.name, "src");

            if(lib_path_make(build_path, LIB_DEFAULT_MODE) < 0) {
                LIB_ERROR(0, "failed to create build directory for `%s/%s`", recipe_namespace_stringify(recipe->namespace), recipe->name);
                goto terminate;
            }

            if(lib_path_make(install_path, LIB_DEFAULT_MODE) < 0) {
                LIB_ERROR(0, "failed to create install directory for `%s/%s`", recipe_namespace_stringify(recipe->namespace), recipe->name);
                goto terminate;
            }

            container_context_set_cwd(cc, "/chariot/build");
            container_context_mounts_addm(cc, source_deps_mount);
            container_context_mounts_addm(cc, host_deps_mount);
            container_context_mounts_addm(cc, target_deps_mount);
            container_context_mounts_add(cc, source_path, "/chariot/source", false);
            container_context_mounts_add(cc, build_path, "/chariot/build", false);
            container_context_mounts_add(cc, install_path, "/chariot/install", false);

            struct {
                embed_variable_t *embed_variables;
                size_t embed_variable_count;
                const char *command;
            } stages[] = {
                { .command = recipe->host_target.configure, .embed_variable_count = 4, .embed_variables = (embed_variable_t[]) {
                    { .name = "prefix", .value = prefix },
                    { .name = "sysroot_dir", .value = "/chariot/sysroot" },
                    { .name = "sources_dir", .value = "/chariot/sources" },
                    { .name = "source_dir", .value = "/chariot/source" }
                } },
                { .command = recipe->host_target.build, .embed_variable_count = 4, .embed_variables = (embed_variable_t[]) {
                    { .name = "prefix", .value = prefix },
                    { .name = "sysroot_dir", .value = "/chariot/sysroot" },
                    { .name = "sources_dir", .value = "/chariot/sources" },
                    { .name = "thread_count", .value = "8" }
                } },
                { .command = recipe->host_target.install, .embed_variable_count = 4, .embed_variables = (embed_variable_t[]) {
                    { .name = "prefix", .value = prefix },
                    { .name = "sysroot_dir", .value = "/chariot/sysroot" },
                    { .name = "sources_dir", .value = "/chariot/sources" },
                    { .name = "install_dir", .value = "/chariot/install" }
                } }
            };

            for(size_t i = 0; i < sizeof(stages) / sizeof(stages[0]); i++) {
                const char *cmd = stages[i].command;
                if(cmd == NULL) continue;
                if((cmd = embed_variables(cmd, stages[i].embed_variable_count, stages[i].embed_variables)) == NULL) goto terminate;
                if(container_context_exec_shell(cc, cmd) != 0) {
                    LIB_ERROR(0, "shell command failed for `%s/%s`", recipe_namespace_stringify(recipe->namespace), recipe->name);
                    goto terminate;
                }
                free((char *) cmd);
            }
        } break;
    }

    container_context_free(cc);
    recipe->status.built = true;
    return 0;

terminate:
    container_context_free(cc);
    if(lib_path_delete(recipe_dir) < 0) LIB_WARN(0, "failed to cleanup broken build, please do so manually `%s/%s`", recipe_namespace_stringify(recipe->namespace), recipe->name);
    return -1;
}

int main(int argc, char **vargs) {
    // container_context_exec_shell(&(container_context_t) { .rootfs = { .path = PATH_CACHE "/rootfs", .read_only = false }, .cwd = "/root", .gid = 0, .uid = 0, .environment = { .variable_count = 0, .variables = NULL }, .mounts = NULL, .mount_count = 0 }, vargs[1]);
    // return 0;

    // int r = lib_path_delete(PATH_CACHE "/rootfs");
    if(lib_path_exists(PATH_CACHE "/rootfs") != 0) {
        int r = install_rootfs(PATH_CACHE "/rootfs");
    }

    config_t *config = config_read("config.chariot");

    size_t forced_recipe_count = 0;
    recipe_t **forced_recipes = NULL;
    for(size_t i = 0; i < config->recipe_count; i++) {
        config->recipes[i]->status.built = false;
        config->recipes[i]->status.invalidated = false;
        // if(config->recipes[i]->namespace == RECIPE_NAMESPACE_HOST) config->recipes[i]->status.invalidated = true;
        forced_recipes = reallocarray(forced_recipes, ++forced_recipe_count, sizeof(recipe_t *));
        forced_recipes[forced_recipe_count - 1] = config->recipes[i];
    }

    for(size_t i = 0; i < forced_recipe_count; i++) {
        if(process_recipe(forced_recipes[i]) < 0) break;
    }


    return 0;
}