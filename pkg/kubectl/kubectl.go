package kubectl

import (
	"fmt"
	"reflect"
	"strings"

	clientcmd "k8s.io/client-go/tools/clientcmd"
)

var (
	recommendedFlags = clientcmd.RecommendedConfigOverrideFlags("")
)

type KubectlFlags struct {
	Command []string
}

type KubeFlags struct {
	ConfigOverrides clientcmd.ConfigOverrides
	Kubeconfig      string
}

func addFlag(cmd *[]string, f *clientcmd.FlagInfo, value, zero interface{}, str string) {
	if reflect.DeepEqual(value, zero) {
		return
	}
	*cmd = append(*cmd, "--"+f.LongName)
	if str == "" {
		*cmd = append(*cmd, fmt.Sprintf("%s", value))
	} else {
		*cmd = append(*cmd, str)
	}
}

func (k *KubeFlags) Flags() []string {
	cmd := make([]string, 0)
	addFlag(
		&cmd,
		&recommendedFlags.AuthOverrideFlags.ClientCertificate,
		k.ConfigOverrides.AuthInfo.ClientCertificate,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.AuthOverrideFlags.ClientKey,
		k.ConfigOverrides.AuthInfo.ClientKey,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.AuthOverrideFlags.Token,
		k.ConfigOverrides.AuthInfo.Token,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.AuthOverrideFlags.Impersonate,
		k.ConfigOverrides.AuthInfo.Impersonate,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.AuthOverrideFlags.ImpersonateUID,
		k.ConfigOverrides.AuthInfo.ImpersonateUID,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.AuthOverrideFlags.ImpersonateGroups,
		k.ConfigOverrides.AuthInfo.ImpersonateGroups,
		[]string{},
		strings.Join(k.ConfigOverrides.AuthInfo.ImpersonateGroups, ","),
	)
	addFlag(
		&cmd,
		&recommendedFlags.AuthOverrideFlags.Username,
		k.ConfigOverrides.AuthInfo.Password,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.ClusterOverrideFlags.APIServer,
		k.ConfigOverrides.ClusterInfo.Server,
		"",
		"",
	)
	/*
		TODO: What flag is this? It doesn't appear to be used?
		addFlag(
			&cmd,
			&recommendedFlags.ClusterOverrideFlags.APIVersion,
			k.ConfigOverrides.ClusterInfo.APIVersion,
			"",
			"",
		)
	*/
	addFlag(
		&cmd,
		&recommendedFlags.ClusterOverrideFlags.CertificateAuthority,
		k.ConfigOverrides.ClusterInfo.CertificateAuthority,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.ClusterOverrideFlags.InsecureSkipTLSVerify,
		k.ConfigOverrides.ClusterInfo.InsecureSkipTLSVerify,
		false,
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.ClusterOverrideFlags.TLSServerName,
		k.ConfigOverrides.ClusterInfo.TLSServerName,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.ClusterOverrideFlags.ProxyURL,
		k.ConfigOverrides.ClusterInfo.ProxyURL,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.ContextOverrideFlags.ClusterName,
		k.ConfigOverrides.Context.Cluster,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.ContextOverrideFlags.AuthInfoName,
		k.ConfigOverrides.Context.AuthInfo,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.ContextOverrideFlags.Namespace,
		k.ConfigOverrides.Context.Namespace,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.CurrentContext,
		k.ConfigOverrides.CurrentContext,
		"",
		"",
	)
	addFlag(
		&cmd,
		&recommendedFlags.Timeout,
		k.ConfigOverrides.Timeout,
		"0",
		"",
	)
	if k.Kubeconfig != "" {
		cmd = append(cmd, "--kubeconfig", k.Kubeconfig)
	}
	return cmd
}

func GetPods(k *KubectlFlags, ku *KubeFlags, namespace string, labels map[string]string) []string {
	cmd := make([]string, len(k.Command))
	copy(cmd, k.Command)
	cmd = append(cmd, "get", "pod", "--output", "json")
	if namespace != "" {
		cmd = append(cmd, "--namespace", namespace)
	}
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
		cmd = append(cmd, "--selector", labelString.String())
	}
	return cmd
}

func ConfigCurrentContext(k *KubectlFlags, ku *KubeFlags) []string {
	cmd := make([]string, len(k.Command))
	copy(cmd, k.Command)
	cmd = append(cmd, "config", "current-context")

	return cmd
}

func ConfigGetContext(k *KubectlFlags, ku *KubeFlags, context string) []string {
	cmd := make([]string, len(k.Command))
	copy(cmd, k.Command)
	cmd = append(cmd, "config", "get", context, "--output", "json")

	return cmd
}

func PortForward(k *KubectlFlags, ku *KubeFlags, namespace, target string, mappings map[string]string) []string {
	cmd := make([]string, len(k.Command))
	copy(cmd, k.Command)
	cmd = append(cmd, "port-forward", target)

	if namespace != "" {
		cmd = append(cmd, "--namespace", namespace)
	}
	for local, remote := range mappings {
		cmd = append(cmd, fmt.Sprintf("%s:%s", local, remote))
	}
	return cmd
}

func Exec(k *KubectlFlags, ku *KubeFlags, namespace, target string, stdin, tty bool, exec ...string) []string {
	cmd := make([]string, len(k.Command))
	copy(cmd, k.Command)
	cmd = append(cmd, "exec", target)
	if namespace != "" {
		cmd = append(cmd, "--namespace", namespace)
	}
	if stdin {
		cmd = append(cmd, "--stdin")
	}
	if tty {
		cmd = append(cmd, "--tty")
	}
	cmd = append(cmd, "--")
	cmd = append(cmd, exec...)
	return cmd
}

func Cp(k *KubectlFlags, ku *KubeFlags, namespace, target, src, dest string) []string {
	cmd := make([]string, len(k.Command))
	copy(cmd, k.Command)
	cmd = append(cmd, "cp")
	if namespace != "" {
		cmd = append(cmd, "--namespace", namespace)
	}
	cmd = append(cmd, fmt.Sprintf("%s:%s", target, src), dest)
	return cmd
}

func Version(k *KubectlFlags, ku *KubeFlags) []string {
	cmd := make([]string, len(k.Command))
	copy(cmd, k.Command)
	cmd = append(cmd, "version")
	return cmd

}
