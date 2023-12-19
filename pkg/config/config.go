package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

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

// RawConfig is the configuration structure as provided by the user
type RawConfig struct {
	metav1.TypeMeta
	// Helm configures the `helm` commands used to manage the internal cluster
	Helm helm.HelmFlags `json:"helm"`
	// Kubectl configures the `kubectl` commands used to interact with the external cluster
	Kubectl kubectl.KubectlFlags `json:"kubectl"`
	// Kubernetes configures the connection to the external cluster
	Kubernetes kubectl.RawKubeFlags `json:"kubernetes"`
	// Docker configures the `docker` commands used to move images from a local daemon into the internal cluster
	Docker docker.DockerFlags `json:"docker"`
	// Chart configures the Helm Chart used to deploy the cluster
	Chart helm.ChartFlags `json:"chart"`
	// Release configures the Helm Release of the Chart that is used to deploy the cluster
	Release helm.ClusterReleaseFlags `json:"release"`
}

// Config is the formatted configuration as usable by the module
func (r *RawConfig) Format() Config {
	return Config{
		Helm:       r.Helm,
		Kubectl:    r.Kubectl,
		Kubernetes: r.Kubernetes.Format(),
		Docker:     r.Docker,
		Chart:      r.Chart,
		Release:    r.Release,
	}
}

// Config contains all of the necessary configuration to run the KinK CLI
type Config struct {
	Helm helm.HelmFlags
	// Kubectl configures the `kubectl` commands used to interact with the external cluster
	Kubectl kubectl.KubectlFlags
	// Kubernetes configures the connection to the external cluster
	Kubernetes kubectl.KubeFlags
	// Docker configures the `docker` commands used to move images from a local daemon into the internal cluster
	Docker docker.DockerFlags
	// Chart configures the Helm Chart used to deploy the cluster
	Chart helm.ChartFlags
	// Release configures the Helm Release of the Chart that is used to deploy the cluster
	Release helm.ClusterReleaseFlags
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

func (c *RawConfig) LoadFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bytes, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	err = yaml.Unmarshal(bytes, c)
	if err != nil {
		return err
	}
	validAPIVersion := false
	for _, version := range APIVersions {
		if c.APIVersion == version {
			validAPIVersion = true
			break
		}
	}
	if !validAPIVersion {
		return fmt.Errorf("Unsupported APIVersion %s, supported: %v", c.APIVersion, APIVersions)
	}
	if c.Kind != Kind {
		return fmt.Errorf("Unsupported Kind %s, must be %s", c.Kind, Kind)
	}
	return nil
}

// A StringMap is a map of strings to strings, but unmarshals from JSON by parsing a string, then re-parsing that string as JSON
type StringMap map[string]string

// UnmarshalJSON implements json.Unmarshaler
func (s *StringMap) UnmarshalJSON(bytes []byte) (err error) {
	var sJSON string
	err = json.Unmarshal(bytes, &sJSON)
	if err != nil {
		return
	}
	x := map[string]string{}
	err = json.Unmarshal([]byte(sJSON), &x)
	if err != nil {
		return err
	}
	*s = StringMap(x)
	return nil
}

type Int int

// An Int is a int that unmarshals from a JSON string
func (i *Int) UnmarshalJSON(bytes []byte) (err error) {
	var sJSON string
	err = json.Unmarshal(bytes, &sJSON)
	if err != nil {
		return
	}
	x := 0
	err = json.Unmarshal([]byte(sJSON), &x)
	if err != nil {
		return err
	}
	*i = Int(x)
	return nil
}

type Bool bool

// An Int is a int that unmarshals from a JSON string
func (b *Bool) UnmarshalJSON(bytes []byte) (err error) {
	var sJSON string
	err = json.Unmarshal(bytes, &sJSON)
	if err != nil {
		return
	}
	x := false
	err = json.Unmarshal([]byte(sJSON), &x)
	if err != nil {
		return err
	}
	*b = Bool(x)
	return nil
}

type LoadBalancerIngressNodePortClassMapping struct {
	Namespace string  `json:"namespace"`
	Name      string  `json:"name"`
	HttpPort  *string `json:"httpPort,omitempty"`
	HttpsPort *string `json:"httpsPort,omitempty"`
}

type LoadBalancerIngressHostPortClassMapping struct {
	HttpPort  *string `json:"httpPort,omitempty"`
	HttpsPort *string `json:"httpsPort,omitempty"`
}

type LoadBalancerIngressClassMapping struct {
	ClassName   string                                   `json:"className"`
	Annotations map[string]string                        `json:"annotations"`
	NodePort    *LoadBalancerIngressNodePortClassMapping `json:"nodePort,omitempty"`
	HostPort    *LoadBalancerIngressHostPortClassMapping `json:"hostPort,omitempty"`
}

func (l *LoadBalancerIngressClassMapping) Ports() (*string, *string) {
	if l.NodePort != nil {
		return l.NodePort.HttpPort, l.NodePort.HttpsPort
	}
	if l.HostPort != nil {
		return l.HostPort.HttpPort, l.HostPort.HttpsPort
	}
	return nil, nil
}

func (l *LoadBalancerIngressClassMapping) Port() (port string, isHttps bool) {
	httpPort, httpsPort := l.Ports()
	if httpsPort != nil {
		return *httpsPort, true
	}
	if httpPort != nil {
		return *httpPort, false
	}
	return "", false
}

type LoadBalancerIngressInner struct {
	Enabled                bool                                       `json:"enabled"`
	HostPortTargetFullname string                                     `json:"hostPortTargetFullname"`
	ClassMappings          map[string]LoadBalancerIngressClassMapping `json:"classMappings"`
}

type LoadBalancerIngress struct {
	LoadBalancerIngressInner
}

// UnmarshalJSON implements json.Unmarshaler
func (l *LoadBalancerIngress) UnmarshalJSON(bytes []byte) (err error) {
	var sJSON string
	err = json.Unmarshal(bytes, &sJSON)
	if err != nil {
		return
	}
	x := LoadBalancerIngressInner{}
	err = json.Unmarshal([]byte(sJSON), &x)
	if err != nil {
		return err
	}
	*l = LoadBalancerIngress{x}
	return nil
}

// ReleaseConfig are the values kept in the helm ConfigMap
type ReleaseConfig struct {
	Fullname                       string              `json:"fullname"`
	Labels                         StringMap           `json:"labels"`
	ControlplaneFullname           string              `json:"controlplane.fullname"`
	ControlplanePort               Int                 `json:"controlplane.port"`
	ControlplaneHostname           string              `json:"controlplane.hostname"`
	ControlplaneIsNodePort         Bool                `json:"controlplane.isNodePort"`
	ControlplaneLabels             StringMap           `json:"controlplane.labels"`
	ControlplaneSelectorLabels     StringMap           `json:"controlplane.selectorLabels"`
	SelectorLabels                 StringMap           `json:"selectorLabels"`
	WorkerFullname                 string              `json:"worker.fullname"`
	WorkerLabels                   StringMap           `json:"worker.labels"`
	WorkerSelectorLabels           StringMap           `json:"worker.selectorLabels"`
	LoadBalancerFullname           string              `json:"load-balancer.fullname"`
	LoadBalancerLabels             StringMap           `json:"load-balancer.labels"`
	LoadBalancerSelectorLabels     StringMap           `json:"load-balancer.selectorLabels"`
	LoadBalancerServiceAnnotations StringMap           `json:"load-balancer.service.annotations"`
	LoadBalancerIngress            LoadBalancerIngress `json:"load-balancer.ingress"`
	LBManagerFullname              string              `json:"lb-manager.fullname"`
	FileGatewayEnabled             Bool                `json:"file-gateway.enabled"`
	FileGatewayHostname            string              `json:"file-gateway.hostname"`
	FileGatewayContainerPort       Int                 `json:"file-gateway.containerPort"`
	RKE2Enabled                    Bool                `json:"rke2.enabled"`
}

func loadMap(path string) (map[string]string, error) {
	valueJSON, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	value := make(map[string]string)
	err = json.Unmarshal(valueJSON, &value)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (r *ReleaseConfig) LoadFromMount(mount string) error {
	f, err := os.Open(filepath.Join(mount, "config.json"))
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(r)
}

func (r *ReleaseConfig) LoadFromMap(data map[string]interface{}) (err error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	return json.Unmarshal(bytes, r)
}

func (r *ReleaseConfig) LoadFromConfigMap(cm *corev1.ConfigMap) (ok bool, err error) {
	configJSON, ok := cm.Data["config.json"]
	if !ok {
		return
	}
	err = json.Unmarshal([]byte(configJSON), r)
	return
}
