package command

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
)

type PipeProcessor func(io.Reader) error

type Commander interface {
	Start() error
	Wait() error
	Run() error
	Kill() error
}

type MultiProcessError struct {
	Errors []error
}

func (e *MultiProcessError) Error() string {
	msg := strings.Builder{}
	msg.WriteString("One or more processes failed: (")
	for _, err := range e.Errors {
		msg.WriteString(err.Error())
		msg.WriteString(", ")
	}
	msg.WriteString(")")
	return msg.String()
}

func FanOut(parallelCount int, cmds ...Commander) error {
	log.Println(parallelCount, len(cmds))
	cmdChan := make(chan Commander)
	errChan := make(chan error)
	sem := make(chan struct{})
	go func() {
		for _, cmd := range cmds {
			cmdChan <- cmd
		}
		close(cmdChan)
		log.Println("all commands pushed")
	}()
	for ix := 0; ix < parallelCount; ix++ {
		go func() {
			log.Println("fanout started")
			defer func() {
				sem <- struct{}{}
				log.Println("sem++")
				log.Println("fanout finished")
			}()
			for cmd := range cmdChan {
				errChan <- cmd.Run()
				log.Println("Wrote err")
			}
		}()
	}
	go func() {
		for ix := 0; ix < parallelCount; ix++ {
			_ = <-sem
			log.Println("sem--")
		}
		log.Println("all fanouts finished")
		close(errChan)
	}()
	errs := make([]error, 0, len(cmds))
	for err := range errChan {
		log.Println("Read err")
		if err != nil {
			errs = append(errs, err)
		}
	}
	log.Println("all errors recorded")
	if len(errs) != 0 {
		return &MultiProcessError{Errors: errs}
	}
	return nil
}

type Cmd struct {
	*exec.Cmd
	HandleStdout    PipeProcessor
	HandleStdoutErr chan error
	Closers         []io.Closer
}

func Command(ctx context.Context, cmd ...string) *Cmd {
	return &Cmd{Cmd: exec.CommandContext(ctx, cmd[0], cmd[1:]...), Closers: make([]io.Closer, 0)}
}

func (c *Cmd) ForwardAll() *Cmd {
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c
}

func (c *Cmd) ForwardOutErr() *Cmd {
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c
}

func (c *Cmd) ForwardErr() *Cmd {
	c.Stderr = os.Stderr
	return c
}

func (c *Cmd) ProcessOut(handler PipeProcessor) *Cmd {
	c.HandleStdout = handler
	c.HandleStdoutErr = make(chan error)
	return c
}

func (c *Cmd) StringIn(in string) *Cmd {
	c.Stdin = strings.NewReader(in)
	return c
}

func (c *Cmd) FileIn(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	c.Cmd.Stdin = f
	c.Closers = append(c.Closers, f)
	return nil
}

func (c *Cmd) FileOut(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	c.Cmd.Stdout = f
	c.Closers = append(c.Closers, f)
	return nil
}

func (c *Cmd) FileErr(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	c.Cmd.Stderr = f
	c.Closers = append(c.Closers, f)
	return nil
}

func (c *Cmd) WithParentEnv() *Cmd {
	c.Env = make([]string, len(os.Environ()))
	copy(c.Env, os.Environ())
	return c
}

func (c *Cmd) WithEnv(env map[string]string) *Cmd {
	envIndex := make(map[string]int, len(c.Cmd.Env))
	for ix, envLine := range c.Cmd.Env {
		split := strings.SplitN(envLine, "=", 2)
		envIndex[split[0]] = ix
	}
	for k, v := range env {
		line := fmt.Sprintf("%s=%s", k, v)
		ix, exists := envIndex[k]
		if exists {
			c.Cmd.Env[ix] = line
		} else {
			c.Cmd.Env = append(c.Cmd.Env, line)
		}
	}
	return c
}

func (c *Cmd) startStdoutProcessor() error {
	if c.HandleStdout != nil {
		stdout, err := c.Cmd.StdoutPipe()
		if err != nil {
			return err
		}
		go func() {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				err, ok := r.(error)
				if ok {
					c.HandleStdoutErr <- err
				} else {
					c.HandleStdoutErr <- fmt.Errorf("panicked: %#v", r)
				}
				close(c.HandleStdoutErr)
			}()
			c.HandleStdoutErr <- c.HandleStdout(stdout)
		}()
	}
	return nil
}

func (c *Cmd) Run() error {
	defer func() {
		for _, closer := range c.Closers {
			closer.Close()
		}
	}()
	err := c.startStdoutProcessor()
	if err != nil {
		return err
	}
	log.Println(c.Path, c.Args)
	err = c.Cmd.Run()
	log.Println("exited", c.Path, c.Args)
	if err != nil {
		return err
	}
	if c.HandleStdoutErr == nil {
		return nil
	}
	err = <-c.HandleStdoutErr
	if err != nil {
		return err
	}
	return nil
}

func (c *Cmd) Start() error {
	err := c.startStdoutProcessor()
	if err != nil {
		return err
	}
	log.Println(c.Path, c.Args, "&")
	return c.Cmd.Start()
}

func (c *Cmd) Wait() error {
	defer func() {
		for _, closer := range c.Closers {
			closer.Close()
		}
	}()

	log.Println("waiting", c.Path, c.Args)
	err := c.Cmd.Wait()
	log.Println("exited", c.Path, c.Args)
	if err != nil {
		return err
	}
	if c.HandleStdoutErr == nil {
		return nil
	}
	err = <-c.HandleStdoutErr
	if err != nil {
		return err
	}
	return nil
}

func (c *Cmd) Kill() error {
	return c.Cmd.Process.Kill()
}

type Pipeline struct {
	Cmds []*Cmd
}

func NewPipeline(cmd ...*Cmd) (*Pipeline, error) {
	if len(cmd) < 2 {
		return nil, fmt.Errorf("Need at least two commands for a pipeline")
	}
	/*
		stdout, err := cmd[0].StdoutPipe()
		if err != nil {
			return nil, err
		}
	*/
	var err error
	prevCmd := cmd[0]
	for _, nextCmd := range cmd[1:] {
		// TODO: Does this need to get cleaned up somehow?
		nextCmd.Stdin, prevCmd.Stdout, err = os.Pipe()
		// stdout, err = nextCmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
	}
	return &Pipeline{Cmds: cmd}, nil
}

func (p *Pipeline) ForwardErr() *Pipeline {
	for _, cmd := range p.Cmds {
		cmd.ForwardErr()
	}
	return p
}

func (p *Pipeline) Run() error {
	err := p.Start()
	if err != nil {
		return err
	}
	err = p.Wait()
	log.Println("pipeline finished")
	if err != nil {
		return err
	}
	return nil
}

func (p *Pipeline) Start() error {
	for ix, cmd := range p.Cmds {
		err := cmd.Start()
		if err != nil {
			for ix2 := 0; ix2 < ix; ix2++ {
				p.Cmds[ix2].Process.Kill()
			}
			return err
		}
	}
	return nil
}

func (p *Pipeline) Wait() error {
	errs := make([]error, 0, len(p.Cmds))
	dead := false
	// Iterating in reverse because if a downstream process, its writer may be blocking forever on its stdout
	// If that's the case, then we attempt to kill each process upstream from that because there's no point in letting them finish
	for ix := len(p.Cmds) - 1; ix >= 0; ix-- {
		cmd := p.Cmds[ix]
		err := cmd.Wait()
		if err != nil {
			errs = append(errs, err)
			if !dead {
				dead = true
				for ix2 := ix - 1; ix2 >= 0; ix-- {
					// We don't care about the error, if this fails, then we'll be deadlocked anyways
					p.Cmds[ix2].Process.Kill()
				}
			}
		}
	}
	if len(errs) != 0 {
		return &MultiProcessError{Errors: errs}
	}
	return nil
}

func (p *Pipeline) Kill() error {
	errs := make([]error, 0, len(p.Cmds))
	for _, cmd := range p.Cmds {
		err := cmd.Kill()
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) != 0 {
		return &MultiProcessError{Errors: errs}
	}
	return nil
}
