package docker

type DockerFlags struct {
	Command []string
	Context string
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
