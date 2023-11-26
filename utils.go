package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
)

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func CopyDirectory(scrDir string, dest string) error {
	entries, err := os.ReadDir(scrDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(scrDir, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		fileInfo, err := os.Stat(sourcePath)
		if err != nil {
			return err
		}

		stat, ok := fileInfo.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to get raw syscall.Stat_t data for '%s'", sourcePath)
		}

		switch fileInfo.Mode() & os.ModeType {
		case os.ModeDir:
			if err := CreateIfNotExists(destPath, 0755); err != nil {
				return err
			}
			if err := CopyDirectory(sourcePath, destPath); err != nil {
				return err
			}
		case os.ModeSymlink:
			if err := CopySymLink(sourcePath, destPath); err != nil {
				return err
			}
		default:
			if err := Copy(sourcePath, destPath); err != nil {
				return err
			}
		}

		if err := os.Lchown(destPath, int(stat.Uid), int(stat.Gid)); err != nil {
			return err
		}

		fInfo, err := entry.Info()
		if err != nil {
			return err
		}

		isSymlink := fInfo.Mode()&os.ModeSymlink != 0
		if !isSymlink {
			if err := os.Chmod(destPath, fInfo.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}

func Copy(srcFile string, dstFile string) error {
	out, err := os.Create(dstFile)
	if err != nil {
		return err
	}

	defer out.Close()

	in, err := os.Open(srcFile)
	if err != nil {
		return err
	}

	defer in.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return nil
}

func CreateIfNotExists(dir string, perm os.FileMode) error {
	if FileExists(dir) {
		return nil
	}

	if err := os.MkdirAll(dir, perm); err != nil {
		return fmt.Errorf("failed to create directory: '%s', error: '%s'", dir, err.Error())
	}

	return nil
}

func CopySymLink(source string, dest string) error {
	link, err := os.Readlink(source)
	if err != nil {
		return err
	}
	return os.Symlink(link, dest)
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

type ChariotCache string

func (cache ChariotCache) Path() string {
	return string(cache)
}

func (cache ChariotCache) ContainerPath() string {
	return filepath.Join(cache.Path(), "container")
}

func (cache ChariotCache) SysrootPath() string {
	return filepath.Join(cache.Path(), "root")
}

func (cache ChariotCache) HostPath() string {
	return filepath.Join(cache.Path(), "hostroot")
}

func (cache ChariotCache) SourcesPath() string {
	return filepath.Join(cache.Path(), "sources")
}

func (cache ChariotCache) SourcePath(id string) string {
	return filepath.Join(cache.SourcesPath(), id)
}

func (cache ChariotCache) BuildsPath(host bool) string {
	sub := "build"
	if host {
		sub = "host-build"
	}
	return filepath.Join(cache.Path(), sub)
}

func (cache ChariotCache) BuildPath(id string, host bool) string {
	return filepath.Join(cache.BuildsPath(host), id)
}

func (cache ChariotCache) BuiltsPath(host bool) string {
	sub := "built"
	if host {
		sub = "host-built"
	}
	return filepath.Join(cache.Path(), sub)
}

func (cache ChariotCache) BuiltPath(id string, host bool) string {
	return filepath.Join(cache.BuiltsPath(host), id)
}

func (cache ChariotCache) Init() error {
	if err := os.MkdirAll(cache.Path(), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cache.SourcesPath(), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cache.BuildsPath(false), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cache.BuildsPath(true), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cache.BuiltsPath(false), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(cache.BuiltsPath(true), 0755); err != nil {
		return err
	}
	return nil
}
