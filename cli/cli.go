package chariot_cli

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/briandowns/spinner"
)

const (
	BLACK         = "\033[30m"
	RED           = "\033[31m"
	GREEN         = "\033[32m"
	YELLOW        = "\033[33m"
	BLUE          = "\033[34m"
	MAGENT        = "\033[35m"
	CYAN          = "\033[36m"
	LIGHT_GRAY    = "\033[37m"
	LIGHT_RED     = "\033[91m"
	LIGHT_GREEN   = "\033[92m"
	LIGHT_YELLOW  = "\033[93m"
	LIGHT_BLUE    = "\033[94m"
	LIGHT_MAGENTA = "\033[95m"
	LIGHT_CYAN    = "\033[96m"
	LIGHT_WHITE   = "\033[97m"
	RESET         = "\033[0m"
)

type CLI struct {
	lock sync.Mutex
	buf  []byte
	out  *io.Writer

	doSpin  bool
	spinner *spinner.Spinner
}

type CLIWriter struct {
	cli   *CLI
	err   bool
	color string
}

func (writer *CLIWriter) Write(buf []byte) (int, error) {
	return writer.cli.write(buf, writer.color)
}

func CreateCLI(out io.Writer) *CLI {
	cli := &CLI{
		buf:     make([]byte, 0),
		out:     &out,
		doSpin:  false,
		spinner: spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithColor("yellow"), spinner.WithWriter(out)),
	}
	return cli
}

func (cli *CLI) write(buf []byte, color string) (int, error) {
	cli.lock.Lock()
	cli.buf = append(cli.buf, buf...)
	last := -1
	for i, b := range cli.buf {
		if b != '\n' {
			continue
		}
		last = i
	}
	if last >= 0 {
		if cli.doSpin {
			cli.spinner.Stop()
		}
		if color != "" {
			(*cli.out).Write([]byte(color))
		}
		(*cli.out).Write(cli.buf[:last+1])
		cli.buf = cli.buf[last+1:]
		if color != "" {
			(*cli.out).Write([]byte(RESET))
		}
		if cli.doSpin {
			cli.spinner.Start()
		}
	}
	cli.lock.Unlock()
	return len(buf), nil
}

func (cli *CLI) GetWriter(err bool, color string) *CLIWriter {
	return &CLIWriter{
		cli:   cli,
		err:   err,
		color: color,
	}
}

func (cli *CLI) StartSpinner(format string, a ...any) {
	cli.doSpin = true
	cli.SetSpinnerMessage(format, a...)
	cli.spinner.Start()
}

func (cli *CLI) SetSpinnerMessage(format string, a ...any) {
	cli.spinner.Suffix = fmt.Sprintf(" %s", fmt.Sprintf(format, a...))
}

func (cli *CLI) StopSpinner() {
	cli.doSpin = false
	cli.spinner.Stop()
}

func (cli *CLI) Printf(format string, a ...any) {
	cli.write([]byte(fmt.Sprintf(format, a...)), "")
}

func (cli *CLI) Println(a ...any) {
	cli.write([]byte(fmt.Sprintln(a...)), "")
}
