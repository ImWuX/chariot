package chariot_container

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/docker/docker/pkg/reexec"
)

type ExecContext struct {
	containerPath string
	cwd           string
	mounts        []Mount
	stdOut        io.Writer
	stdErr        io.Writer
}

type Mount struct {
	To   string
	From string
}

func HostInit() {
	reexec.Register("container_init", containerEntry)
	if reexec.Init() {
		os.Exit(0)
	}
}

func Exec(containerPath string, cmd string, cwd string, mounts []Mount, stdOut io.Writer, stdErr io.Writer, stdIn io.Reader) error {
	var strs []string = make([]string, 0)
	for _, mount := range mounts {
		strs = append(strs, mount.To+":"+mount.From)
	}

	proc := reexec.Command("container_init", containerPath, cmd, strings.Join(strs, "::"), cwd)
	if stdOut != nil {
		proc.Stdout = stdOut
	}
	if stdErr != nil {
		proc.Stderr = stdErr
	}
	if stdIn != nil {
		proc.Stdin = stdIn
	}
	proc.Env = []string{
		"LANG=en_US.UTF-8",
		"LC_COLLATE=C",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/bin:/usr/bin/core_perl",
	}
	proc.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWUSER,
		UidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Geteuid(), Size: 1},
		},
		GidMappings: []syscall.SysProcIDMap{
			{ContainerID: 0, HostID: os.Getegid(), Size: 1},
		},
	}

	if err := proc.Start(); err != nil {
		return err
	}
	return proc.Wait()
}

func Use(containerPath string, cwd string, mounts []Mount, stdOut io.Writer, stdErr io.Writer) *ExecContext {
	var context ExecContext
	context.containerPath = containerPath
	context.cwd = cwd
	context.mounts = mounts
	context.stdOut = stdOut
	context.stdErr = stdErr
	return &context
}

func (context *ExecContext) Exec(cmd string) error {
	return Exec(context.containerPath, cmd, context.cwd, context.mounts, context.stdOut, context.stdErr, nil)
}

func containerEntry() {
	root := os.Args[1]
	command := os.Args[2]
	mounts := os.Args[3]
	cwd := os.Args[4]

	isolate(root, mounts)

	cmd := exec.Command("sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Dir = cwd

	if err := cmd.Start(); err != nil {
		fmt.Printf("exec error: %s\n", err)
	} else {
		cmd.Wait()
	}
}

func isolate(rootPath string, mounts string) {
	const PIVOT_CACHE = "/.temp-pivot"

	mountFs := func(src string, dest string, fstype string, flags uintptr) {
		if err := os.MkdirAll(dest, 0755); err != nil {
			panic(err)
		}
		if err := syscall.Mount(src, dest, fstype, flags, ""); err != nil {
			panic(err)
		}
	}

	mountNewFs := func(dir string, fstype string) {
		mountFs("", filepath.Join(rootPath, dir), fstype, 0)
	}

	mountFile := func(file string) {
		if new, err := os.Create(filepath.Join(rootPath, file)); err != nil {
			panic(err)
		} else {
			if err := new.Close(); err != nil {
				panic(err)
			}
		}
		if err := syscall.Mount(file, filepath.Join(rootPath, file), "", syscall.MS_BIND, ""); err != nil {
			panic(err)
		}
	}

	mountFs("/dev", filepath.Join(rootPath, "dev"), "", syscall.MS_REC|syscall.MS_BIND|syscall.MS_SLAVE)
	mountFile("/etc/resolv.conf")
	mountNewFs(filepath.Join("dev", "pts"), "devpts")
	mountNewFs(filepath.Join("dev", "shm"), "tmpfs")
	mountNewFs("tmp", "tmpfs")
	mountNewFs("run", "tmpfs")
	mountNewFs("proc", "proc")

	for _, mount := range strings.Split(mounts, "::") {
		toFrom := strings.Split(mount, ":")
		if len(toFrom) < 2 {
			continue
		}
		mountFs(toFrom[1], filepath.Join(rootPath, toFrom[0]), "", syscall.MS_BIND)
	}

	if err := syscall.Mount(rootPath, rootPath, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		panic(err)
	}

	old := filepath.Join(rootPath, PIVOT_CACHE)
	if err := os.MkdirAll(old, 0700); err != nil {
		panic(err)
	}

	if err := syscall.PivotRoot(rootPath, old); err != nil {
		panic(err)
	}

	if err := os.Chdir("/"); err != nil {
		panic(err)
	}

	if err := syscall.Unmount(PIVOT_CACHE, syscall.MNT_DETACH); err != nil {
		panic(err)
	}

	if err := os.RemoveAll(PIVOT_CACHE); err != nil {
		panic(err)
	}
}
