#include "config.h"

#include <assert.h>
#include <string.h>
#include <ctype.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/stat.h>

typedef struct {
    char *buffer;
    size_t size;
    size_t index;
} parser_data_t;

static bool match_string(parser_data_t *parser, const char *string) {
    size_t string_length = strlen(string);
    if(string_length > parser->size - parser->index) return false;
    if(strncmp(string, &parser->buffer[parser->index], string_length) == 0) {
        parser->index += string_length;
        return true;
    }
    return false;
}

static bool match_char(parser_data_t *parser, char ch) {
    if(parser->size <= parser->index) return false;
    if(parser->buffer[parser->index] == ch) {
        parser->index++;
        return true;
    }
    return false;
}

static void expect_char(parser_data_t *parser, char ch) {
    if(match_char(parser, ch)) return;
    printf("expected `%c`\n", ch);
    exit(EXIT_FAILURE);
}

static void ignore_whitespace(parser_data_t *parser) {
    while(isspace(parser->buffer[parser->index])) parser->index++;
}

static const char *parse_to_eol(parser_data_t *parser) {
    size_t start_index = parser->index;
    while(parser->index <= parser->size && parser->buffer[parser->index] != '\n') parser->index++;
    size_t end_index = parser->index - 1;
    while(end_index > start_index && isspace(parser->buffer[end_index])) end_index--;

    size_t value_length = end_index - start_index + 1;
    char *value = malloc(value_length + 1);
    memcpy(value, &parser->buffer[start_index], value_length);
    value[value_length] = '\0';
    return value;
}

static const char *parse_block(parser_data_t *parser) {
    match_char(parser, '{');
    ignore_whitespace(parser);
    size_t start_index = parser->index;
    while(parser->index <= parser->size && parser->buffer[parser->index] != '}') parser->index++;
    size_t end_index = parser->index - 1;
    while(end_index > start_index && isspace(parser->buffer[end_index])) end_index--;

    match_char(parser, '}');

    size_t block_length = end_index - start_index + 1;
    char *block = malloc(block_length + 1);
    memcpy(block, &parser->buffer[start_index], block_length);
    block[block_length] = '\0';
    return block;
}

static const char *parse_identifier(parser_data_t *parser) {
    size_t start_index = parser->index;
    if(!isalpha(parser->buffer[parser->index]) && parser->buffer[parser->index] != '_') {
        printf("invalid identifier");
        exit(EXIT_FAILURE);
    }
    while(isalnum(parser->buffer[parser->index]) || parser->buffer[parser->index] == '_') parser->index++;

    size_t identifier_length = parser->index - start_index;
    char *identifier = malloc(identifier_length + 1);
    memcpy(identifier, &parser->buffer[start_index], identifier_length);
    identifier[identifier_length] = '\0';
    return identifier;
}

static recipe_namespace_t parse_namespace(parser_data_t *parser) {
    recipe_namespace_t namespace;
    if(match_string(parser, "source")) namespace = RECIPE_NAMESPACE_SOURCE;
    else if(match_string(parser, "host")) namespace = RECIPE_NAMESPACE_HOST;
    else if(match_string(parser, "target")) namespace = RECIPE_NAMESPACE_TARGET;
    else {
        printf("invalid namespace\n");
        exit(EXIT_FAILURE);
    }
    return namespace;
}

static void parse_dependencies(parser_data_t *parser, recipe_dependency_t **dependencies, size_t *dependency_count) {
    recipe_dependency_t *deps = NULL;
    size_t dep_count = 0;

    expect_char(parser, '[');
    while(!match_char(parser, ']')) {
        ignore_whitespace(parser);

        bool is_runtime = match_char(parser, '*');
        recipe_namespace_t namespace = parse_namespace(parser);
        expect_char(parser, '/');
        const char *identifier = parse_identifier(parser);

        deps = reallocarray(deps, ++dep_count, sizeof(recipe_dependency_t));
        deps[dep_count - 1] = (recipe_dependency_t) { .name = identifier, .namespace = namespace, .runtime = is_runtime, .resolved = NULL };

        ignore_whitespace(parser);
    }

    *dependencies = deps;
    *dependency_count = dep_count;
}

static recipe_t *parse_recipe(parser_data_t *parser) {
    recipe_namespace_t namespace = parse_namespace(parser);
    expect_char(parser, '/');
    const char *identifier = parse_identifier(parser);

    recipe_t *recipe = malloc(sizeof(recipe_t));
    recipe->namespace = namespace;
    recipe->name = identifier;
    recipe->dependencies = NULL;
    recipe->dependency_count = 0;

    ignore_whitespace(parser);
    expect_char(parser, '{');

    switch(namespace) {
        case RECIPE_NAMESPACE_SOURCE:
            recipe->source.strap = NULL;
            recipe->source.patch = NULL;
            bool found_url = false, found_b2sum = false, found_type = false;
            while(true) {
                ignore_whitespace(parser);
                if(match_string(parser, "url")) {
                    ignore_whitespace(parser);
                    expect_char(parser, ':');
                    ignore_whitespace(parser);
                    recipe->source.url = parse_to_eol(parser);
                    found_url = true;
                } else if(match_string(parser, "type")) {
                    ignore_whitespace(parser);
                    expect_char(parser, ':');
                    ignore_whitespace(parser);
                    if(match_string(parser, "tar.gz")) recipe->source.type = RECIPE_SOURCE_TYPE_TAR_GZ;
                    else if(match_string(parser, "tar.xz")) recipe->source.type = RECIPE_SOURCE_TYPE_TAR_XZ;
                    else if(match_string(parser, "local")) recipe->source.type = RECIPE_SOURCE_TYPE_LOCAL;
                    else {
                        printf("invalid type\n");
                        exit(EXIT_FAILURE);
                    }
                    found_type = true;
                } else if(match_string(parser, "patch")) {
                    ignore_whitespace(parser);
                    expect_char(parser, ':');
                    ignore_whitespace(parser);
                    recipe->source.patch = parse_to_eol(parser);
                } else if(match_string(parser, "b2sum")) {
                    ignore_whitespace(parser);
                    expect_char(parser, ':');
                    ignore_whitespace(parser);
                    recipe->source.b2sum = parse_to_eol(parser);
                    found_b2sum = true;
                } else if(match_string(parser, "dependencies")) {
                    ignore_whitespace(parser);
                    parse_dependencies(parser, &recipe->dependencies, &recipe->dependency_count);
                } else if(match_string(parser, "strap")) {
                    ignore_whitespace(parser);
                    recipe->source.strap = parse_block(parser);
                } else {
                    expect_char(parser, '}');
                    break;
                }
            }
            if(!found_url) {
                printf("missing url\n");
                exit(EXIT_FAILURE);
            }
            if(!found_type) {
                printf("missing type\n");
                exit(EXIT_FAILURE);
            }
            if(!found_b2sum && (recipe->source.type == RECIPE_SOURCE_TYPE_TAR_GZ || recipe->source.type == RECIPE_SOURCE_TYPE_TAR_XZ)) {
                printf("missing b2sum\n");
                exit(EXIT_FAILURE);
            }
            break;
        case RECIPE_NAMESPACE_HOST:
        case RECIPE_NAMESPACE_TARGET:
            recipe->host_target.source.name = NULL;
            recipe->host_target.source.namespace = RECIPE_NAMESPACE_SOURCE;
            recipe->host_target.source.resolved = NULL;
            recipe->host_target.source.runtime = false;
            recipe->host_target.configure = NULL;
            recipe->host_target.build = NULL;
            recipe->host_target.install = NULL;
            while(true) {
                ignore_whitespace(parser);
                if(match_string(parser, "source")) {
                    ignore_whitespace(parser);
                    expect_char(parser, ':');
                    ignore_whitespace(parser);
                    recipe->host_target.source.name = parse_identifier(parser);
                } else if(match_string(parser, "configure")) {
                    ignore_whitespace(parser);
                    recipe->host_target.configure = parse_block(parser);
                } else if(match_string(parser, "build")) {
                    ignore_whitespace(parser);
                    recipe->host_target.build = parse_block(parser);
                } else if(match_string(parser, "install")) {
                    ignore_whitespace(parser);
                    recipe->host_target.install = parse_block(parser);
                } else if(match_string(parser, "dependencies")) {
                    ignore_whitespace(parser);
                    parse_dependencies(parser, &recipe->dependencies, &recipe->dependency_count);
                } else {
                    expect_char(parser, '}');
                    break;
                }
            }
            if(recipe->host_target.source.name == NULL) {
                printf("missing source\n");
                exit(EXIT_FAILURE);
            }
            break;
        default:
            printf("unsupported namespace\n");
            exit(EXIT_FAILURE);
    }
    return recipe;
}

static recipe_t *find_recipe(recipe_t **recipes, size_t recipe_count, recipe_namespace_t namespace, const char *name) {
    for(size_t i = 0; i < recipe_count; i++) {
        if(recipes[i]->namespace != namespace) continue;
        if(strcmp(recipes[i]->name, name) != 0) continue;
        return recipes[i];
    }
    return NULL;
}

config_t *config_read(const char *path) {
    config_t *config = malloc(sizeof(config_t));

    FILE *f = fopen(path, "r");
    assert(f != NULL);

    struct stat t;
    assert(fstat(fileno(f), &t) == 0);

    parser_data_t data = {
        .buffer = malloc(t.st_size),
        .size = t.st_size,
        .index = 0
    };
    assert(fread(data.buffer, 1, data.size, f) == data.size);
    fclose(f);

    recipe_t **recipes = NULL;
    size_t recipe_count = 0;
    while(true) {
        ignore_whitespace(&data);
        if(data.size <= data.index + 1) break;
        if(match_string(&data, "//")) {
            parse_to_eol(&data);
            continue;
        }
        recipes = reallocarray(recipes, ++recipe_count, sizeof(recipe_t *));
        recipes[recipe_count - 1] = parse_recipe(&data);
    }

    for(size_t i = 0; i < recipe_count; i++) {
        for(size_t j = 0; j < recipes[i]->dependency_count; j++) {
            recipe_t *recipe = find_recipe(recipes, recipe_count, recipes[i]->dependencies[j].namespace, recipes[i]->dependencies[j].name);
            if(recipe == NULL) {
                printf("couldnt find dependency `%s/%s` for recipe `%s/%s`\n", recipe_namespace_stringify(recipes[i]->dependencies[j].namespace), recipes[i]->dependencies[j].name, recipe_namespace_stringify(recipes[i]->namespace), recipes[i]->name);
                exit(EXIT_FAILURE);
            }
            recipes[i]->dependencies[j].resolved = recipe;
        }
        if(recipes[i]->namespace == RECIPE_NAMESPACE_HOST || recipes[i]->namespace == RECIPE_NAMESPACE_TARGET) {
            recipe_t *recipe = find_recipe(recipes, recipe_count, RECIPE_NAMESPACE_SOURCE, recipes[i]->host_target.source.name);
            if(recipe == NULL) {
                printf("couldnt find source `%s`\n", recipes[i]->host_target.source.name);
                exit(EXIT_FAILURE);
            }
            recipes[i]->host_target.source.resolved = recipe;
        }
    }

    config->recipes = recipes;
    config->recipe_count = recipe_count;
    return config;
}