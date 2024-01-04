package e2e_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	gtypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/meln5674/gosh"

	"github.com/meln5674/kink/pkg/docker"
	"github.com/meln5674/kink/pkg/flags"
	"github.com/meln5674/kink/pkg/helm"
	"github.com/meln5674/kink/pkg/kubectl"

	"github.com/meln5674/k8s-smoke-test/pkg/test"
	k8ssmoketest "github.com/meln5674/k8s-smoke-test/pkg/test"
)

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2e Suite")
}

var (
	// These 4 are used when debugging interactively. They should be set to false in the actual repo
	noSuiteCleanup = false
	noCleanup      = false
	noCreate       = false
	noPull         = false
	noLoad         = false
	noDeps         = false
	noRebuild      = false

	kindConfigPath           = "../integration-test/kind.config.yaml"
	kindKubeconfigPath       = "../integration-test/kind.kubeconfig"
	rootedKindKubeconfigPath = "integration-test/kind.kubeconfig"

	kindOpts = KindOpts{
		KindCommand:       []string{"../bin/kind"},
		KubeconfigOutPath: kindKubeconfigPath,
		ClusterName:       "kink-it",
	}
	dockerOpts = docker.DockerFlags{
		Command: []string{"docker"},
	}
	kubectlOpts = kubectl.KubectlFlags{
		Command: []string{"../bin/kubectl"},
	}
	kindKubeOpts = kubectl.KubeFlags{
		Kubeconfig: kindOpts.KubeconfigOutPath,
	}
	rootedKindKubeOpts = kubectl.KubeFlags{
		Kubeconfig: rootedKindKubeconfigPath,
	}
	helmOpts = helm.HelmFlags{
		Command: []string{"../bin/helm"},
	}

	sharedLocalPathProvisionerMount = "/opt/shared-local-path-provisioner"

	ingressNginxChartRepo    = "https://kubernetes.github.io/ingress-nginx"
	ingressNginxChartName    = "ingress-nginx"
	ingressNginxChartVersion = "4.4.2"

	k8sSmokeTestVersion = "v0.2.0"
	k8sSmokeTestChart   = helm.ChartFlags{
		ChartName: "oci://ghcr.io/meln5674/k8s-smoke-test/charts/k8s-smoke-test",
		Version:   k8sSmokeTestVersion,
	}
	k8sSmokeTestDeploymentImageRepo  = "ghcr.io/meln5674/k8s-smoke-test/deployment"
	k8sSmokeTestDeploymentImage      = k8sSmokeTestDeploymentImageRepo + ":" + k8sSmokeTestVersion
	k8sSmokeTestStatefulSetImageRepo = "ghcr.io/meln5674/k8s-smoke-test/statefulset"
	k8sSmokeTestStatefulSetImage     = k8sSmokeTestStatefulSetImageRepo + ":" + k8sSmokeTestVersion
	k8sSmokeTestJobImageRepo         = "ghcr.io/meln5674/k8s-smoke-test/job"
	k8sSmokeTestJobImage             = k8sSmokeTestJobImageRepo + ":" + k8sSmokeTestVersion

	k8sSmokeTestDeploymentTarballPath = "../integration-test/k8smoke-test-deployment.tar"
	k8sSmokeTestJobTarballPath        = "../integration-test/k8smoke-test-job.tar"

	defaultTag       = "it"
	beforeSuiteState suiteState
)

type suiteState struct {
	ImageRepo    string
	ImageTag     string
	DefaultImage string
	BuiltImage   string
}

var _ = SynchronizedBeforeSuite(beforeSuiteGlobal, beforeSuiteLocal)

func beforeSuiteGlobal() []byte {

	BuildImage()
	if !noPull {
		FetchK8sSmokeTestImages()
	}
	InitKindCluster()

	podWatch := gosh.
		Command(kubectl.WatchPods(&kubectlOpts, &kindKubeOpts, nil, true)...).
		WithStreams(GinkgoOutErr)
	ExpectStart(podWatch)
	DeferCleanup(func() {
		ExpectStop(podWatch)
	})
	ingressWatch := gosh.
		Command(kubectl.Kubectl(&kubectlOpts, &kindKubeOpts, "get", "ingress", "-A", "-w")...).
		WithStreams(GinkgoOutErr)
	ExpectStart(ingressWatch)
	DeferCleanup(func() {
		ExpectStop(ingressWatch)
	})
	serviceWatch := gosh.
		Command(kubectl.Kubectl(&kubectlOpts, &kindKubeOpts, "get", "service", "-A", "-w")...).
		WithStreams(GinkgoOutErr)
	ExpectStart(serviceWatch)
	DeferCleanup(func() {
		ExpectStop(serviceWatch)
	})
	endpointWatch := gosh.
		Command(kubectl.Kubectl(&kubectlOpts, &kindKubeOpts, "get", "endpoints", "-A", "-w")...).
		WithStreams(GinkgoOutErr)
	ExpectStart(endpointWatch)
	DeferCleanup(func() {
		ExpectStop(endpointWatch)
	})

	beforeSuiteStateJSON, err := json.Marshal(&beforeSuiteState)
	Expect(err).ToNot(HaveOccurred())

	return beforeSuiteStateJSON
}

func beforeSuiteLocal(beforeSuiteStateJSON []byte) {
	gosh.GlobalLog = GinkgoLogr
	gosh.CommandLogLevel = 0

	klog.InitFlags(flag.CommandLine)
	if _, gconfig := GinkgoConfiguration(); gconfig.Verbosity().GTE(gtypes.VerbosityLevelVerbose) {
		flag.Set("v", "11")
		klog.SetOutput(GinkgoWriter)
	}
	http.DefaultClient.Transport = transport.DebugWrappers(http.DefaultTransport)
	http.DefaultClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	Expect(json.Unmarshal(beforeSuiteStateJSON, &beforeSuiteState)).To(Succeed())
}

var (
	GinkgoErr    = gosh.WriterErr(GinkgoWriter)
	GinkgoOut    = gosh.WriterOut(GinkgoWriter)
	GinkgoOutErr = gosh.SetStreams(GinkgoOut, GinkgoErr)
)

func ExpectRun(cmd gosh.Commander) {
	GinkgoHelper()
	Expect(cmd.Run()).To(Succeed())
}

func ExpectRunFlaky(count int, getCmd func() *gosh.Cmd) {
	var err error
	for i := 0; i < count-1; i++ {
		cmd := getCmd()
		err = cmd.Run()
		if err == nil {
			break
		}
		klog.Infof("!!! Flaky: %v: %v", cmd.AsShellArgs(), err)
	}
	Expect(err).To(Succeed())
}

func ExpectStart(cmd gosh.Commander) {
	Expect(cmd.Start()).To(Succeed())
}

func ExpectStop(cmd gosh.Commander) {
	Expect(cmd.Kill()).To(Succeed())
	cmd.Wait()
}

func DeferExpectStop(cmd gosh.Commander) {
	defer func() {
		defer GinkgoRecover()
		ExpectStop(cmd)
	}()
}

func BuildImage() {
	beforeSuiteState.ImageRepo = "local.host/meln5674/kink"
	beforeSuiteState.DefaultImage = fmt.Sprintf("%s:%s", beforeSuiteState.ImageRepo, defaultTag)
	if noRebuild {
		beforeSuiteState.ImageTag = defaultTag
		beforeSuiteState.BuiltImage = beforeSuiteState.DefaultImage
	} else {
		beforeSuiteState.ImageTag = fmt.Sprintf("%d", time.Now().Unix())
		beforeSuiteState.BuiltImage = fmt.Sprintf("%s:%s", beforeSuiteState.ImageRepo, beforeSuiteState.ImageTag)
	}

	if !noRebuild {
		ExpectRun(
			/*
				gosh.
					Command(docker.Build(&dockerOpts, builtImage, "..")...).
					WithParentEnvAnd(map[string]string{"DOCKER_BUILDKIT": "1"}).
					WithStreams(GinkgoOutErr),
			*/
			gosh.And(
				gosh.
					Command("../build-env.sh", "make", "-C", "..", "bin/kink.cover").
					WithParentEnvAnd(map[string]string{"IMAGE_TAG": beforeSuiteState.ImageTag}),
				gosh.
					Command(docker.Build(&dockerOpts, beforeSuiteState.BuiltImage, "..", "-f", "../standalone.Dockerfile", "--build-arg", "KINK_BINARY=bin/kink.cover")...).
					WithParentEnvAnd(map[string]string{"DOCKER_BUILDKIT": "1"}),
				gosh.
					Command(docker.Build(&dockerOpts, fmt.Sprintf("%s:it", beforeSuiteState.ImageRepo), "..", "-f", "../standalone.Dockerfile", "--build-arg", "KINK_BINARY=bin/kink.cover")...).
					WithParentEnvAnd(map[string]string{"DOCKER_BUILDKIT": "1"}),
			).WithStreams(GinkgoOutErr),
		)

		// TEMPORARY: Remove when local-path-provisioner feature/multiple-storage-classes is merged
		ExpectRun(
			gosh.And(
				/*
					gosh.
						Command("../build-env.sh", "make", "-C", "../charts/local-path-provisioner", "build").
						WithParentEnvAnd(map[string]string{"IMAGE_TAG": beforeSuiteState.ImageTag}),
				*/
				gosh.
					Command("docker", "build", "-t", "local.host/meln5674/local-path-provisioner:testing", "-f", "../charts/local-path-provisioner/package/Dockerfile", "../charts/local-path-provisioner"),
			).WithStreams(GinkgoOutErr),
		)
	}
}

func FetchK8sSmokeTestImages() {
	ExpectRun(gosh.Command(docker.Pull(&dockerOpts, k8sSmokeTestDeploymentImage)...).WithStreams(GinkgoOutErr))
	ExpectRun(gosh.Command(docker.Pull(&dockerOpts, k8sSmokeTestStatefulSetImage)...).WithStreams(GinkgoOutErr))
	k8sSmokeTestJobImg, err := crane.Pull(k8sSmokeTestJobImage)
	Expect(err).ToNot(HaveOccurred())
	crane.Save(k8sSmokeTestJobImg, k8sSmokeTestJobImage, k8sSmokeTestJobTarballPath)
}

type KindOpts struct {
	KindCommand       []string
	KubeconfigOutPath string
	ClusterName       string
}

func (k *KindOpts) CreateCluster(configPath, targetKubeconfigPath string) *gosh.Cmd {
	cmd := []string{}
	cmd = append(cmd, k.KindCommand...)
	cmd = append(cmd, "create", "cluster")
	if k.ClusterName != "" {
		cmd = append(cmd, "--name", k.ClusterName)
	}
	if configPath != "" {
		cmd = append(cmd, "--config", configPath)
	}
	if targetKubeconfigPath != "" {
		cmd = append(cmd, "--kubeconfig", targetKubeconfigPath)
	}
	return gosh.Command(cmd...).WithStreams(GinkgoOutErr)
}

func (k *KindOpts) LoadImages(dockerImages ...string) *gosh.Cmd {
	cmd := []string{}
	cmd = append(cmd, k.KindCommand...)
	cmd = append(cmd, "load", "docker-image")
	if k.ClusterName != "" {
		cmd = append(cmd, "--name", k.ClusterName)
	}
	cmd = append(cmd, dockerImages...)
	return gosh.Command(cmd...).WithStreams(GinkgoOutErr)
}

func (k *KindOpts) DeleteCluster() *gosh.Cmd {
	cmd := []string{}
	cmd = append(cmd, k.KindCommand...)
	cmd = append(cmd, "delete", "cluster")
	if k.ClusterName != "" {
		cmd = append(cmd, "--name", k.ClusterName)
	}
	return gosh.Command(cmd...).WithStreams(GinkgoOutErr)
}

func InitKindCluster() {
	pwd, err := os.Getwd()
	repoRoot := filepath.Join(pwd, "..")
	Expect(err).ToNot(HaveOccurred())
	kindConfig, err := ioutil.ReadFile(kindConfigPath + ".tpl")
	kindConfig = []byte(strings.ReplaceAll(string(kindConfig), "${PWD}", repoRoot))
	ioutil.WriteFile(kindConfigPath, kindConfig, 0700)

	if !noCreate {
		ExpectRun(kindOpts.CreateCluster(kindConfigPath, kindKubeconfigPath))
	}
	if !noSuiteCleanup {
		DeferCleanup(func() {
			CleanupPVCDirs()
		})
		DeferCleanup(func() {
			ExpectRun(kindOpts.DeleteCluster())
		})
	}

	if !noLoad {
		ExpectRun(kindOpts.LoadImages(
			beforeSuiteState.BuiltImage,
			beforeSuiteState.DefaultImage,
		))
	}

	if !noDeps {
		lppKubeFlags := kindKubeOpts
		lppKubeFlags.ConfigOverrides.Context.Namespace = "kube-system"
		ExpectRun(
			gosh.
				Command(helm.Upgrade(
					&helmOpts,
					&helm.ChartFlags{
						ChartName: "../charts/local-path-provisioner-0.0.24-dev.tgz",
					},
					&helm.ReleaseFlags{
						Name: "shared-local-path-provisioner",
						Set: map[string]string{
							"storageClass.name":    "shared-local-path",
							"nodePathMap":          "null",
							"sharedFileSystemPath": sharedLocalPathProvisionerMount,
							"fullnameOverride":     "shared-local-path-provisioner",
							"configmap.name":       "shared-local-path-provisioner",
						},
					},
					&lppKubeFlags,
				)...).
				WithStreams(GinkgoOutErr),
		)

		nginxKubeFlags := kindKubeOpts
		nginxKubeFlags.ConfigOverrides.Context.Namespace = "ingress-nginx"
		ExpectRun(
			gosh.
				Command(helm.Upgrade(
					&helmOpts,
					&helm.ChartFlags{
						RepositoryURL: ingressNginxChartRepo,
						ChartName:     ingressNginxChartName,
						Version:       ingressNginxChartVersion,
					},
					&helm.ReleaseFlags{
						Name: "ingress-nginx",
						Set: map[string]string{
							"controller.kind":                             "DaemonSet",
							"controller.service.type":                     "ClusterIP",
							"controller.hostPort.enabled":                 "true",
							"controller.extraArgs.enable-ssl-passthrough": "true",
						},
						UpgradeFlags: []string{"--create-namespace"},
					},
					&nginxKubeFlags,
				)...).
				WithStreams(GinkgoOutErr),
		)
		ExpectRun(
			gosh.
				Command(kubectl.Kubectl(
					&kubectlOpts,
					&nginxKubeFlags,
					"rollout", "status", "ds/ingress-nginx-controller",
				)...).
				WithStreams(GinkgoOutErr),
		)
	}
}

type KinkFlags struct {
	Command     []string
	ConfigPath  string
	ClusterName string
	Env         map[string]string
}

func (k *KinkFlags) Kink(ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, args ...string) *gosh.Cmd {
	cmd := make([]string, 0, len(k.Command)+len(args))
	cmd = append(cmd, k.Command...)
	cmd = append(cmd, flags.AsFlags(ku.Flags())...)
	if k.ConfigPath != "" {
		cmd = append(cmd, "--config", k.ConfigPath)
	}
	if k.ClusterName != "" {
		cmd = append(cmd, "--name", k.ClusterName)
	}
	if chart.ChartName != "" {
		cmd = append(cmd, "--chart", chart.ChartName)
	}
	if chart.RepositoryURL != "" {
		cmd = append(cmd, "--repository-url", chart.RepositoryURL)
	}
	if chart.Version != "" {
		cmd = append(cmd, "--chart-version", chart.Version)
	}
	cmd = append(cmd, release.ValuesFlags()...)
	cmd = append(cmd, args...)
	command := gosh.Command(cmd...).UsingProcessGroup()
	if k.Env != nil {
		command = command.WithParentEnvAnd(k.Env)
	}
	return command
}

func (k *KinkFlags) CreateCluster(ku *kubectl.KubeFlags, targetKubeconfigPath string, controlplaneIngressURL string, chart *helm.ChartFlags, release *helm.ReleaseFlags) *gosh.Cmd {
	args := []string{"create", "cluster"}
	if targetKubeconfigPath != "" {
		args = append(args, "--out-kubeconfig", targetKubeconfigPath)
	}
	if controlplaneIngressURL != "" {
		args = append(args, "--controlplane-ingress-url", controlplaneIngressURL)
	}
	args = append(args, release.UpgradeFlags...)
	return k.Kink(ku, chart, release, args...)
}

func (k *KinkFlags) DeleteCluster(ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags) *gosh.Cmd {
	return k.Kink(ku, chart, release, "delete", "cluster", "--delete-pvcs")
}

func (k *KinkFlags) Shell(ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, script string) *gosh.Cmd {
	return k.Kink(ku, chart, release, "sh", "--", script)
}

func (k *KinkFlags) Load(ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, typ string, flags []string, flag string, items ...string) *gosh.Cmd {
	args := []string{"load", typ}
	args = append(args, flags...)
	for _, item := range items {
		args = append(args, "--"+flag, item)
	}
	return k.Kink(ku, chart, release, args...)
}

func (k *KinkFlags) LoadDockerImage(ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, flags []string, images ...string) *gosh.Cmd {
	return k.Load(ku, chart, release, "docker-image", flags, "image", images...)
}

func (k *KinkFlags) LoadDockerArchive(ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, flags []string, archives ...string) *gosh.Cmd {
	return k.Load(ku, chart, release, "docker-archive", flags, "archive", archives...)
}

func (k *KinkFlags) LoadOCIArchive(ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, flags []string, archives ...string) *gosh.Cmd {
	return k.Load(ku, chart, release, "oci-archive", flags, "archive", archives...)
}

func (k *KinkFlags) PortForward(ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags) *gosh.Cmd {
	return k.Kink(ku, chart, release, "port-forward")
}

func (k *KinkFlags) FileGatewaySend(ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, flags []string, paths ...string) *gosh.Cmd {
	args := []string{"file-gateway", "send"}
	args = append(args, flags...)
	args = append(args, paths...)
	return k.Kink(ku, chart, release, args...)
}

type ExtraChart struct {
	Chart    helm.ChartFlags
	Release  helm.ReleaseFlags
	Rollouts []string
}

type CaseIngressService struct {
	Namespace      string
	Name           string
	HTTPPortName   string
	HTTPSPortName  string
	Hostname       string
	StaticHostname string
	HTTPSOnly      bool
}

type CaseControlplane struct {
	External bool
	NodePort bool
}

type CaseSmokeTest struct {
	Set     map[string]string
	Ingress CaseIngressService
}

type Case struct {
	Name               string
	LoadFlags          []string
	SmokeTest          CaseSmokeTest
	ExtraCharts        []ExtraChart
	Controlplane       CaseControlplane
	Disabled           bool
	FileGatewayEnabled bool

	Focus bool
}

func (c Case) Run() bool {
	if c.Disabled {
		return false
	}
	f := func() {
		It("should work", func(ctx context.Context) {
			pwd, err := os.Getwd()
			repoRoot := filepath.Join(pwd, "..")
			Expect(err).ToNot(HaveOccurred())

			gocoverdir := os.Getenv("GOCOVERDIR")
			Expect(gocoverdir).ToNot(BeEmpty(), "GOCOVERDIR was not set")

			gocoverdir, err = filepath.Abs(filepath.Join("..", gocoverdir))
			Expect(err).ToNot(HaveOccurred())

			gocoverdirArgs := []string{
				"--set", "extraVolumes[0].name=src",
				"--set", "extraVolumes[0].hostPath.path=/src/kink/",
				"--set", "extraVolumes[0].hostPath.type=Directory",

				"--set", "extraVolumeMounts[0].name=src",
				"--set", "extraVolumeMounts[0].mountPath=" + repoRoot,

				"--set", "extraEnv[0].name=GOCOVERDIR",
				"--set", "extraEnv[0].value=" + gocoverdir,
			}

			kinkOpts := KinkFlags{
				//Command:     []string{"go", "run", "../main.go"},
				Command:     append([]string{"../bin/kink.cover"}, gocoverdirArgs...),
				ConfigPath:  filepath.Join("../integration-test", "kink."+c.Name+".config.yaml"),
				ClusterName: c.Name,
				Env:         map[string]string{"GOCOVERDIR": gocoverdir},
			}
			rootedKinkOpts := KinkFlags{
				//Command:     []string{"go", "run", "../main.go"},
				Command:     append([]string{"bin/kink.cover"}, gocoverdirArgs...),
				ConfigPath:  filepath.Join("integration-test", "kink."+c.Name+".config.yaml"),
				ClusterName: c.Name,
				Env:         map[string]string{"GOCOVERDIR": gocoverdir},
			}
			if _, gconfig := GinkgoConfiguration(); gconfig.Verbosity().GTE(gtypes.VerbosityLevelVerbose) {
				kinkOpts.Command = append(kinkOpts.Command, "-v11")
			}
			kinkKubeconfigPath := filepath.Join("../integration-test", "kink."+c.Name+".kubeconfig")

			chart := helm.ChartFlags{
				ChartName: "../helm/kink",
			}
			rootedChart := helm.ChartFlags{
				ChartName: "./helm/kink",
			}
			release := helm.ReleaseFlags{
				Set: map[string]string{
					"image.repository": beforeSuiteState.ImageRepo,
					"image.tag":        beforeSuiteState.ImageTag,
				},
			}

			By("Creating a cluster")
			ExpectRun(kinkOpts.CreateCluster(
				&kindKubeOpts,
				kinkKubeconfigPath,
				"https://localhost",
				&chart,
				&release,
			).WithStreams(GinkgoOutErr))
			DeferCleanup(func() {
				if !noCleanup {
					ExpectRun(kinkOpts.DeleteCluster(
						&kindKubeOpts,
						&chart,
						&release,
					).WithStreams(GinkgoOutErr))
				}
			})

			ExpectRun(
				gosh.
					Command(kubectl.Kubectl(
						&kubectlOpts,
						&kindKubeOpts,
						"rollout", "status", fmt.Sprintf("sts/kink-%s-controlplane", c.Name), "-n", c.Name,
					)...).
					WithStreams(GinkgoOutErr),
			)
			ExpectRun(
				gosh.
					Command(kubectl.Kubectl(
						&kubectlOpts,
						&kindKubeOpts,
						"rollout", "status", fmt.Sprintf("sts/kink-%s-worker", c.Name), "-n", c.Name,
					)...).
					WithStreams(GinkgoOutErr),
			)
			// TEMPORARY: Remove when local-path-provisioner feature/multiple-storage-classes is merged
			ExpectRunFlaky(5, func() *gosh.Cmd {
				return kinkOpts.LoadDockerImage(
					&kindKubeOpts,
					&chart,
					&release,
					c.LoadFlags,
					"local.host/meln5674/local-path-provisioner:testing",
				).WithStreams(GinkgoOutErr)
			})
			ExpectRun(
				gosh.
					Command(kubectl.Kubectl(
						&kubectlOpts,
						&kindKubeOpts,
						"rollout", "status", fmt.Sprintf("deploy/kink-%s-lb-manager", c.Name), "-n", c.Name,
					)...).
					WithStreams(GinkgoOutErr),
			)

			kinkKubeOpts := kubectl.KubeFlags{
				Kubeconfig: kinkKubeconfigPath,
			}

			By("Connecting to the controlplane w/ kubectl within a shell script")
			ExpectRun(kinkOpts.Shell(
				&kindKubeOpts,
				&chart,
				&release,
				`
				set -xe
				while ! kubectl version ; do
					sleep 10;
				done
				kubectl cluster-info
				while true; do
					NODES=$(kubectl get nodes)
					if ! grep NotReady <<< "${NODES}"; then
						break
					fi
					echo 'Not all nodes are ready yet'
					sleep 15
				done
				`,
			).WithStreams(GinkgoOutErr))

			if !noDeps && !noLoad {
				k8sSmokeTestStatefulSetLoadFlags := make([]string, 0, len(c.LoadFlags)+2)
				k8sSmokeTestStatefulSetLoadFlags = append(k8sSmokeTestStatefulSetLoadFlags, c.LoadFlags...)
				k8sSmokeTestStatefulSetLoadFlags = append(k8sSmokeTestStatefulSetLoadFlags, "--parallel-loads", "1")

				By("Loading an image from the docker daemon")
				Eventually(func() error {
					return kinkOpts.LoadDockerImage(
						&kindKubeOpts,
						&chart,
						&release,
						k8sSmokeTestStatefulSetLoadFlags,
						k8sSmokeTestStatefulSetImage,
					).WithStreams(GinkgoOutErr).Run()
				}, "15m").Should(Succeed())

				By("Loading an image from a docker archive")

				ExpectRun(gosh.Command(docker.Save(&dockerOpts, k8sSmokeTestDeploymentImage)...).WithStreams(gosh.FileOut(k8sSmokeTestDeploymentTarballPath), GinkgoErr))
				Eventually(func() error {
					return kinkOpts.LoadDockerArchive(
						&kindKubeOpts,
						&chart,
						&release,
						c.LoadFlags,
						k8sSmokeTestDeploymentTarballPath,
					).WithStreams(GinkgoOutErr).Run()
				}, "15m").Should(Succeed())

				By("Loading an image from an OCI archive")
				Eventually(func() error {
					return kinkOpts.LoadOCIArchive(
						&kindKubeOpts,
						&chart,
						&release,
						c.LoadFlags, k8sSmokeTestJobTarballPath,
					).WithStreams(GinkgoOutErr).Run()
				}).Should(Succeed())
			}

			if !c.Controlplane.External {
				By("Forwarding the controplane port")
				controlplanePortForward := kinkOpts.PortForward(
					&kindKubeOpts,
					&chart,
					&release,
				).WithStreams(GinkgoOutErr)
				ExpectStart(controlplanePortForward)
				DeferCleanup(func() {
					ExpectStop(controlplanePortForward)
				})
			}
			Eventually(func() error {
				return gosh.Command(kubectl.Version(&kubectlOpts, &kinkKubeOpts)...).WithStreams(GinkgoOutErr).Run()
			}, "30s", "1s").Should(Succeed())

			By("Watching pods")
			podWatch := gosh.
				Command(kubectl.WatchPods(&kubectlOpts, &kinkKubeOpts, nil, true)...).
				WithStreams(GinkgoOutErr)
			ExpectStart(podWatch)
			DeferCleanup(func() {
				ExpectStop(podWatch)
			})

			if !noDeps {
				for _, chart := range c.ExtraCharts {
					By(fmt.Sprintf("Releasing %s the helm chart", chart.Chart.ChartName))

					ExpectRun(gosh.Command(helm.RepoAdd(&helmOpts, &chart.Chart)...).WithStreams(GinkgoOutErr))
					ExpectRun(gosh.Command(helm.RepoUpdate(&helmOpts, chart.Chart.RepoName())...).WithStreams(GinkgoOutErr))
					ExpectRun(gosh.Command(helm.Upgrade(&helmOpts, &chart.Chart, &chart.Release, &kinkKubeOpts)...).WithStreams(GinkgoOutErr))
					if !noCleanup {
						DeferCleanup(func() {
							ExpectRun(gosh.Command(helm.Delete(&helmOpts, &chart.Chart, &chart.Release, &kinkKubeOpts)...).WithStreams(GinkgoOutErr))
						})
					}

					for _, rollout := range chart.Rollouts {
						ExpectRun(gosh.Command(kubectl.Kubectl(&kubectlOpts, &kinkKubeOpts, "rollout", "status", rollout)...).WithStreams(GinkgoOutErr))
					}
				}

				By("Releasing the main helm chart")

				k8sSmokeTestRelease := helm.ReleaseFlags{
					Name:         "k8s-smoke-test",
					UpgradeFlags: []string{"--debug", "--timeout=15m"},
					Set:          c.SmokeTest.Set,
				}
				ExpectRun(gosh.Command(helm.Upgrade(&helmOpts, &k8sSmokeTestChart, &k8sSmokeTestRelease, &kinkKubeOpts)...).WithStreams(GinkgoOutErr))
				if !noCleanup {
					DeferCleanup(func() {
						ExpectRun(gosh.Command(helm.Delete(&helmOpts, &k8sSmokeTestChart, &k8sSmokeTestRelease, &kinkKubeOpts)...).WithStreams(GinkgoOutErr))
					})
				}
				ExpectRun(gosh.Command(kubectl.Kubectl(&kubectlOpts, &kinkKubeOpts, "patch", "deploy/k8s-smoke-test", "-p", `{
                        "spec": {
                                "template": {
                                        "spec": {
                                                "containers": [
                                                        {
                                                                "name": "k8s-smoke-test",
                                                                "ports": [
                                                                        {
                                                                                "containerPort": 8080,
                                                                                "hostPort": 9080
                                                                        },
                                                                        {
                                                                                "containerPort": 8443,
                                                                                "hostPort": 9443
                                                                        }
                                                                ]
                                                        }
                                                ]
                                        }
                                }
                        }
                }`)...).WithStreams(GinkgoOutErr))
			}
			ExpectRun(
				gosh.
					Command(kubectl.Kubectl(
						&kubectlOpts,
						&kinkKubeOpts,
						"rollout", "status", "deploy/k8s-smoke-test",
					)...).
					WithStreams(GinkgoOutErr),
			)
			ExpectRun(
				gosh.
					Command(kubectl.Kubectl(
						&kubectlOpts,
						&kinkKubeOpts,
						"rollout", "status", "sts/k8s-smoke-test",
					)...).
					WithStreams(GinkgoOutErr),
			)

			var mergedValues k8ssmoketest.MergedValues
			Expect(
				gosh.
					Command(
						helmOpts.Helm(
							&kinkKubeOpts,
							"get", "values", "--all", "-o", "json", "k8s-smoke-test",
						)...).
					WithStreams(
						gosh.FuncOut(gosh.SaveJSON(&mergedValues)),
						GinkgoErr,
					).
					Run(),
			).To(Succeed())

			kinkKubeConfigLoader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				&clientcmd.ClientConfigLoadingRules{
					ExplicitPath: kinkKubeOpts.Kubeconfig,
				},
				&kinkKubeOpts.ConfigOverrides,
			)

			kinkKubeconfigNamespace, _, err := kinkKubeConfigLoader.Namespace()
			Expect(err).ToNot(HaveOccurred())

			kinkKubeconfig, err := kinkKubeConfigLoader.ClientConfig()
			Expect(err).ToNot(HaveOccurred())

			k8ssmoketest.Test(ctx, &test.Config{
				HTTP:                 http.DefaultClient,
				K8sConfig:            kinkKubeconfig,
				ReleaseNamespace:     kinkKubeconfigNamespace,
				ReleaseName:          "k8s-smoke-test",
				MergedValues:         &mergedValues,
				PortForwardLocalPort: 8080,
			})

			if c.SmokeTest.Ingress.StaticHostname != "" {
				By("Interacting with the released service over a static ingress (HTTP)")
				req, err := http.NewRequest("GET", "http://localhost:80/rwx/test-file", nil)
				Expect(err).ToNot(HaveOccurred())
				req.Host = c.SmokeTest.Ingress.StaticHostname
				// TODO: Actually set up a cert for this
				http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
				Eventually(func() error { _, err := http.DefaultClient.Do(req); return err }, "30s", "1s").Should(Succeed())
				resp, err := http.DefaultClient.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				By("Interacting with the released service over a static ingress (HTTPS)")
				req, err = http.NewRequest("GET", "https://localhost:443/rwx/test-file", nil)
				Expect(err).ToNot(HaveOccurred())
				req.Host = c.SmokeTest.Ingress.StaticHostname
				// TODO: Actually set up a cert for this
				http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
				Eventually(func() error { _, err := http.DefaultClient.Do(req); return err }, "30s", "1s").Should(Succeed())
				resp, err = http.DefaultClient.Do(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			}

			if !c.FileGatewayEnabled {
				return
			}

			kubeOpts := kindKubeOpts
			kubeOpts.ConfigOverrides.Context.Namespace = c.Name

			By("Sending a file through the file gateway")
			ExpectRun(rootedKinkOpts.FileGatewaySend(
				&rootedKindKubeOpts,
				&rootedChart,
				&release,
				[]string{
					"--send-dest", sharedLocalPathProvisionerMount,
					"--send-exclude", "integration-test/volumes", // This will cause an infinite loop of copying to itself
					"--send-exclude", "integration-test/log", // This is being written to while the test is running, meaning it will be bigger than its header, thus fail
					"--file-gateway-ingress-url", "https://localhost",
					"--port-forward=false",
					"-v11",
				},
				"Makefile",
				"integration-test",
			).WithStreams(GinkgoOutErr).WithWorkingDir(repoRoot))

			By("Checking the files were received")
			ExpectRun(gosh.Command(
				kubectl.Exec(
					&kubectlOpts,
					&kubeOpts,
					fmt.Sprintf("kink-%s-controlplane-0", c.Name),
					false, false,
					"cat", filepath.Join(sharedLocalPathProvisionerMount, "Makefile"))...,
			).WithStreams(GinkgoOutErr))
			ExpectRun(gosh.Command(
				kubectl.Exec(
					&kubectlOpts,
					&kubeOpts,
					fmt.Sprintf("kink-%s-controlplane-0", c.Name),
					false, false,
					"ls", filepath.Join(sharedLocalPathProvisionerMount, "Makefile"))...,
			).WithStreams(GinkgoOutErr))
		})

	}
	if c.Focus {
		return FDescribe(c.Name, Label(c.Name), f)
	} else {
		return Describe(c.Name, Label(c.Name), f)
	}
}

func CleanupPVCDirs() {
	pwd, err := os.Getwd()
	Expect(err).ToNot(HaveOccurred())
	repoRoot := filepath.Join(pwd, "..")
	cleaner := gosh.Command("./hack/clean-tests-afterwards.sh").WithStreams(GinkgoOutErr)
	cleaner.Cmd.Dir = repoRoot
	ExpectRun(cleaner)
}

var _ = Case{
	Name: "k3s",
	SmokeTest: CaseSmokeTest{
		Set: map[string]string{
			"persistence.rwo.storageClassName": "standard", // default
			"persistence.rwx.storageClassName": "shared-local-path",
			"deployment.ingress.hostname":      "smoke-test.k3s.ingress.local",
		},
		Ingress: CaseIngressService{
			StaticHostname: "k8s-sfb.k3s.ingress.outer",
		},
	},
	ExtraCharts: []ExtraChart{
		{
			Chart: helm.ChartFlags{
				RepositoryURL: ingressNginxChartRepo,
				ChartName:     ingressNginxChartName,
				Version:       ingressNginxChartVersion,
			},
			Release: helm.ReleaseFlags{
				Name: "ingress-nginx",
			},
			Rollouts: []string{
				"deploy/ingress-nginx-controller",
			},
		},
	},
	Controlplane: CaseControlplane{
		External: true,
	},
	FileGatewayEnabled: true,
}.Run()

var _ = Case{
	Name: "k3s-ha",
	SmokeTest: CaseSmokeTest{
		Set: map[string]string{
			"persistence.enabled":                         "true",
			"persistence.storageClass":                    "shared-local-path",
			"persistence.accessModes":                     "{ReadWriteMany}",
			"replicaCount":                                "2",
			"podAntiAffinityPreset":                       "hard",
			"ingress.enabled":                             "true",
			"ingress.ingressClassName":                    "nginx",
			"ingress.hostname":                            "wordpress.k3s-ha.ingress.local",
			"mariadb.enabled":                             "true",
			"memcached.enabled":                           "true",
			"image.pullPolicy":                            "Never",
			"mariadb.image.pullPolicy":                    "Never",
			"memcached.image.pullPolicy":                  "Never",
			"updateStrategy.rollingUpdate.maxSurge":       "0",
			"updateStrategy.rollingUpdate.maxUnavailable": "1",
		},
		Ingress: CaseIngressService{
			Namespace:      "default",
			Name:           "ingress-nginx-controller",
			HTTPPortName:   "http",
			HTTPSPortName:  "https",
			Hostname:       "wordpress.k3s-ha.ingress.local",
			StaticHostname: "k8s-sfb.k3s-ha.ingress.outer",
			HTTPSOnly:      true,
		},
	},
	ExtraCharts: []ExtraChart{
		{
			Chart: helm.ChartFlags{
				RepositoryURL: ingressNginxChartRepo,
				ChartName:     ingressNginxChartName,
				Version:       ingressNginxChartVersion,
			},
			Release: helm.ReleaseFlags{
				Name: "ingress-nginx",
			},
			Rollouts: []string{
				"deploy/ingress-nginx-controller",
			},
		},
	},

	Controlplane: CaseControlplane{
		External: true,
		NodePort: true,
	},

	FileGatewayEnabled: true,
}.Run()

var _ = Case{
	Name:      "k3s-single",
	LoadFlags: []string{"--only-load-workers=false"},
	SmokeTest: CaseSmokeTest{
		Set: map[string]string{
			"persistence.enabled":          "true",
			"persistence.storageClass":     "shared-local-path",
			"persistence.accessModes":      "{ReadWriteMany}",
			"replicaCount":                 "1",
			"ingress.enabled":              "true",
			"ingress.ingressClassName":     "nginx",
			"ingress.hostname":             "wordpress.k3s-single.ingress.local",
			"mariadb.enabled":              "true",
			"memcached.enabled":            "true",
			"service.type":                 "ClusterIP",
			"image.pullPolicy":             "Never",
			"mariadb.image.pullPolicy":     "Never",
			"memcached.image.pullPolicy":   "Never",
			"updateStrategy.type":          "Recreate",
			"updateStrategy.rollingUpdate": "null",
		},
		Ingress: CaseIngressService{
			Namespace:      "default",
			Name:           "ingress-nginx-controller",
			HTTPPortName:   "http",
			HTTPSPortName:  "https",
			Hostname:       "wordpress.k3s-single.ingress.local",
			StaticHostname: "k8s-sfb.k3s-single.ingress.outer",
		},
	},
	ExtraCharts: []ExtraChart{
		{
			Chart: helm.ChartFlags{
				RepositoryURL: ingressNginxChartRepo,
				ChartName:     ingressNginxChartName,
				Version:       ingressNginxChartVersion,
			},
			Release: helm.ReleaseFlags{
				Name: "ingress-nginx",
				Set: map[string]string{
					"controller.kind": "DaemonSet",
					//"controller.service.type":     "ClusterIP",
					"controller.hostPort.enabled": "true",
				},
			},
			Rollouts: []string{
				"ds/ingress-nginx-controller",
			},
		},
	},

	FileGatewayEnabled: true,
}.Run()

var _ = Case{
	Name: "rke2",
	SmokeTest: CaseSmokeTest{
		Set: map[string]string{
			"persistence.enabled":                         "true",
			"persistence.storageClass":                    "shared-local-path",
			"persistence.accessModes":                     "{ReadWriteMany}",
			"replicaCount":                                "2",
			"podAntiAffinityPreset":                       "hard",
			"ingress.enabled":                             "true",
			"ingress.ingressClassName":                    "nginx",
			"ingress.hostname":                            "wordpress.rke2.ingress.local",
			"mariadb.enabled":                             "true",
			"memcached.enabled":                           "true",
			"service.type":                                "ClusterIP",
			"image.pullPolicy":                            "Never",
			"mariadb.image.pullPolicy":                    "Never",
			"memcached.image.pullPolicy":                  "Never",
			"updateStrategy.rollingUpdate.maxSurge":       "0",
			"updateStrategy.rollingUpdate.maxUnavailable": "1",
		},
		Ingress: CaseIngressService{
			Namespace:     "default",
			Name:          "ingress-nginx-controller",
			HTTPPortName:  "http",
			HTTPSPortName: "https",
			Hostname:      "wordpress.rke2.ingress.local",
			HTTPSOnly:     true,
		},
	},
	ExtraCharts: []ExtraChart{
		{
			Chart: helm.ChartFlags{
				RepositoryURL: ingressNginxChartRepo,
				ChartName:     ingressNginxChartName,
				Version:       ingressNginxChartVersion,
			},
			Release: helm.ReleaseFlags{
				Name: "ingress-nginx",
				Set: map[string]string{
					"controller.kind": "DaemonSet",
					//"controller.service.type":     "ClusterIP",
					"controller.hostPort.enabled": "true",
				},
			},
			Rollouts: []string{
				"ds/ingress-nginx-controller",
			},
		},
	},
}.Run()

var (
	randPortLock = make(chan struct{}, 1)
)

func GetRandomPort() int {
	return WithRandomPort[int](func(port int) int { return port })
}
func WithRandomPort[T any](f func(int) T) T {
	return WithRandomPorts[T](1, func(ports []int) T { return f(ports[0]) })
}
func WithRandomPorts[T any](count int, f func([]int) T) T {
	randPortLock <- struct{}{}
	defer func() { <-randPortLock }()

	listeners := make([]net.Listener, count)
	ports := make([]int, count)
	for ix := 0; ix < count; ix++ {

		listener, err := net.Listen("tcp", ":0")
		Expect(err).ToNot(HaveOccurred())
		defer listener.Close()

		listeners[ix] = listener
		ports[ix] = listener.Addr().(*net.TCPAddr).Port
	}
	for _, listener := range listeners {
		listener.Close()
	}

	return f(ports)
}
