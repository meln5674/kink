package helm

import (
	"fmt"
	"strings"

	"k8s.io/klog/v2"

	"github.com/meln5674/kink/pkg/config/util"
	"github.com/meln5674/kink/pkg/flags"
	"github.com/meln5674/kink/pkg/kubectl"
)

const (
	ClusterLabel     = "kink.meln5674.github.com/cluster"
	ClusterNodeLabel = "kink.meln5674.github.com/cluster-node"
	ReleasePrefix    = "kink-"
)

var (
	kubeFlagTranslation = map[string]string{
		"server":                "kube-apiserver",
		"as-group":              "kube-as-group",
		"as":                    "kube-as-user",
		"certificate-authority": "kube-ca-file",
		"context":               "kube-context",
		"token":                 "kube-token",
	}

	kubeFlagDrop = []string{
		"request-timeout",
		"--log-file",
		"--log-file-max-size",
		"--logtostderr",
		"--match-server-version",
	}
)

func KubeHelmFlags(ku *kubectl.KubeFlags) map[string]string {
	flags := ku.Flags()
	// fmt.Println(flags)
	for _, key := range kubeFlagDrop {
		_, ok := flags[key]
		if ok {
			klog.Warningf("Ignoring unsupported helm flag from kubectl: %s", key)
			delete(flags, key)
		}
	}
	// fmt.Println(flags)
	for kubeFlag, helmFlag := range kubeFlagTranslation {
		value, ok := flags[kubeFlag]
		if ok {
			flags[helmFlag] = value
			delete(flags, kubeFlag)
		}
	}
	// fmt.Println(flags)
	return flags
}

func IsKinkRelease(name string) bool {
	return strings.HasPrefix(name, ReleasePrefix)
}

func GetReleaseClusterName(release string) (string, bool) {
	if !IsKinkRelease(release) {
		return "", false
	}

	return strings.TrimPrefix(release, ReleasePrefix), true
}

type HelmFlags struct {
	Command []string `json:"command"`
}

func (h *HelmFlags) Override(h2 *HelmFlags) {
	util.Override(&h.Command, &h2.Command)
}

func (h *HelmFlags) Helm(k *kubectl.KubeFlags, args ...string) []string {
	cmd := make([]string, 0, len(h.Command)+len(args))

	cmd = append(cmd, h.Command...)
	cmd = append(cmd, flags.AsFlags(KubeHelmFlags(k))...)
	cmd = append(cmd, args...)

	return cmd
}

type ChartFlags struct {
	RepositoryURL string `json:"repositoryURL"`
	ChartName     string `json:"chart"`
	Version       string `json:"version"`
}

func (c *ChartFlags) Override(c2 *ChartFlags) {
	util.Override(&c.RepositoryURL, &c2.RepositoryURL)
	util.Override(&c.ChartName, &c2.ChartName)
	util.Override(&c.Version, &c2.Version)
}

func (c *ChartFlags) IsOCIChart() bool {
	return strings.HasPrefix(c.ChartName, "oci://")
}

func (c *ChartFlags) IsLocalChart() bool {
	return !c.IsOCIChart() && (strings.HasPrefix(c.ChartName, "./") || strings.HasPrefix(c.ChartName, "../") || strings.HasPrefix(c.ChartName, "/"))
}

func (c *ChartFlags) RepoName() string {
	name := c.RepositoryURL
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	return name
}

func (c *ChartFlags) FullChartName() string {
	if c.IsOCIChart() || c.IsLocalChart() {
		return c.ChartName
	} else {
		return fmt.Sprintf("%s/%s", c.RepoName(), c.ChartName)
	}
}

func (c *ChartFlags) UpgradeFlags() []string {
	cmd := make([]string, 0)
	if c.Version != "" {
		cmd = append(cmd, "--version", c.Version)
	}
	return cmd
}

type ClusterReleaseFlags struct {
	ClusterName  string            `json:"clusterName"`
	Values       []string          `json:"values"`
	Set          map[string]string `json:"set"`
	SetString    map[string]string `json:"setString"`
	UpgradeFlags []string          `json:"upgradeFlags"`
}

func (f *ClusterReleaseFlags) Raw() ReleaseFlags {
	return ReleaseFlags{
		Name:         fmt.Sprintf("kink-%s", f.ClusterName),
		Values:       f.Values,
		Set:          f.Set,
		SetString:    f.SetString,
		UpgradeFlags: f.UpgradeFlags,
	}
}

type ReleaseFlags struct {
	Name         string            `json:"name"`
	Values       []string          `json:"values"`
	Set          map[string]string `json:"set"`
	SetString    map[string]string `json:"setString"`
	UpgradeFlags []string          `json:"upgradeFlags"`
}

func (r *ReleaseFlags) ValuesFlags() []string {
	flags := make([]string, 0)
	for _, values := range r.Values {
		flags = append(flags, "--values", values)
	}
	for k, v := range r.Set {
		flags = append(flags, "--set", fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range r.SetString {
		flags = append(flags, "--set-string", fmt.Sprintf("%s=%s", k, v))
	}
	return flags
}

func (r *ClusterReleaseFlags) Override(r2 *ClusterReleaseFlags) {
	util.Override(&r.ClusterName, &r2.ClusterName)
	util.Override(&r.Values, &r2.Values)
	//fmt.Printf("%#v\n", r.Set)
	//fmt.Printf("%#v\n", r2.Set)
	util.Override(&r.Set, &r2.Set)
	//fmt.Printf("%#v\n", r.Set)
	util.Override(&r.SetString, &r2.SetString)
	util.Override(&r.UpgradeFlags, &r2.UpgradeFlags)
}

func RepoAdd(h *HelmFlags, c *ChartFlags) []string {
	return h.Helm(&kubectl.KubeFlags{}, "repo", "add", c.RepoName(), c.RepositoryURL)
}

func RepoUpdate(h *HelmFlags, repoNames ...string) []string {
	args := make([]string, 0, 2+len(repoNames))
	args = append(args, "repo", "update")
	args = append(args, repoNames...)

	return h.Helm(&kubectl.KubeFlags{}, args...)
}

func Upgrade(h *HelmFlags, c *ChartFlags, r *ReleaseFlags, k *kubectl.KubeFlags) []string {
	args := make([]string, 0)
	args = append(args, "upgrade", "--install", "--wait", r.Name, c.FullChartName())
	args = append(args, c.UpgradeFlags()...)
	args = append(args, r.ValuesFlags()...)
	args = append(args, r.UpgradeFlags...)
	return h.Helm(k, args...)
}

func UpgradeCluster(h *HelmFlags, c *ChartFlags, r *ClusterReleaseFlags, k *kubectl.KubeFlags) []string {
	raw := r.Raw()
	return Upgrade(h, c, &raw, k)
}

func Template(h *HelmFlags, c *ChartFlags, r *ReleaseFlags, k *kubectl.KubeFlags) []string {
	args := make([]string, 0)
	args = append(args, "template", r.Name, c.FullChartName())
	args = append(args, c.UpgradeFlags()...)
	args = append(args, r.ValuesFlags()...)
	return h.Helm(k, args...)
}

func TemplateCluster(h *HelmFlags, c *ChartFlags, r *ClusterReleaseFlags, k *kubectl.KubeFlags) []string {
	raw := r.Raw()
	return Template(h, c, &raw, k)
}

func Delete(h *HelmFlags, c *ChartFlags, r *ReleaseFlags, k *kubectl.KubeFlags) []string {
	return h.Helm(k, "delete", r.Name)
}

func List(h *HelmFlags, k *kubectl.KubeFlags) []string {
	return h.Helm(k, "list", "--output", "json", "--all")
}
