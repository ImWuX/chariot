package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"

	ChariotCLI "github.com/imwux/chariot/cli"
	ChariotContainer "github.com/imwux/chariot/container"
)

type ChariotOptions struct {
	threads        uint
	debugError     bool
	debugVerbose   bool
	refetchSources bool
	resetContainer bool
}

type ChariotState struct {
	targets      []string
	targetStates map[string]int
}

type ChariotContext struct {
	cwd       string
	cachePath string
	config    Config
	options   ChariotOptions
	state     ChariotState
	cli       *ChariotCLI.CLI
}

const (
	TARGET_STATE_NONE   = 0
	TARGET_STATE_QUEUED = 1
	TARGET_STATE_DONE   = 2
	TARGET_STATE_FAILED = 3
)

func (ctx *ChariotContext) wipeContainer() {
	ctx.cli.StartSpinner("Deleting container")
	defer ctx.cli.StopSpinner()

	containerPath := filepath.Join(ctx.cachePath, "container")

	if err := filepath.WalkDir(containerPath, func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			return nil
		}
		return os.Chmod(path, 0777)
	}); err != nil {
		panic(err)
	}

	if err := os.RemoveAll(containerPath); err != nil {
		panic(err)
	}
}

func (ctx *ChariotContext) initContainer() {
	ctx.cli.StartSpinner("Initializing container")
	defer ctx.cli.StopSpinner()

	containerPath := filepath.Join(ctx.cachePath, "container")

	if _, err := os.Stat(filepath.Join(ctx.cachePath, "archlinux-bootstrap-x86_64.tar.gz")); err != nil {
		ctx.cli.SetSpinnerMessage("Downloading arch linux image")
		cmd := exec.Command("wget", "https://geo.mirror.pkgbuild.com/iso/latest/archlinux-bootstrap-x86_64.tar.gz")
		cmd.Dir = ctx.cachePath
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		if err := cmd.Wait(); err != nil {
			panic(err)
		}
	}

	ctx.cli.SetSpinnerMessage("Extracting arch linux image")
	cmd := exec.Command("bsdtar", "-zxf", "archlinux-bootstrap-x86_64.tar.gz")
	cmd.Dir = ctx.cachePath
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	cmd = exec.Command("mv", "root.x86_64", "container")
	cmd.Dir = ctx.cachePath
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	ctx.cli.SetSpinnerMessage("Rewriting container permissions")
	cmd = exec.Command("sh", "-c", "for f in $(find ./container -perm 000 2> /dev/null); do chmod 755 \"$f\"; done")
	cmd.Dir = ctx.cachePath
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	ctx.cli.SetSpinnerMessage("Running initialization commands")
	execContext := ChariotContainer.Use(containerPath, "/root", []ChariotContainer.Mount{}, nil, nil)
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

func main() {
	ChariotContainer.HostInit()

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("Chariot UNKNOWN-VERSION")
	} else {
		fmt.Printf("Chariot %s\n", buildInfo.Main.Version)
	}

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	configPath := flag.String("config", "chariot.toml", "Path to the config file")
	cachePath := flag.String("cache", filepath.Join(cwd, ".chariot-cache"), "Path to the cache directory")
	refetchSources := flag.Bool("refetch-sources", false, "Refetch all sources")
	resetContainer := flag.Bool("reset-container", false, "Create a new container")
	debugError := flag.Bool("debug-err", true, "Turn on error debugging")
	debugVerbose := flag.Bool("debug-verbose", false, "Turn on verbose debugging")
	threads := flag.Uint("threads", 8, "Number of simultaneous threads to use")
	flag.Parse()
	targets := flag.Args()

	config := ReadConfig(*configPath)

	context := ChariotContext{
		cwd:       cwd,
		cachePath: *cachePath,
		config:    config,
		options: ChariotOptions{
			threads:        *threads,
			debugError:     *debugError,
			debugVerbose:   *debugVerbose,
			refetchSources: *refetchSources,
			resetContainer: *resetContainer,
		},
		state: ChariotState{
			targets:      targets,
			targetStates: make(map[string]int),
		},
		cli: ChariotCLI.CreateCLI(os.Stdout),
	}

	if err := os.MkdirAll(context.cachePath, 0755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(filepath.Join(context.cachePath, "root"), 0755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(filepath.Join(context.cachePath, "sources"), 0755); err != nil {
		panic(err)
	}

	if context.options.resetContainer {
		context.wipeContainer()
	}
	if !FileExists(filepath.Join(context.cachePath, "container")) {
		context.initContainer()
	}

	for _, tag := range context.state.targets {
		context.do(tag)
	}
	context.cli.Println("DONE")
}

func (ctx *ChariotContext) recurseDeps(deps []string) bool {
	for _, dep := range deps {
		if !ctx.do(dep) {
			return false
		}
	}
	return true
}

func (ctx *ChariotContext) writers() (io.Writer, io.Writer) {
	var verboseWriter io.Writer = nil
	if ctx.options.debugVerbose {
		verboseWriter = ctx.cli.GetWriter(false, ChariotCLI.LIGHT_GRAY)
	}
	var errorWriter io.Writer = nil
	if ctx.options.debugError {
		errorWriter = ctx.cli.GetWriter(true, ChariotCLI.LIGHT_RED)
	}
	return verboseWriter, errorWriter
}

func (ctx *ChariotContext) do(tag string) (ok bool) {
	if ctx.state.targetStates[tag] == TARGET_STATE_FAILED {
		ctx.cli.Printf("Target %s failed previously\n", tag)
		return false
	}
	if ctx.state.targetStates[tag] != TARGET_STATE_NONE {
		return true
	}
	ctx.state.targetStates[tag] = TARGET_STATE_QUEUED
	defer func() {
		if ok {
			ctx.state.targetStates[tag] = TARGET_STATE_DONE
		} else {
			ctx.state.targetStates[tag] = TARGET_STATE_FAILED
		}
	}()

	tag, tagType := ParseTag(tag)
	if tagType != "" {
		switch tagType {
		case "host":
			target := ctx.config.FindHostTarget(tag)
			if target == nil {
				goto notFound
			}
			if !ctx.recurseDeps(target.Dependencies) {
				return false
			}
			return ctx.doTarget(true, tag, target)
		case "source":
			source := ctx.config.FindSource(tag)
			if source == nil {
				goto notFound
			}

			if !ctx.recurseDeps(source.Dependencies) {
				return false
			}

			modifierDeps := make([]string, 0)
			for _, modififer := range source.Modifiers {
				if modififer.Source == "" {
					continue
				}
				modifierDeps = append(modifierDeps, MakeTag(modififer.Source, "source"))
			}
			if !ctx.recurseDeps(modifierDeps) {
				return false
			}

			return ctx.doSource(tag, source)
		}
	} else {
		target := ctx.config.FindTarget(tag)
		if target == nil {
			goto notFound
		}
		if !ctx.recurseDeps(target.Dependencies) {
			return false
		}
		return ctx.doTarget(false, tag, target)
	}
	ctx.cli.Printf("Invalid tag %s. Skipping...\n", tag)
	return false
notFound:
	ctx.cli.Printf("Tag %s not found. Skipping...\n", tag)
	return false
}

func (ctx *ChariotContext) doSource(tag string, source *ConfigSource) (ok bool) {
	ctx.cli.Printf("SOURCE: %s\n", tag)
	sourcePath := filepath.Join(ctx.cachePath, "sources", tag)
	if FileExists(sourcePath) {
		if !ArrIncludes(ctx.state.targets, MakeTag(tag, "source")) {
			return true
		}
		if err := os.RemoveAll(sourcePath); err != nil {
			return false
		}
	}

	ctx.cli.StartSpinner("Initializing source %s", tag)
	defer ctx.cli.StopSpinner()

	if err := os.MkdirAll(sourcePath, 0755); err != nil {
		ctx.cli.Println(err)
		return false
	}
	defer func() {
		if !ok {
			os.RemoveAll(sourcePath)
		}
	}()

	ctx.cli.SetSpinnerMessage("Fetching source %s", tag)
	var cmd *exec.Cmd
	switch source.Type {
	case "tar":
		cmd = exec.Command("sh", "-c", fmt.Sprintf("wget -qO- %s | tar --strip-components 1 -xvz -C %s", source.Url, sourcePath))
	case "local":
		cmd = exec.Command("cp", "-r", "-T", source.Url, sourcePath)
	default:
		ctx.cli.Printf("WARNING: Source %s has an invalid type %s. Skipping...\n", tag, source.Type)
		return false
	}
	if err := cmd.Start(); err != nil {
		ctx.cli.Println(err)
		return false
	}
	if err := cmd.Wait(); err != nil {
		ctx.cli.Println(err)
		return false
	}

	ctx.cli.SetSpinnerMessage("Applying source modifications %s", tag)
	for _, modifier := range source.Modifiers {
		modSourcePath := filepath.Join(ctx.cachePath, "sources", modifier.Source)
		switch modifier.Type {
		case "patch":
			cmd = exec.Command("patch", "-p1", "-i", filepath.Join(modSourcePath, modifier.File))
		case "git-patch":
			cmd = exec.Command("git", "apply", filepath.Join(modSourcePath, modifier.File))
		case "merge":
			cmd = exec.Command("cp", "-r", fmt.Sprintf("%s/.", modSourcePath), ".")
		case "exec":
			verboseWriter, errorWriter := ctx.writers()
			execCtx := ChariotContainer.Use(filepath.Join(ctx.cachePath, "container"), "/chariot/source", []ChariotContainer.Mount{
				{To: "/chariot/sources", From: filepath.Join(ctx.cachePath, "sources")},
				{To: "/chariot/source", From: sourcePath},
			}, verboseWriter, errorWriter)

			cmd := modifier.Cmd
			for _, tag := range source.Dependencies {
				tag, tagType := ParseTag(tag)
				if tagType != "source" {
					continue
				}
				cmd = strings.ReplaceAll(cmd, fmt.Sprintf("$SOURCE:%s", tag), fmt.Sprintf("/chariot/sources/%s", tag))
			}
			cmd = strings.ReplaceAll(cmd, "$THREADS", fmt.Sprint(ctx.options.threads))

			if err := execCtx.Exec(cmd); err != nil {
				ctx.cli.Println(err)
				return false
			}
			continue
		default:
			ctx.cli.Printf("WARNING: Source %s has an invalid modifier of type %s. Skipping...\n", tag, modifier.Type)
			return false
		}
		cmd.Dir = sourcePath
		if err := cmd.Start(); err != nil {
			ctx.cli.Println(err)
			return false
		}
		if err := cmd.Wait(); err != nil {
			ctx.cli.Println(err)
			return false
		}
	}

	return true
}

func (ctx *ChariotContext) doTarget(host bool, tag string, target *ConfigTarget) (ok bool) {
	var buildDir string
	if host {
		ctx.cli.Printf("HOST TARGET: %s\n", tag)
		buildDir = filepath.Join(ctx.cachePath, "host-build", tag)
	} else {
		ctx.cli.Printf("TARGET: %s\n", tag)
		buildDir = filepath.Join(ctx.cachePath, "build", tag)
	}

	fullTag := tag
	if host {
		fullTag = MakeTag(tag, "host")
	}
	if FileExists(buildDir) {
		if !ArrIncludes(ctx.state.targets, fullTag) {
			return true
		}
		if err := os.RemoveAll(buildDir); err != nil {
			return false
		}
	}

	ctx.cli.StartSpinner("Initializing target %s", fullTag)
	defer ctx.cli.StopSpinner()

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		ctx.cli.Println(err)
		return false
	}
	defer func() {
		if !ok {
			os.RemoveAll(buildDir)
		}
	}()

	var installDir string
	if host {
		installDir = filepath.Join(ctx.cachePath, "root") // TODO: build out the host system
	} else {
		installDir = filepath.Join(ctx.cachePath, "root")
	}

	verboseWriter, errorWriter := ctx.writers()
	execContext := ChariotContainer.Use(filepath.Join(ctx.cachePath, "container"), "/chariot/build", []ChariotContainer.Mount{
		{To: "/chariot/root", From: filepath.Join(ctx.cachePath, "root")},
		{To: "/chariot/sources", From: filepath.Join(ctx.cachePath, "sources")},
		{To: "/chariot/install", From: installDir},
		{To: "/chariot/build", From: buildDir},
	}, verboseWriter, errorWriter)

	processCmd := func(cmd string) string {
		for _, tag := range target.Dependencies {
			tag, tagType := ParseTag(tag)
			if tagType != "source" {
				continue
			}
			cmd = strings.ReplaceAll(cmd, fmt.Sprintf("$SOURCE:%s", tag), fmt.Sprintf("/chariot/sources/%s", tag))
		}
		cmd = strings.ReplaceAll(cmd, "$ROOT", "/chariot/root")
		cmd = strings.ReplaceAll(cmd, "$BUILD", "/chariot/build")
		cmd = strings.ReplaceAll(cmd, "$INSTALL", "/chariot/install")
		cmd = strings.ReplaceAll(cmd, "$THREADS", fmt.Sprint(ctx.options.threads))
		return cmd
	}

	ctx.cli.SetSpinnerMessage("Configuring target %s", fullTag)
	for _, cmd := range target.Configure {
		if err := execContext.Exec(processCmd(cmd)); err != nil {
			return false
		}
	}
	ctx.cli.SetSpinnerMessage("Building target %s", fullTag)
	for _, cmd := range target.Build {
		if err := execContext.Exec(processCmd(cmd)); err != nil {
			return false
		}
	}
	ctx.cli.SetSpinnerMessage("Installing target %s", fullTag)
	for _, cmd := range target.Install {
		if err := execContext.Exec(processCmd(cmd)); err != nil {
			return false
		}
	}

	return true
}
