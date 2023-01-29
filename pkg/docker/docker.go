package docker

import (
	"github.com/meln5674/kink/pkg/config/util"
	"github.com/meln5674/kink/pkg/flags"
)

type DockerFlags struct {
	Command []string `json:"command"`
	Context string   `json:"context"`
}

func (d *DockerFlags) Override(d2 *DockerFlags) {
	util.Override(&d.Command, &d2.Command)
	util.Override(&d.Context, &d2.Context)
}

func (d *DockerFlags) Flags() map[string]string {
	flags := make(map[string]string)
	if d.Context != "" {
		flags["context"] = d.Context
	}
	return flags
}

func (d *DockerFlags) Docker(args ...string) []string {
	cmd := make([]string, len(d.Command))
	copy(cmd, d.Command)
	cmd = append(cmd, flags.AsFlags(d.Flags())...)
	cmd = append(cmd, args...)
	return cmd
}

func Save(d *DockerFlags, images ...string) []string {
	args := make([]string, 0, len(images)+1)
	args = append(args, "save")
	args = append(args, images...)
	return d.Docker(args...)
}

func Pull(d *DockerFlags, image string) []string {
	return d.Docker("pull", image)
}

func Build(d *DockerFlags, tag, dir string, flags ...string) []string {
	// This is really only used in the E2E tests, don't depend on it
	args := make([]string, 0, len(flags)+4)
	args = append(args, "build", "-t", tag)
	args = append(args, flags...)
	args = append(args, dir)
	return d.Docker(args...)
}

func Run(d *DockerFlags, flags []string, image string, cmd ...string) []string {
	args := make([]string, 0, len(flags)+len(cmd)+2)
	args = append(args, "run")
	args = append(args, flags...)
	args = append(args, image)
	args = append(args, cmd...)
	return d.Docker(args...)
}

func Exec(d *DockerFlags, containerID string, flags []string, cmd ...string) []string {
	// This is really only used in the E2E tests, don't depend on it
	args := make([]string, 0, len(flags)+len(cmd)+2)
	args = append(args, "exec")
	args = append(args, flags...)
	args = append(args, containerID)
	args = append(args, cmd...)
	return d.Docker(args...)

}
