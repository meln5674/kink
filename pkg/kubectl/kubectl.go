package kubectl

import (
	"fmt"
	"strings"
)

type KubectlFlags struct {
	Command []string
}

type KubeFlags struct {
	// TODO
}

func (k *KubeFlags) Flags() []string {
	return []string{} // TODO
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
