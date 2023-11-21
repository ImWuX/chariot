package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ParseTag(tag string) (string, string) {
	tagParts := strings.Split(tag, ":")
	if len(tagParts) > 1 {
		return tagParts[1], tagParts[0]
	}
	return tagParts[0], ""
}

func MakeTag(tag string, tagType string) string {
	return fmt.Sprintf("%s:%s", tagType, tag)
}

func ArrIncludes(arr []string, str string) bool {
	return slices.ContainsFunc(arr, func(e string) bool {
		return e == str
	})
}
