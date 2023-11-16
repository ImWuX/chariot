package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"

	ChariotContainer "github.com/imwux/chariot/container"
)

type ChariotContext struct {
	cachePath string
}

func (context *ChariotContext) resetContainer() {
	containerPath := filepath.Join(context.cachePath, "container")

	fmt.Println("RESET: Pulling arch image")
	if _, err := os.Stat(filepath.Join(context.cachePath, "archlinux-bootstrap-x86_64.tar.gz")); err != nil {
		cmd := exec.Command("wget", "https://geo.mirror.pkgbuild.com/iso/latest/archlinux-bootstrap-x86_64.tar.gz")
		cmd.Dir = context.cachePath
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		if err := cmd.Wait(); err != nil {
			panic(err)
		}
	}

	if _, err := os.Stat(containerPath); err == nil {
		if err := filepath.WalkDir(containerPath, func(path string, d fs.DirEntry, err error) error {
			if !d.IsDir() {
				return nil
			}
			return os.Chmod(path, 0777)
		}); err != nil {
			panic(err)
		}

		fmt.Println("RESET: Deleting")
		if err := os.RemoveAll(containerPath); err != nil {
			panic(err)
		}
	}

	fmt.Println("RESET: Untaring arch")
	cmd := exec.Command("bsdtar", "-zxf", "archlinux-bootstrap-x86_64.tar.gz")
	cmd.Dir = context.cachePath
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	cmd = exec.Command("mv", "root.x86_64", "container")
	cmd.Dir = context.cachePath
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	fmt.Println("RESET: Rewriting perms")
	cmd = exec.Command("sh", "-c", "for f in $(find ./container -perm 000 2> /dev/null); do chmod 755 \"$f\"; done")
	cmd.Dir = context.cachePath
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	container := ChariotContainer.Use(containerPath)

	fmt.Println("RESET: Running init commands")
	container.Exec("echo 'Server = https://geo.mirror.pkgbuild.com/$repo/os/$arch' > /etc/pacman.d/mirrorlist", "/root", []ChariotContainer.ChariotContainerMount{}, nil, nil)
	container.Exec("echo 'Server = https://mirror.rackspace.com/archlinux/$repo/os/$arch' >> /etc/pacman.d/mirrorlist", "/root", []ChariotContainer.ChariotContainerMount{}, nil, nil)
	container.Exec("echo 'Server = https://mirror.leaseweb.net/archlinux/$repo/os/$arch' >> /etc/pacman.d/mirrorlist", "/root", []ChariotContainer.ChariotContainerMount{}, nil, nil)
	container.Exec("echo 'en_US.UTF-8 UTF-8' > /etc/locale.gen", "/root", []ChariotContainer.ChariotContainerMount{}, nil, nil)
	container.Exec("locale-gen", "/root", []ChariotContainer.ChariotContainerMount{}, nil, nil)
	container.Exec("pacman-key --init", "/root", []ChariotContainer.ChariotContainerMount{}, nil, nil)
	container.Exec("pacman-key --populate archlinux", "/root", []ChariotContainer.ChariotContainerMount{}, nil, nil)
	container.Exec("pacman --noconfirm -Sy archlinux-keyring", "/root", []ChariotContainer.ChariotContainerMount{}, nil, nil)
	container.Exec("pacman --noconfirm -S pacman pacman-mirrorlist", "/root", []ChariotContainer.ChariotContainerMount{}, nil, nil)
	container.Exec("pacman --noconfirm -Syu", "/root", []ChariotContainer.ChariotContainerMount{}, nil, nil)
}

func main() {
	ChariotContainer.HostInit()

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("Chariot vUNKNOWN")
	} else {
		fmt.Printf("Chariot v%s\n", buildInfo.Main.Version)
	}

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	cachePath := flag.String("cache", filepath.Join(cwd, ".chariot-cache"), "Path to the cache directory")
	flag.Parse()

	context := ChariotContext{
		cachePath: *cachePath,
	}

	if err := os.MkdirAll(context.cachePath, 0755); err != nil {
		panic(err)
	}

	context.resetContainer()

	// container := ChariotContainer.Use(".cache/")
}
