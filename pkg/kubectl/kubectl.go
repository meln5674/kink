package kubectl

import (
	"fmt"
	"reflect"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/meln5674/kink/pkg/config/util"
	"github.com/meln5674/kink/pkg/flags"
)

var (
	recommendedFlags = clientcmd.RecommendedConfigOverrideFlags("")
)

type KubectlFlags struct {
	Command []string
}

func (k *KubectlFlags) Override(k2 *KubectlFlags) {
	util.Override(&k.Command, &k2.Command)
}

type KubeFlags struct {
	ConfigOverrides clientcmd.ConfigOverrides
	Kubeconfig      string
}

func (k *KubeFlags) Override(k2 *KubeFlags) {
	util.Override(&k.Kubeconfig, &k2.Kubeconfig)
	util.Override(&k.ConfigOverrides, &k2.ConfigOverrides)
}

func Kubectl(k *KubectlFlags, ku *KubeFlags, args ...string) []string {
	cmd := make([]string, 0, len(k.Command))
	cmd = append(cmd, k.Command...)
	cmd = append(cmd, flags.AsFlags(ku.Flags())...)
	cmd = append(cmd, args...)
	return cmd
}

func isNillable(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Chan, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return true
	default:
		return false
	}
}

func isNillableNil(v reflect.Value) bool {
	return isNillable(v) && v.IsNil()
}

func addFlag(flags map[string]string, f *clientcmd.FlagInfo, value, zero interface{}, str string) {
	if value == nil || isNillableNil(reflect.ValueOf(value)) || reflect.DeepEqual(value, zero) {
		return
	}
	klog.Infof("%s=%#v (%#v)", f.LongName, value, zero)
	if str == "" {
		flags[f.LongName] = fmt.Sprintf("%s", value)
	} else {
		flags[f.LongName] = str
	}
}

func (k *KubeFlags) Flags() map[string]string {
	flags := make(map[string]string)
	addFlag(
		flags,
		&recommendedFlags.AuthOverrideFlags.ClientCertificate,
		k.ConfigOverrides.AuthInfo.ClientCertificate,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.AuthOverrideFlags.ClientKey,
		k.ConfigOverrides.AuthInfo.ClientKey,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.AuthOverrideFlags.Token,
		k.ConfigOverrides.AuthInfo.Token,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.AuthOverrideFlags.Impersonate,
		k.ConfigOverrides.AuthInfo.Impersonate,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.AuthOverrideFlags.ImpersonateUID,
		k.ConfigOverrides.AuthInfo.ImpersonateUID,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.AuthOverrideFlags.ImpersonateGroups,
		k.ConfigOverrides.AuthInfo.ImpersonateGroups,
		//[]string(nil),
		[]string{},
		strings.Join(k.ConfigOverrides.AuthInfo.ImpersonateGroups, ","),
	)
	addFlag(
		flags,
		&recommendedFlags.AuthOverrideFlags.Username,
		k.ConfigOverrides.AuthInfo.Password,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.ClusterOverrideFlags.APIServer,
		k.ConfigOverrides.ClusterInfo.Server,
		"",
		"",
	)
	/*
		TODO: What flag is this? It doesn't appear to be used?
		addFlag(
			flags,
			&recommendedFlags.ClusterOverrideFlags.APIVersion,
			k.ConfigOverrides.ClusterInfo.APIVersion,
			"",
			"",
		)
	*/
	addFlag(
		flags,
		&recommendedFlags.ClusterOverrideFlags.CertificateAuthority,
		k.ConfigOverrides.ClusterInfo.CertificateAuthority,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.ClusterOverrideFlags.InsecureSkipTLSVerify,
		k.ConfigOverrides.ClusterInfo.InsecureSkipTLSVerify,
		false,
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.ClusterOverrideFlags.TLSServerName,
		k.ConfigOverrides.ClusterInfo.TLSServerName,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.ClusterOverrideFlags.ProxyURL,
		k.ConfigOverrides.ClusterInfo.ProxyURL,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.ContextOverrideFlags.ClusterName,
		k.ConfigOverrides.Context.Cluster,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.ContextOverrideFlags.AuthInfoName,
		k.ConfigOverrides.Context.AuthInfo,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.ContextOverrideFlags.Namespace,
		k.ConfigOverrides.Context.Namespace,
		"",
		"",
	)
	addFlag(
		flags,
		&recommendedFlags.CurrentContext,
		k.ConfigOverrides.CurrentContext,
		"",
		"",
	)
	// Special case, both empty string and zero mean no timeout
	var timeout = k.ConfigOverrides.Timeout
	if timeout == "" {
		timeout = "0"
	}
	addFlag(
		flags,
		&recommendedFlags.Timeout,
		timeout,
		"0",
		"",
	)
	if k.Kubeconfig != "" {
		flags["kubeconfig"] = k.Kubeconfig
	}
	return flags
}

func GetPods(k *KubectlFlags, ku *KubeFlags, labels map[string]string) []string {
	args := make([]string, 0, 4+len(labels)*2)
	args = append(args, "get", "pod", "--output", "json")
	if len(labels) != 0 {
		labelString := strings.Builder{}
		first := true
		for k, v := range labels {
			if !first {
				labelString.WriteString(",")
			}
			labelString.WriteString(k)
			labelString.WriteString("=")
			labelString.WriteString(v)
			first = false
		}
		args = append(args, "--selector", labelString.String())
	}
	return Kubectl(k, ku, args...)
}

func WatchPods(k *KubectlFlags, ku *KubeFlags, labels map[string]string, allNamespaces bool) []string {
	args := make([]string, 0, 3+len(labels)*2+1)
	args = append(args, "get", "pod", "--watch")
	if len(labels) != 0 {
		labelString := strings.Builder{}
		first := true
		for k, v := range labels {
			if !first {
				labelString.WriteString(",")
			}
			labelString.WriteString(k)
			labelString.WriteString("=")
			labelString.WriteString(v)
			first = false
		}
		args = append(args, "--selector", labelString.String())
	}
	if allNamespaces {
		args = append(args, "--all-namespaces")
	}
	return Kubectl(k, ku, args...)
}

func ConfigCurrentContext(k *KubectlFlags, ku *KubeFlags) []string {
	return Kubectl(k, ku, "config", "current-context")
}

func ConfigGetContext(k *KubectlFlags, ku *KubeFlags, context string) []string {
	return Kubectl(k, ku, "config", "get", context, "--output", "json")
}

func PortForward(k *KubectlFlags, ku *KubeFlags, target string, mappings map[string]string) []string {
	args := make([]string, 0, 2+len(mappings))
	args = append(args, "port-forward", target)

	for local, remote := range mappings {
		args = append(args, fmt.Sprintf("%s:%s", local, remote))
	}
	return Kubectl(k, ku, args...)
}

func Exec(k *KubectlFlags, ku *KubeFlags, target string, stdin, tty bool, exec ...string) []string {
	args := make([]string, 0, 4+len(exec))
	args = append(args, "exec", target)
	if stdin {
		args = append(args, "--stdin")
	}
	if tty {
		args = append(args, "--tty")
	}
	args = append(args, "--")
	args = append(args, exec...)
	return Kubectl(k, ku, args...)
}

func Cp(k *KubectlFlags, ku *KubeFlags, target, src, dest string) []string {
	return Kubectl(k, ku, "cp", fmt.Sprintf("%s:%s", target, src), dest)
}

func Version(k *KubectlFlags, ku *KubeFlags) []string {
	return Kubectl(k, ku, "version")
}

func ConfigSetCluster(k *KubectlFlags, ku *KubeFlags, cluster string, data map[string]string) []string {
	args := make([]string, 0, 3+len(data))
	args = append(args, "config", "set-cluster", cluster)
	for k, v := range data {
		args = append(args, fmt.Sprintf("--%s=%s", k, v))
	}
	return Kubectl(k, ku, args...)
}
