#pragma once

#include "recipe.h"

#include <stddef.h>

typedef struct {
    recipe_t **recipes;
    size_t recipe_count;
} config_t;

config_t *config_read(const char *path);