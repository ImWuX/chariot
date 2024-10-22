#include "recipe.h"

#include <assert.h>
#include <stdlib.h>

const char *recipe_namespace_stringify(recipe_namespace_t namespace) {
    switch(namespace) {
        case RECIPE_NAMESPACE_SOURCE: return "source";
        case RECIPE_NAMESPACE_HOST: return "host";
        case RECIPE_NAMESPACE_TARGET: return "target";
    }
    unreachable();
}

void recipe_list_add(recipe_list_t *list, recipe_t *recipe) {
    assert(!recipe_list_find(list, recipe));
    list->recipes = reallocarray(list->recipes, ++list->recipe_count, sizeof(recipe_t *));
    list->recipes[list->recipe_count - 1] = recipe;
}

bool recipe_list_find(recipe_list_t *list, recipe_t *recipe) {
    for(size_t i = 0; i < list->recipe_count; i++) {
        if(list->recipes[i] != recipe) continue;
        return true;
    }
    return false;
}

void recipe_list_free(recipe_list_t *list) {
    free(list->recipes);
}