/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/meln5674/rflag"
	"github.com/spf13/cobra"

	helmctlv1 "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	"github.com/rancher/wharfie/pkg/registries"

	etcdtypes "go.etcd.io/etcd/api/v3/etcdserverpb"
	etcd "go.etcd.io/etcd/client/v3"

	"k8s.io/apimachinery/pkg/util/yaml"
	yamlwriter "sigs.k8s.io/yaml"

	"k8s.io/klog/v2"
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a KinK container",
	Long:  `This command is used to initialize a container running a KinK controlplane or worker, you should not use it outside of one`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInit(context.Background(), &initArgs)
	},
}

type initEtcdArgsT struct {
	ResetMember    bool   `rflag:"usage=If present and node is an existing etcd member,, update it with the new IP"`
	MemberNamePath string `rflag:"usage=Path to file containing etcd member name"`
	ConfigPath     string `rflag:"usage=Path to etcd config file to mutate"`
	Endpoint       string `rflag:"usage=Endpoint to connect to etcd cluster"`
}

type initPodArgsT struct {
	IP string `rflag:"usage=IP of the pod this is executing in"`
}

type initExtraManifestsArgsT struct {
	SystemPath string `rflag:"usage=Directory containing system charts"`
	UserPath   string `rflag:"usage=Directory containing user charts"`
	Path       string `rflag:"usage=Directory to copy system and user chart manifests to"`
}

type initLocalPathProvisionerArgsT struct {
	ManifestPath string `rflag:"usage=Path to local-path-provisioner manifest to mutate"`
	ChartPath    string `rflag:"usage=Path to local-path-provisioner chart tarball to inject into manifest"`
}

type initRegistriesArgsT struct {
	TemplatePath        string   `rflag:"usage=Path containing template for registries.yaml"`
	Path                string   `rflag:"usage=Path to write instantiated template for registries.yaml"`
	CredentialsRootPath string   `rflag:"usage=Path containing credential subdirs for registries"`
	TLSMounted          []string `rflag:"usage=List of registries that have mounted tls,slice-type=array"`
	AuthMounted         []string `rflag:"usage=List of registries that have mounted auth,slice-type=array"`
}

type initKubeletArgsT struct {
	CertPath string `rflag:"usage=Path to kubelet cert file to delete"`
	KeyPath  string `rflag:"usage=Path to kubelet key file to delete"`
}

type initArgsT struct {
	Etcd                 initEtcdArgsT                 `rflag:"prefix=etcd-"`
	Pod                  initPodArgsT                  `rflag:"prefix=pod-"`
	ExtraManifests       initExtraManifestsArgsT       `rflag:"prefix=extra-manifests-"`
	LocalPathProvisioner initLocalPathProvisionerArgsT `rflag:"prefix=local-path-provisioner-"`
	Registries           initRegistriesArgsT           `rflag:"prefix=registries-"`
	Kubelet              initKubeletArgsT              `rflag:"prefix=kubelet-"`
	IsControlPlane       bool                          `rflag:"usage=If present,, initialize as control plane"`
}

var initArgs initArgsT

func init() {
	rootCmd.AddCommand(initCmd)
	rflag.MustRegister(rflag.ForPFlag(initCmd.Flags()), "", &initArgs)
}

func runInit(ctx context.Context, args *initArgsT) error {
	var err error
	if args.IsControlPlane {
		err = resetEtcdMember(ctx, &args.Etcd, &args.Pod)
		if err != nil {
			return err
		}
		err = copyExtraManifests(ctx, &args.ExtraManifests)
		if err != nil {
			return err
		}
		err = mutateLocalPathProvisionerManifest(ctx, &args.LocalPathProvisioner)
		if err != nil {
			return err
		}
	}
	// When a pod is re-created, the kubelet certs remain, but the pod IP changes, invalidating the cert.
	// This means that kubelet logs, kubectl port-forward, etc, cease to function.
	// Deleting the certificate and key file forces k3s/rke2 to generate a new one.
	err = resetKubeletCerts(ctx, &args.Kubelet)
	if err != nil {
		return err
	}
	err = generateRegistriesConfig(ctx, &args.Registries)
	if err != nil {
		return err
	}

	return nil
}

func resetEtcdMember(ctx context.Context, args *initEtcdArgsT, pod *initPodArgsT) error {
	if !args.ResetMember {
		return nil
	}
	configF, err := os.Open(args.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		klog.InfoS("Etcd file doesn't exist, assuming first run, not resetting member", "path", args.ConfigPath)
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "Failed to open etcd config at path %s", args.ConfigPath)
	}
	var config ETCDConfig
	err = yaml.NewYAMLOrJSONDecoder(configF, 1024).Decode(&config)
	if err != nil {
		return err
	}

	memberName, err := os.ReadFile(args.MemberNamePath)

	yc := config.ServerTrust
	tlscfg := tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if yc.TrustedCAFile != "" {
		tlscfg.RootCAs = x509.NewCertPool()
		caPEM, err := os.ReadFile(yc.TrustedCAFile)
		if err != nil {
			return err
		}
		tlscfg.RootCAs.AppendCertsFromPEM(caPEM)
	}

	if yc.CertFile != "" && yc.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(yc.CertFile, yc.KeyFile)
		if err != nil {
			return err
		}
		tlscfg.Certificates = []tls.Certificate{cert}
	}

	etcdCfg := etcd.Config{
		Endpoints: []string{args.Endpoint},
		TLS:       &tlscfg,
	}
	etcdClient, err := etcd.New(etcdCfg)
	if err != nil {
		return errors.Wrap(err, "Failed to build etcd client")
	}
	members, err := etcdClient.MemberList(ctx)
	if err != nil {
		return errors.Wrap(err, "Failed to list etcd members")
	}
	var member *etcdtypes.Member
	for ix := range members.Members {
		if members.Members[ix].Name == string(memberName) {
			member = members.Members[ix]
			break
		}
	}
	if member == nil {
		klog.InfoS("No etcd member matched name, assuming not part of the cluster, not resetting member", "name", memberName, "members", members.Members)
		return nil
	}
	_, err = etcdClient.MemberUpdate(ctx, member.ID, []string{fmt.Sprintf("https://%s:2380", pod.IP)})
	if err != nil {
		return errors.Wrap(err, "Failed to update etcd member to new pod IP")
	}
	return nil
}

func copyFile(dest, src string) error {
	destF, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destF.Close()
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()
	_, err = io.Copy(destF, srcF)
	if err != nil {
		return err
	}
	return nil
}

func copyDirRecursive(dest, src string, dirMode os.FileMode) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if entry.IsDir() {
			return nil
		}
		if err != nil {
			return err
		}
		entrySuffix, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		entryDest := filepath.Join(dest, entrySuffix)
		err = os.MkdirAll(filepath.Dir(entryDest), dirMode)
		if err != nil {
			return err
		}
		err = copyFile(entryDest, path)
		if err != nil {
			return err
		}
		return nil
	})
}

func copyExtraManifests(ctx context.Context, args *initExtraManifestsArgsT) error {
	err := copyDirRecursive(args.Path, args.SystemPath, 0700)
	if err != nil {
		return err
	}
	err = copyDirRecursive(args.Path, args.UserPath, 0700)
	if err != nil {
		return err
	}
	return nil
}

func mutateLocalPathProvisionerManifest(ctx context.Context, args *initLocalPathProvisionerArgsT) error {
	manifestF, err := os.Open(args.ManifestPath)
	if err != nil {
		return err
	}
	defer manifestF.Close()
	var manifest helmctlv1.HelmChart
	err = yaml.NewYAMLOrJSONDecoder(manifestF, 1024).Decode(&manifest)
	if err != nil {
		return err
	}
	manifestF.Close()
	chartF, err := os.Open(args.ChartPath)
	if err != nil {
		return err
	}
	defer chartF.Close()
	var buf bytes.Buffer
	encodedChartF := base64.NewEncoder(base64.StdEncoding, &buf)
	_, err = io.Copy(encodedChartF, chartF)
	if err != nil {
		return err
	}
	manifest.Spec.ChartContent = buf.String()
	manifestBytes, err := yamlwriter.Marshal(&manifest)
	if err != nil {
		return err
	}
	err = os.WriteFile(args.ManifestPath, manifestBytes, 0600)
	if err != nil {
		return err
	}
	return nil
}

func hasString(haystack []string, needle string) bool {
	for _, straw := range haystack {
		if straw == needle {
			return true
		}
	}
	return false
}

func replaceRegistryAuthField(field *string, path string) error {
	if *field == "" {
		return nil
	}
	val, err := os.ReadFile(filepath.Join(path, *field))
	if err != nil {
		return err
	}
	*field = string(val)
	return nil
}

func replaceRegistryAuth(name string, auth *registries.AuthConfig, path string, args *initRegistriesArgsT) error {
	if auth == nil {
		return nil
	}
	if !hasString(args.AuthMounted, name) {
		return nil
	}
	klog.V(1).InfoS("Replacing auth", "name", name, "auth", auth, "path", path, "mounted", args.AuthMounted)
	authDir := filepath.Join(path, "auth")
	fields := []*string{&auth.Username, &auth.Password, &auth.Auth, &auth.IdentityToken}
	for _, field := range fields {
		err := replaceRegistryAuthField(field, authDir)
		if err != nil {
			return err
		}
		klog.V(1).InfoS("Replacing auth", "name", name, "auth", auth, "path", path, "mounted", args.AuthMounted)
	}

	return nil
}

func replaceRegistryTLSField(field *string, path string) error {
	if *field == "" {
		return nil
	}
	*field = filepath.Join(path, *field)
	return nil
}

func replaceRegistryTLS(name string, tls *registries.TLSConfig, path string, args *initRegistriesArgsT) error {
	if tls == nil {
		return nil
	}
	if !hasString(args.TLSMounted, name) {
		return nil
	}
	klog.V(1).InfoS("Replacing tls", "name", name, "tls", tls, "path", path, "mounted", args.TLSMounted)
	tlsDir := filepath.Join(path, "tls")
	fields := []*string{&tls.CAFile, &tls.CertFile, &tls.KeyFile}
	for _, field := range fields {
		err := replaceRegistryTLSField(field, tlsDir)
		if err != nil {
			return err
		}
		klog.V(1).InfoS("Replacing tls", "name", name, "tls", tls, "path", path, "mounted", args.TLSMounted)
	}

	return nil
}

func resetKubeletCerts(ctx context.Context, args *initKubeletArgsT) error {
	err := os.Remove(args.CertPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	err = os.Remove(args.KeyPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func generateRegistriesConfig(ctx context.Context, args *initRegistriesArgsT) error {
	templateF, err := os.Open(args.TemplatePath)
	if err != nil {
		return err
	}
	var config registries.Registry
	err = yaml.NewYAMLOrJSONDecoder(templateF, 1024).Decode(&config)
	if err != nil {
		return err
	}
	for name, registry := range config.Configs {
		regDir := filepath.Join(args.CredentialsRootPath, name)
		err = replaceRegistryAuth(name, registry.Auth, regDir, args)
		if err != nil {
			return err
		}
		err = replaceRegistryTLS(name, registry.TLS, regDir, args)
		if err != nil {
			return err
		}
	}
	klog.InfoS("Generated registries.yaml", "registries", config)
	configBytes, err := yamlwriter.Marshal(&config)
	if err != nil {
		return err
	}
	err = os.WriteFile(args.Path, configBytes, 0600)
	if err != nil {
		return err
	}
	return nil
}

// EtcdConfig is copied from https://github.com/k3s-io/k3s/blob/102ff763287bd9b1346f394f945cf448ea570b4f/pkg/daemons/executor/executor.go#L53
// because k3s can't be imported
type ETCDConfig struct {
	InitialOptions                  `json:",inline"`
	Name                            string      `json:"name,omitempty"`
	ListenClientURLs                string      `json:"listen-client-urls,omitempty"`
	ListenClientHTTPURLs            string      `json:"listen-client-http-urls,omitempty"`
	ListenMetricsURLs               string      `json:"listen-metrics-urls,omitempty"`
	ListenPeerURLs                  string      `json:"listen-peer-urls,omitempty"`
	AdvertiseClientURLs             string      `json:"advertise-client-urls,omitempty"`
	DataDir                         string      `json:"data-dir,omitempty"`
	SnapshotCount                   int         `json:"snapshot-count,omitempty"`
	ServerTrust                     ServerTrust `json:"client-transport-security"`
	PeerTrust                       PeerTrust   `json:"peer-transport-security"`
	ForceNewCluster                 bool        `json:"force-new-cluster,omitempty"`
	HeartbeatInterval               int         `json:"heartbeat-interval"`
	ElectionTimeout                 int         `json:"election-timeout"`
	Logger                          string      `json:"logger"`
	LogOutputs                      []string    `json:"log-outputs"`
	ExperimentalInitialCorruptCheck bool        `json:"experimental-initial-corrupt-check"`
}

type ServerTrust struct {
	CertFile       string `json:"cert-file"`
	KeyFile        string `json:"key-file"`
	ClientCertAuth bool   `json:"client-cert-auth"`
	TrustedCAFile  string `json:"trusted-ca-file"`
}

type PeerTrust struct {
	CertFile       string `json:"cert-file"`
	KeyFile        string `json:"key-file"`
	ClientCertAuth bool   `json:"client-cert-auth"`
	TrustedCAFile  string `json:"trusted-ca-file"`
}

type InitialOptions struct {
	AdvertisePeerURL string `json:"initial-advertise-peer-urls,omitempty"`
	Cluster          string `json:"initial-cluster,omitempty"`
	State            string `json:"initial-cluster-state,omitempty"`
}
