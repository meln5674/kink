package docker

import (
	"github.com/meln5674/kink/pkg/config/util"
)

type DockerFlags struct {
	Command []string `json:"command"`
	Context string   `json:"context"`
}

func (d *DockerFlags) Override(d2 *DockerFlags) {
	util.OverrideStringSlice(&d.Command, &d2.Command)
	util.OverrideString(&d.Context, &d2.Context)
}

func Save(d *DockerFlags, images ...string) []string {
	cmd := make([]string, len(d.Command))
	copy(cmd, d.Command)
	if d.Context != "" {
		cmd = append(cmd, "--context", d.Context)
	}
	cmd = append(cmd, "save")
	cmd = append(cmd, images...)
	return cmd
}
