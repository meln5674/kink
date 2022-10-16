package helm

import (
	"fmt"
	"github.com/meln5674/kink/pkg/kubectl"
	"strings"
)

const (
	ClusterLabel = "kind.meln5674.github.com/cluster"
)

func IsKinkRelease(name string) bool {
	return strings.HasPrefix(name, "kink-")
}

type HelmFlags struct {
	Command []string
}

type ChartFlags struct {
	RepositoryURL string
	ChartName     string
	Version       string
}

func (c *ChartFlags) IsLocalChart() bool {
	return strings.HasPrefix(c.ChartName, "./")
}

func (c *ChartFlags) RepoName() string {
	return strings.ReplaceAll(c.RepositoryURL, "/", "-")
}

func (c *ChartFlags) FullChartName() string {
	if c.IsLocalChart() {
		return c.ChartName
	} else {
		return fmt.Sprintf("%s/%s", c.RepoName(), c.ChartName)
	}
}

type ReleaseFlags struct {
	Namespace   string
	ClusterName string
	Values      []string
	Set         []string
}

func (r *ReleaseFlags) ReleaseName() string {
	return fmt.Sprintf("kink-%s", r.ClusterName)
}

func (r *ReleaseFlags) ExtraLabels() map[string]string {
	return map[string]string{
		ClusterLabel: r.ClusterName,
	}
}

func (r *ReleaseFlags) ExtraLabelFlags() []string {
	cmd := []string{}
	for k, v := range r.ExtraLabels() {
		for _, component := range []string{"worker", "controlplane"} {
			cmd = append(cmd, "--set", fmt.Sprintf("%s.extraLabels.%s=%s", component, strings.ReplaceAll(strings.ReplaceAll(k, ",", `\,`), ".", `\.`), v))
		}
	}
	return cmd
}

func RepoAdd(h *HelmFlags, c *ChartFlags, r *ReleaseFlags) []string {
	cmd := make([]string, len(h.Command))
	copy(cmd, h.Command)
	cmd = append(cmd, "repo", "add", c.RepoName(), c.RepositoryURL)

	return cmd
}

func Upgrade(h *HelmFlags, c *ChartFlags, r *ReleaseFlags, k *kubectl.KubeFlags) []string {
	cmd := make([]string, len(h.Command))
	copy(cmd, h.Command)

	cmd = append(cmd, "upgrade", "--install", "--wait", r.ReleaseName(), c.FullChartName())
	if c.Version != "" {
		cmd = append(cmd, "--version", c.ChartName)
	}
	if r.Namespace != "" {
		cmd = append(cmd, "--namespace", r.Namespace)
	}
	for _, values := range r.Values {
		cmd = append(cmd, "--values", values)
	}
	for _, set := range r.Set {
		cmd = append(cmd, "--set", set)
	}
	cmd = append(cmd, r.ExtraLabelFlags()...)
	cmd = append(cmd, k.Flags()...)
	return cmd
}

func Delete(h *HelmFlags, c *ChartFlags, r *ReleaseFlags, k *kubectl.KubeFlags) []string {
	cmd := make([]string, len(h.Command))
	copy(cmd, h.Command)
	cmd = append(cmd, "delete", r.ReleaseName())
	if r.Namespace != "" {
		cmd = append(cmd, "--namespace", r.Namespace)
	}
	cmd = append(cmd, k.Flags()...)
	return cmd
}

func List(h *HelmFlags, c *ChartFlags, r *ReleaseFlags, k *kubectl.KubeFlags) []string {
	cmd := make([]string, len(h.Command))
	copy(cmd, h.Command)

	cmd = append(cmd, "list", "--output", "json")
	cmd = append(cmd, k.Flags()...)

	return cmd
}
