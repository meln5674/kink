package containerd

import (
	"github.com/meln5674/kink/pkg/config/util"
)

type CtrFlags struct {
	Command   []string `json:"command"`
	Namespace string   `json:"namespace"`
	Address   string   `json:"address"`
}

func (c *CtrFlags) Override(c2 *CtrFlags) {
	util.OverrideStringSlice(&c.Command, &c2.Command)
	util.OverrideString(&c.Namespace, &c2.Namespace)
	util.OverrideString(&c.Address, &c2.Address)
}

func ImportImage(c *CtrFlags, image string) []string {
	cmd := make([]string, len(c.Command))
	copy(cmd, c.Command)
	if c.Namespace != "" {
		cmd = append(cmd, "--namespace", c.Namespace)
	}
	if c.Address != "" {
		cmd = append(cmd, "--address", c.Address)
	}
	cmd = append(cmd, "image", "import", image)
	return cmd
}
