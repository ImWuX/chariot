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
	"time"

	"github.com/briandowns/spinner"
	ChariotContainer "github.com/imwux/chariot/container"
)

type ChariotContext struct {
	cachePath string
	config    Config

	optThreads        uint
	optDebugError     bool
	optDebugVerbose   bool
	optRefetchSources bool
	optResetContainer bool
}

type SpinnerWriter struct {
	buf     []byte
	spinner *spinner.Spinner
}

func (w *SpinnerWriter) Write(buf []byte) (int, error) {
	w.buf = append(w.buf, buf...)
	last := -1
	for i, b := range w.buf {
		if b != '\n' {
			continue
		}
		last = i
	}
	if last >= 0 {
		w.spinner.Stop()
		os.Stdout.Write([]byte("\033[37m"))
		os.Stdout.Write(w.buf[:last+1])
		w.buf = w.buf[last+1:]
		os.Stdout.Write([]byte("\033[0m"))
		w.spinner.Start()
	}
	return len(buf), nil
}

func createSpinner() *spinner.Spinner {
	spinner := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	spinner.Color("yellow")
	return spinner
}

func (ctx *ChariotContext) wipeContainer() {
	s := createSpinner()
	s.Suffix = " Deleting container"
	s.FinalMSG = "Container deleted\n"
	s.Start()
	defer s.Stop()

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
	s := createSpinner()
	s.Suffix = " Initializing container"
	s.FinalMSG = "Container initialized\n"
	s.Start()
	defer s.Stop()

	containerPath := filepath.Join(ctx.cachePath, "container")

	if _, err := os.Stat(filepath.Join(ctx.cachePath, "archlinux-bootstrap-x86_64.tar.gz")); err != nil {
		s.Suffix = " Downloading arch linux image"
		cmd := exec.Command("wget", "https://geo.mirror.pkgbuild.com/iso/latest/archlinux-bootstrap-x86_64.tar.gz")
		cmd.Dir = ctx.cachePath
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		if err := cmd.Wait(); err != nil {
			panic(err)
		}
	}

	s.Suffix = " Extracting arch linux image"
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

	s.Suffix = " Rewriting container permissions"
	cmd = exec.Command("sh", "-c", "for f in $(find ./container -perm 000 2> /dev/null); do chmod 755 \"$f\"; done")
	cmd.Dir = ctx.cachePath
	if err := cmd.Start(); err != nil {
		panic(err)
	}
	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	s.Suffix = " Running initialization commands"
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
	execContext.Exec("pacman --noconfirm -S ninja meson git wget perl diffutils inetutils python help2man bison flex gettext libtool m4 make patch texinfo which")
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
		cachePath:         *cachePath,
		config:            config,
		optThreads:        *threads,
		optDebugError:     *debugError,
		optDebugVerbose:   *debugVerbose,
		optRefetchSources: *refetchSources,
		optResetContainer: *resetContainer,
	}

	if err := os.MkdirAll(context.cachePath, 0755); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(filepath.Join(context.cachePath, "root"), 0755); err != nil {
		panic(err)
	}

	if context.optResetContainer {
		context.wipeContainer()
	}
	if _, err := os.Stat(filepath.Join(context.cachePath, "container")); err != nil {
		context.initContainer()
	}

	for _, tag := range targets {
		context.doTarget(tag)
	}
	fmt.Println("Done")
}

func (ctx *ChariotContext) doTarget(tag string) {
	target := ctx.config.FindTarget(tag)
	if target == nil {
		fmt.Printf("WARNING: Could not locate target %s. Skipping...\n", tag)
		return
	}

	for _, sourceTag := range target.Sources {
		ctx.doSource(sourceTag)
	}

	fmt.Printf("Target: %s\n", tag)

	s := createSpinner()
	s.Suffix = fmt.Sprintf(" Initializing target %s", tag)
	s.Start()
	defer s.Stop()
	writer := SpinnerWriter{spinner: s, buf: make([]byte, 0)}

	buildDir := filepath.Join(ctx.cachePath, "build", tag)
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		panic(err)
	}

	var errWriter io.Writer
	if ctx.optDebugError {
		errWriter = &writer
	}
	var verboseWriter io.Writer
	if ctx.optDebugVerbose {
		verboseWriter = &writer
	}
	execContext := ChariotContainer.Use(filepath.Join(ctx.cachePath, "container"), "/chariot/build", []ChariotContainer.Mount{
		{To: "/chariot/sources", From: filepath.Join(ctx.cachePath, "sources")},
		{To: "/chariot/build", From: buildDir},
	}, verboseWriter, errWriter)

	processCmd := func(cmd string) string {
		for _, tag := range target.Sources {
			cmd = strings.ReplaceAll(cmd, fmt.Sprintf("$SOURCE:%s", tag), fmt.Sprintf("/chariot/sources/%s", tag))
		}
		cmd = strings.ReplaceAll(cmd, "$BUILD", "/chariot/build")
		cmd = strings.ReplaceAll(cmd, "$ROOT", "/chariot/root")
		cmd = strings.ReplaceAll(cmd, "$THREADS", fmt.Sprint(ctx.optThreads))

		return cmd
	}

	s.Suffix = fmt.Sprintf(" Configuring target %s", tag)
	for _, cmd := range target.Configure {
		execContext.Exec(processCmd(cmd))
	}
	s.Suffix = fmt.Sprintf(" Building target %s", tag)
	for _, cmd := range target.Build {
		execContext.Exec(processCmd(cmd))
	}
	s.Suffix = fmt.Sprintf(" Installing target %s", tag)
	for _, cmd := range target.Install {
		execContext.Exec(processCmd(cmd))
	}
}

func (ctx *ChariotContext) doSource(tag string) {
	source := ctx.config.FindSource(tag)
	if source == nil {
		fmt.Printf("WARNING: Could not locate source %s. Skipping...\n", tag)
		return
	}

	fmt.Printf("Source: %s\n", tag)

	sourceDir := filepath.Join(ctx.cachePath, "sources", tag)

	if _, err := os.Stat(sourceDir); err == nil {
		if ctx.optRefetchSources {
			if err := os.RemoveAll(sourceDir); err != nil {
				panic(err)
			}
		} else {
			return
		}
	}

	s := createSpinner()
	s.Suffix = fmt.Sprintf(" Fetching source %s", tag)
	s.Start()
	defer s.Stop()

	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		panic(err)
	}

	switch source.Type {
	case "tar":
		cmd := exec.Command("sh", "-c", fmt.Sprintf("wget -qO- %s | tar xvz -C %s", source.Url, sourceDir))
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		if err := cmd.Wait(); err != nil {
			panic(err)
		}
	case "git":
		cmd := exec.Command("git", "clone", source.Url, sourceDir)
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		if err := cmd.Wait(); err != nil {
			panic(err)
		}
	case "local":
		cmd := exec.Command("cp", "-r", source.Url, sourceDir)
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		if err := cmd.Wait(); err != nil {
			panic(err)
		}
	default:
		s.Stop()
		fmt.Printf("WARNING: Source %s has an invalid type (%s). Skipping...\n", tag, source.Type)
		return
	}
}
