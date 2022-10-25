package config

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/meln5674/kink/pkg/docker"
	"github.com/meln5674/kink/pkg/helm"
	"github.com/meln5674/kink/pkg/kubectl"
)

const (
	Kind               = "Config"
	DefaultClusterName = "kink"
)

var (
	APIVersions = []string{"kink.meln5674.github.com/v0"}
)

// Config contains all of the necessary configuration to run the KinK CLI
type Config struct {
	metav1.TypeMeta
	// Helm configures the `helm` commands used to manage the internal cluster
	Helm helm.HelmFlags `json:"helm"`
	// Kubectl configures the `kubectl` commands used to interact with the external cluster
	Kubectl kubectl.KubectlFlags `json:"kubectl"`
	// Kubernetes configures the connection to the external cluster
	Kubernetes kubectl.KubeFlags `json:"kubernetes"`
	// Docker configures the `docker` commands used to move images from a local daemon into the internal cluster
	Docker docker.DockerFlags `json:"docker"`
	// Chart configures the Helm Chart used to deploy the cluster
	Chart helm.ChartFlags `json:"chart"`
	// Release configures the Helm Release of the Chart that is used to deploy the cluster
	Release helm.ReleaseFlags `json:"release"`
}

// Overrides sets any non-zero fields from another config in this one
func (c *Config) Override(c2 *Config) {
	c.Helm.Override(&c2.Helm)
	c.Kubectl.Override(&c2.Kubectl)
	c.Kubernetes.Override(&c2.Kubernetes)
	c.Docker.Override(&c2.Docker)
	c.Chart.Override(&c2.Chart)
	c.Release.Override(&c2.Release)
}
