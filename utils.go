package main

import (
	"fmt"
	"os"
	"path/filepath"
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

type ChariotCache string

func (cache ChariotCache) Path() string {
	return string(cache)
}

func (cache ChariotCache) Container() string {
	return filepath.Join(cache.Path(), "container")
}

func (cache ChariotCache) Sysroot() string {
	return filepath.Join(cache.Path(), "root")
}

func (cache ChariotCache) Sources() string {
	return filepath.Join(cache.Path(), "sources")
}

func (cache ChariotCache) Source(tag string) string {
	return filepath.Join(cache.Sources(), tag)
}

func (cache ChariotCache) Builds(host bool) string {
	sub := "build"
	if host {
		sub = "host-build"
	}
	return filepath.Join(cache.Path(), sub)
}

func (cache ChariotCache) Build(tag string, host bool) string {
	return filepath.Join(cache.Builds(host), tag)
}

func (cache ChariotCache) Init() error {
	if err := os.MkdirAll(cache.Path(), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cache.Sysroot(), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cache.Sources(), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cache.Builds(false), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cache.Builds(true), 0755); err != nil {
		return err
	}
	return nil
}
