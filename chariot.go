package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ChariotCLI "github.com/imwux/chariot/cli"
	ChariotContainer "github.com/imwux/chariot/container"
)

const DEFAULT_FILE_PERM = 0755

type ChariotOptions struct {
	cache          string
	refetchSources bool
	resetContainer bool
	verbose        bool
	threads        uint
}

type ChariotContext struct {
	options *ChariotOptions
	targets []*ChariotTarget
	cli     *ChariotCLI.CLI
	cache   ChariotCache
}

type ChariotTarget struct {
	tag          Tag
	dependencies []*ChariotTarget

	built   bool
	touched bool
	redo    bool

	do func() error
}

func main() {
	ChariotContainer.HostInit()

	cli := ChariotCLI.CreateCLI(os.Stdout)
	cli.Println("Chariot")

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	config := flag.String("config", "chariot.toml", "Path to the config file")
	cache := flag.String("cache", filepath.Join(cwd, ".chariot-cache"), "Path to the cache directory")
	refetchSources := flag.Bool("refetch-sources", false, "Refetch all sources")
	resetContainer := flag.Bool("reset-container", false, "Create a new container")
	verbose := flag.Bool("verbose", false, "Turn on verbose logging")
	threads := flag.Uint("threads", 8, "Number of simultaneous threads to use")
	// shell := flag.Bool("shell", false, "Open shell into the container")
	flag.Parse()

	ctx := &ChariotContext{
		options: &ChariotOptions{
			cache:          *cache,
			refetchSources: *refetchSources,
			resetContainer: *resetContainer,
			verbose:        *verbose,
			threads:        *threads,
		},
		cli:   cli,
		cache: ChariotCache(*cache),
	}

	cfg := ReadConfig(*config)

	targets, err := cfg.BuildTargets(ctx)
	if err != nil {
		cli.Println(err)
		return
	}
	ctx.targets = targets

	doTargets := make([]*ChariotTarget, 0)
	for _, stag := range flag.Args() {
		tag, err := StringToTag(stag)
		if err != nil {
			cli.Println(err)
			return
		}
		for _, target := range targets {
			if target.tag != tag {
				continue
			}
			target.redo = true
			doTargets = append(doTargets, target)
		}
	}

	if err := ctx.cache.Init(); err != nil {
		cli.Println(err)
		return
	}

	if !FileExists(filepath.Join(ctx.cache.Path(), "archlinux-bootstrap-x86_64.tar.gz")) {
		ctx.cli.StartSpinner("Downloading arch linux image")
		cmd := exec.Command("wget", "https://geo.mirror.pkgbuild.com/iso/latest/archlinux-bootstrap-x86_64.tar.gz")
		cmd.Dir = ctx.cache.Path()
		if err := cmd.Start(); err != nil {
			cli.Println(err)
			return
		}
		if err := cmd.Wait(); err != nil {
			cli.Println(err)
			return
		}
		ctx.cli.StopSpinner()
	}
	if ctx.options.resetContainer {
		ctx.wipeContainer()
	}
	if !FileExists(ctx.cache.ContainerPath()) {
		ctx.initContainer()
	}

	cli.Printf("Project: %s\n", cfg.Project.Name)
	for _, target := range doTargets {
		if err := ctx.do(target); err != nil {
			cli.Println(err)
			return
		}
	}
}

func (ctx *ChariotContext) wipeContainer() {
	ctx.cli.StartSpinner("Deleting container")
	defer ctx.cli.StopSpinner()

	if err := filepath.WalkDir(ctx.cache.ContainerPath(), func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			return nil
		}
		return os.Chmod(path, 0777)
	}); err != nil {
		panic(err)
	}

	if err := os.RemoveAll(ctx.cache.ContainerPath()); err != nil {
		panic(err)
	}
}

func (ctx *ChariotContext) initContainer() {
	ctx.cli.StartSpinner("Initializing container")
	defer ctx.cli.StopSpinner()

	if _, err := os.Stat(filepath.Join(ctx.cache.Path(), "archlinux-bootstrap-x86_64.tar.gz")); err != nil {
		ctx.cli.SetSpinnerMessage("Downloading arch linux image")
		cmd := exec.Command("wget", "https://geo.mirror.pkgbuild.com/iso/latest/archlinux-bootstrap-x86_64.tar.gz")
		cmd.Dir = ctx.cache.Path()
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		if err := cmd.Wait(); err != nil {
			panic(err)
		}
	}

	ctx.cli.SetSpinnerMessage("Extracting arch linux image")
	cmd := exec.Command("bsdtar", "-zxf", "archlinux-bootstrap-x86_64.tar.gz")
	cmd.Dir = ctx.cache.Path()
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	cmd = exec.Command("mv", "root.x86_64", "container")
	cmd.Dir = ctx.cache.Path()
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	ctx.cli.SetSpinnerMessage("Rewriting container permissions")
	cmd = exec.Command("sh", "-c", "for f in $(find ./container -perm 000 2> /dev/null); do chmod 755 \"$f\"; done")
	cmd.Dir = ctx.cache.Path()
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	ctx.cli.SetSpinnerMessage("Running initialization commands")
	execContext := ChariotContainer.Use(ctx.cache.ContainerPath(), "/root", []ChariotContainer.Mount{}, nil, nil)
	execContext.Exec("echo 'Server = https://geo.mirror.pkgbuild.com/$repo/os/$arch' > /etc/pacman.d/mirrorlist")
	execContext.Exec("echo 'Server = https://mirror.rackspace.com/archlinux/$repo/os/$arch' >> /etc/pacman.d/mirrorlist")
	execContext.Exec("echo 'Server = https://mirror.leaseweb.net/archlinux/$repo/os/$arch' >> /etc/pacman.d/mirrorlist")
	execContext.Exec("echo 'en_US.UTF-8 UTF-8' > /etc/locale.gen")
	execContext.Exec("locale-gen")
	execContext.Exec("pacman-key --init")
	execContext.Exec("pacman-key --populate archlinux")
	execContext.Exec("pacman --noconfirm -Sy archlinux-keyring")
	execContext.Exec("pacman --noconfirm -S pacman pacman-mirrorlist")
	execContext.Exec("pacman --noconfirm -Syu")
	execContext.Exec("pacman --noconfirm -S ninja meson git wget perl diffutils inetutils python help2man bison flex gettext libtool m4 make patch texinfo which binutils gcc gcc-fortran")
}

func (ctx *ChariotContext) prepContainer(deps []string) error {
	if err := os.RemoveAll(ctx.cache.HostPath()); err != nil {
		return err
	}
	if err := os.RemoveAll(ctx.cache.SysrootPath()); err != nil {
		return err
	}
	if err := os.MkdirAll(ctx.cache.HostPath(), DEFAULT_FILE_PERM); err != nil {
		return err
	}
	if err := os.MkdirAll(ctx.cache.SysrootPath(), DEFAULT_FILE_PERM); err != nil {
		return err
	}

	// var installDeps func(deps []string) error
	installDeps := func(deps []string) error {
		for _, dep := range deps {
			depTag, err := StringToTag(dep)
			if err != nil {
				continue
			}
			switch depTag.kind {
			case "source":
				continue
			case "host":
				// TODO: << Recurse thru runtime deps
				if err := CopyDirectory(filepath.Join(ctx.cache.BuiltPath(depTag.id, true), "usr", "local"), ctx.cache.HostPath()); err != nil {
					return err
				}
			case "":
				if err := CopyDirectory(ctx.cache.BuiltPath(depTag.id, false), ctx.cache.SysrootPath()); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return installDeps(deps)
}

func (ctx *ChariotContext) writers() (io.Writer, io.Writer) {
	var verboseWriter io.Writer = nil
	if ctx.options.verbose {
		verboseWriter = ctx.cli.GetWriter(false, ChariotCLI.LIGHT_GRAY)
	}
	return verboseWriter, ctx.cli.GetWriter(true, ChariotCLI.LIGHT_RED)
}

func (ctx *ChariotContext) do(target *ChariotTarget) error {
	if target.touched {
		return nil
	}
	target.touched = true

	for _, dep := range target.dependencies {
		if err := ctx.do(dep); err != nil {
			return err
		}
	}

	if target.built && !target.redo {
		return nil
	}
	target.redo = false

	ctx.cli.Printf(">> %s\n", target.tag.ToString())
	return target.do()
}

func (ctx *ChariotContext) makeSourceDoer(tag Tag, source *ConfigSource) func() error {
	return func() (err error) {
		sourcePath := ctx.cache.SourcePath(tag.id)

		ctx.cli.StartSpinner("Initializing source %s", tag.ToString())
		defer ctx.cli.StopSpinner()

		if err := os.MkdirAll(sourcePath, DEFAULT_FILE_PERM); err != nil {
			return err
		}
		defer func() {
			if err != nil {
				os.RemoveAll(sourcePath)
			}
		}()

		ctx.cli.SetSpinnerMessage("Fetching source %s", tag.ToString())
		var cmd *exec.Cmd
		switch source.Type {
		case "tar":
			cmd = exec.Command("sh", "-c", fmt.Sprintf("wget -qO- %s | tar --strip-components 1 -xvz -C %s", source.Url, sourcePath))
		case "local":
			cmd = exec.Command("cp", "-r", "-T", source.Url, sourcePath)
		default:
			return fmt.Errorf("source %s has an invalid type (%s)", tag.ToString(), source.Type)
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		if err := cmd.Wait(); err != nil {
			return err
		}

		ctx.cli.SetSpinnerMessage("Applying source modifications %s", tag.ToString())
		for _, modifier := range source.Modifiers {
			modSourcePath := ctx.cache.SourcePath(modifier.Source)
			switch modifier.Type {
			case "patch":
				cmd = exec.Command("patch", "-p1", "-i", filepath.Join(modSourcePath, modifier.File))
			case "merge":
				cmd = exec.Command("cp", "-r", fmt.Sprintf("%s/.", modSourcePath), ".")
			case "exec":
				deps := source.Dependencies
				for _, modifier := range source.Modifiers {
					depTag, err := CreateTag(modifier.Source, "source")
					if err != nil {
						continue
					}
					deps = append(deps, depTag.ToString())
				}
				if err := ctx.prepContainer(deps); err != nil {
					return err
				}

				verboseWriter, errorWriter := ctx.writers()
				execCtx := ChariotContainer.Use(ctx.cache.ContainerPath(), "/chariot/source", []ChariotContainer.Mount{
					{To: "/usr/local", From: ctx.cache.HostPath()},
					{To: "/chariot/root", From: ctx.cache.SysrootPath()},
					{To: "/chariot/sources", From: ctx.cache.SourcesPath()},
					{To: "/chariot/source", From: sourcePath},
				}, verboseWriter, errorWriter)

				cmd := modifier.Cmd
				for _, tagStr := range deps {
					tag, err := StringToTag(tagStr)
					if err != nil || tag.kind != "source" {
						continue
					}
					cmd = strings.ReplaceAll(cmd, fmt.Sprintf("$SOURCE:%s", tag.id), fmt.Sprintf("/chariot/sources/%s", tag.id))
				}
				cmd = strings.ReplaceAll(cmd, "$THREADS", fmt.Sprint(ctx.options.threads))

				if err := execCtx.Exec(cmd); err != nil {
					return err
				}
				continue
			default:
				return fmt.Errorf("source %s has an invalid (%s)", tag.ToString(), modifier.Type)
			}
			cmd.Dir = sourcePath
			if err := cmd.Start(); err != nil {
				return err
			}
			if err := cmd.Wait(); err != nil {
				return err
			}
		}

		return nil
	}
}

func (ctx *ChariotContext) makeHostDoer(tag Tag, host *ConfigHost) func() error {
	return func() (err error) {
		buildDir := ctx.cache.BuildPath(tag.id, true)
		builtDir := ctx.cache.BuiltPath(tag.id, true)

		ctx.cli.StartSpinner("Preparing %s", tag.ToString())
		defer ctx.cli.StopSpinner()

		if err := os.MkdirAll(buildDir, DEFAULT_FILE_PERM); err != nil {
			return err
		}
		if err := os.MkdirAll(builtDir, DEFAULT_FILE_PERM); err != nil {
			return err
		}
		defer func() {
			if err != nil {
				os.RemoveAll(buildDir)
				os.RemoveAll(builtDir)
			}
		}()

		if err := ctx.prepContainer(append(host.Dependencies, host.RuntimeDependencies...)); err != nil {
			return err
		}

		verboseWriter, errorWriter := ctx.writers()
		execContext := ChariotContainer.Use(ctx.cache.ContainerPath(), "/chariot/build", []ChariotContainer.Mount{
			{To: "/usr/local", From: ctx.cache.HostPath()},
			{To: "/chariot/root", From: ctx.cache.SysrootPath()},
			{To: "/chariot/build", From: buildDir},
			{To: "/chariot/install", From: builtDir},
			{To: "/chariot/sources", From: ctx.cache.SourcesPath()},
		}, verboseWriter, errorWriter)

		processCmd := func(cmd string) string {
			for _, dep := range host.Dependencies {
				tag, err := StringToTag(dep)
				if err != nil || tag.kind != "source" {
					continue
				}
				cmd = strings.ReplaceAll(cmd, fmt.Sprintf("$SOURCE:%s", tag.id), fmt.Sprintf("/chariot/sources/%s", tag.id))
			}
			cmd = strings.ReplaceAll(cmd, "$ROOT", "/chariot/root")
			cmd = strings.ReplaceAll(cmd, "$BUILD", "/chariot/build")
			cmd = strings.ReplaceAll(cmd, "$INSTALL", "/chariot/install")
			cmd = strings.ReplaceAll(cmd, "$THREADS", fmt.Sprint(ctx.options.threads))
			return cmd
		}

		ctx.cli.SetSpinnerMessage("Configuring %s", tag.ToString())
		for _, cmd := range host.Configure {
			if err := execContext.Exec(processCmd(cmd)); err != nil {
				return err
			}
		}
		ctx.cli.SetSpinnerMessage("Building %s", tag.ToString())
		for _, cmd := range host.Build {
			if err := execContext.Exec(processCmd(cmd)); err != nil {
				return err
			}
		}
		ctx.cli.SetSpinnerMessage("Installing %s", tag.ToString())
		for _, cmd := range host.Install {
			if err := execContext.Exec(processCmd(cmd)); err != nil {
				return err
			}
		}

		return nil
	}
}

func (ctx *ChariotContext) makeTargetDoer(tag Tag, target *ConfigTarget) func() error {
	return func() (err error) {
		buildDir := ctx.cache.BuildPath(tag.id, false)
		builtDir := ctx.cache.BuiltPath(tag.id, false)

		ctx.cli.StartSpinner("Preparing %s", tag.ToString())
		defer ctx.cli.StopSpinner()

		if err := os.MkdirAll(buildDir, DEFAULT_FILE_PERM); err != nil {
			return err
		}
		if err := os.MkdirAll(builtDir, DEFAULT_FILE_PERM); err != nil {
			return err
		}
		defer func() {
			if err != nil {
				os.RemoveAll(buildDir)
				os.RemoveAll(builtDir)
			}
		}()

		if err := ctx.prepContainer(target.Dependencies); err != nil {
			return err
		}

		verboseWriter, errorWriter := ctx.writers()
		execContext := ChariotContainer.Use(ctx.cache.ContainerPath(), "/chariot/build", []ChariotContainer.Mount{
			{To: "/usr/local", From: ctx.cache.HostPath()},
			{To: "/chariot/root", From: ctx.cache.SysrootPath()},
			{To: "/chariot/build", From: buildDir},
			{To: "/chariot/install", From: builtDir},
			{To: "/chariot/sources", From: ctx.cache.SourcesPath()},
		}, verboseWriter, errorWriter)

		processCmd := func(cmd string) string {
			for _, dep := range target.Dependencies {
				tag, err := StringToTag(dep)
				if err != nil || tag.kind != "source" {
					continue
				}
				cmd = strings.ReplaceAll(cmd, fmt.Sprintf("$SOURCE:%s", tag.id), fmt.Sprintf("/chariot/sources/%s", tag.id))
			}
			cmd = strings.ReplaceAll(cmd, "$ROOT", "/chariot/root")
			cmd = strings.ReplaceAll(cmd, "$BUILD", "/chariot/build")
			cmd = strings.ReplaceAll(cmd, "$INSTALL", "/chariot/install")
			cmd = strings.ReplaceAll(cmd, "$THREADS", fmt.Sprint(ctx.options.threads))
			return cmd
		}

		ctx.cli.SetSpinnerMessage("Configuring %s", tag.ToString())
		for _, cmd := range target.Configure {
			if err := execContext.Exec(processCmd(cmd)); err != nil {
				return err
			}
		}
		ctx.cli.SetSpinnerMessage("Building %s", tag.ToString())
		for _, cmd := range target.Build {
			if err := execContext.Exec(processCmd(cmd)); err != nil {
				return err
			}
		}
		ctx.cli.SetSpinnerMessage("Installing %s", tag.ToString())
		for _, cmd := range target.Install {
			if err := execContext.Exec(processCmd(cmd)); err != nil {
				return err
			}
		}

		return nil
	}
}
