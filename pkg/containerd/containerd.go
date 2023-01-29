package containerd

import (
	"github.com/meln5674/kink/pkg/config/util"
	"github.com/meln5674/kink/pkg/flags"
)

type CtrFlags struct {
	Command   []string `json:"command"`
	Namespace string   `json:"namespace"`
	Address   string   `json:"address"`
}

func (c *CtrFlags) Override(c2 *CtrFlags) {
	util.Override(&c.Command, &c2.Command)
	util.Override(&c.Namespace, &c2.Namespace)
	util.Override(&c.Address, &c2.Address)
}

func (c *CtrFlags) Flags() map[string]string {
	flags := make(map[string]string)
	if c.Namespace != "" {
		flags["namespace"] = c.Namespace
	}
	if c.Address != "" {
		flags["address"] = c.Address
	}
	return flags
}

func (c *CtrFlags) Ctr(args ...string) []string {
	cmd := make([]string, 0, len(c.Command)+len(args))
	cmd = append(cmd, c.Command...)
	cmd = append(cmd, flags.AsFlags(c.Flags())...)
	cmd = append(cmd, args...)
	return cmd
}

func ImportImage(c *CtrFlags, image string) []string {
	return c.Ctr("image", "import", image)
}
