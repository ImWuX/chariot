package main

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ArrIncludes(arr []string, str string) bool {
	return slices.ContainsFunc(arr, func(e string) bool {
		return e == str
	})
}

type Tag struct {
	id   string
	kind string
}

func CreateTag(id string, kind string) (Tag, error) {
	tag := Tag{id: id, kind: kind}
	if tag.kind != "" && tag.kind != "host" && tag.kind != "source" {
		return Tag{}, fmt.Errorf("invalid tag kind (%s)", tag.kind)
	}
	if r, _ := regexp.Compile(`^[a-z\-1-9]+$`); !r.Match([]byte(tag.id)) {
		return Tag{}, fmt.Errorf("tag id (%s) contains invalid characters", tag.id)
	}
	return tag, nil
}

func StringToTag(tag string) (Tag, error) {
	tagParts := strings.Split(tag, ":")
	if len(tagParts) > 1 {
		return CreateTag(tagParts[1], tagParts[0])
	} else {
		return CreateTag(tagParts[0], "")
	}
}

func StringsToTags(tags []string) ([]Tag, error) {
	ntags := make([]Tag, 0)
	for _, tag := range tags {
		ntag, err := StringToTag(tag)
		if err != nil {
			return nil, err
		}
		ntags = append(ntags, ntag)
	}
	return ntags, nil
}

func (tag Tag) ToString() string {
	if tag.kind == "" {
		return tag.id
	}
	return fmt.Sprintf("%s:%s", tag.kind, tag.id)
}
