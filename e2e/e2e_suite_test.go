package e2e_test

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	gtypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"k8s.io/klog/v2"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/meln5674/gosh"

	"github.com/meln5674/kink/pkg/docker"
	"github.com/meln5674/kink/pkg/flags"
	"github.com/meln5674/kink/pkg/helm"
	"github.com/meln5674/kink/pkg/kubectl"
)

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2e Suite")
}

var (
	imageRepo  string
	imageTag   string
	builtImage string

	kindConfigPath     = "../integration-test/kind.config.yaml"
	kindKubeconfigPath = "../integration-test/kind.kubeconfig"

	kindOpts = KindOpts{
		KubeconfigOutPath: kindKubeconfigPath,
		ClusterName:       "kink-it",
	}
	dockerOpts = docker.DockerFlags{
		Command: []string{"docker"},
	}
	kubectlOpts = kubectl.KubectlFlags{
		Command: []string{"kubectl"},
	}
	kindKubeOpts = kubectl.KubeFlags{
		Kubeconfig: kindOpts.KubeconfigOutPath,
	}
	helmOpts = helm.HelmFlags{
		Command: []string{"helm"},
	}

	localPathProvisionerStorageRoot = "../integration-test"

	localPathProvisionerStorageRel       = "local-path-provisioner"
	localPathProvisionerStorage          = filepath.Join(localPathProvisionerStorageRoot, localPathProvisionerStorageRel)
	localPathProvisionerMount            = "/var/local-path-provisioner"
	sharedLocalPathProvisionerStorageRel = "shared-local-path-provisioner"
	sharedLocalPathProvisionerStorage    = filepath.Join(localPathProvisionerStorageRoot, sharedLocalPathProvisionerStorageRel)
	sharedLocalPathProvisionerMount      = "/var/shared-local-path-provisioner"

	wordpressChartRepo    = "https://charts.bitnami.com/bitnami"
	wordpressChartName    = "wordpress"
	wordpressChartVersion = "15.2.7"

	wordpressImage = "docker.io/bitnami/wordpress:6.0.3-debian-11-r3"
	mariadbImage   = "docker.io/bitnami/mariadb:10.6.10-debian-11-r6"
	memcachedImage = "docker.io/bitnami/memcached:1.6.17-debian-11-r15"

	mariadbTarballPath   = "../integration-test/mariadb.tar"
	memcachedTarballPath = "../integration-test/memcached.tar"
)

var _ = BeforeSuite(func() {
	klog.InitFlags(flag.CommandLine)
	if _, gconfig := GinkgoConfiguration(); gconfig.Verbosity().GTE(gtypes.VerbosityLevelVerbose) {
		flag.Set("v", "11")
		klog.SetOutput(GinkgoWriter)
	}

	BuildImage()
	FetchWordpressImages()
	InitKindCluster()

	podWatch := gosh.
		Command(kubectl.WatchPods(&kubectlOpts, &kindKubeOpts, nil)...).
		WithStreams(GinkgoOutErr)
	ExpectStart(podWatch)
	DeferCleanup(func() {
		ExpectStop(podWatch)
	})
})

var (
	GinkgoErr    = gosh.WriterErr(GinkgoWriter)
	GinkgoOut    = gosh.WriterOut(GinkgoWriter)
	GinkgoOutErr = gosh.SetStreams(GinkgoOut, GinkgoErr)
)

func ExpectRun(cmd gosh.Commander) {
	Expect(cmd.Run()).To(Succeed())
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
	imageRepo = "local.host/meln5674/kink"
	imageTag = fmt.Sprintf("%d", time.Now().Unix())
	builtImage = fmt.Sprintf("%s:%s", imageRepo, imageTag)

	ExpectRun(
		gosh.
			Command(docker.Build(&dockerOpts, builtImage, "..")...).
			WithParentEnvAnd(map[string]string{"DOCKER_BUILDKIT": "1"}).
			WithStreams(GinkgoOutErr),
	)
}

func FetchWordpressImages() {
	ExpectRun(gosh.Command(docker.Pull(&dockerOpts, wordpressImage)...).WithStreams(GinkgoOutErr))
	ExpectRun(gosh.Command(docker.Pull(&dockerOpts, mariadbImage)...).WithStreams(GinkgoOutErr))
	memcachedImg, err := crane.Pull(memcachedImage)
	Expect(err).ToNot(HaveOccurred())
	crane.Save(memcachedImg, memcachedImage, memcachedTarballPath)
}

type KindOpts struct {
	KubeconfigOutPath string
	ClusterName       string
}

func (k *KindOpts) CreateCluster(configPath, targetKubeconfigPath string) *gosh.Cmd {
	cmd := []string{"kind", "create", "cluster"}
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
	cmd := []string{"kind", "load", "docker-image"}
	if k.ClusterName != "" {
		cmd = append(cmd, "--name", k.ClusterName)
	}
	cmd = append(cmd, dockerImages...)
	return gosh.Command(cmd...).WithStreams(GinkgoOutErr)
}

func (k *KindOpts) DeleteCluster() *gosh.Cmd {
	cmd := []string{"kind", "delete", "cluster"}
	if k.ClusterName != "" {
		cmd = append(cmd, "--name", k.ClusterName)
	}
	return gosh.Command(cmd...).WithStreams(GinkgoOutErr)
}

func InitKindCluster() {
	pwd, err := os.Getwd()
	Expect(err).ToNot(HaveOccurred())
	kindConfig, err := ioutil.ReadFile(kindConfigPath + ".tpl")
	kindConfig = []byte(strings.ReplaceAll(string(kindConfig), "${PWD}", pwd))
	ioutil.WriteFile(kindConfigPath, kindConfig, 0700)

	ExpectRun(kindOpts.CreateCluster(kindConfigPath, kindKubeconfigPath))
	DeferCleanup(func() {
		ExpectRun(kindOpts.DeleteCluster())
	})
	DeferCleanup(func() {
		CleanupPVCDirs()
	})

	ExpectRun(kindOpts.LoadImages(builtImage))

	ExpectRun(
		gosh.
			Command(docker.Exec(
				&dockerOpts,
				kindOpts.ClusterName+"-control-plane",
				[]string{},
				"mkdir", "-p", sharedLocalPathProvisionerMount,
			)...).
			WithStreams(GinkgoOutErr),
	)

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

}

type KinkFlags struct {
	Command     []string
	ConfigPath  string
	ClusterName string
}

func (k *KinkFlags) Kink(ku *kubectl.KubeFlags, args ...string) *gosh.Cmd {
	cmd := make([]string, 0, len(k.Command)+len(args))
	cmd = append(cmd, k.Command...)
	cmd = append(cmd, flags.AsFlags(ku.Flags())...)
	if k.ConfigPath != "" {
		cmd = append(cmd, "--config", k.ConfigPath)
	}
	if k.ClusterName != "" {
		cmd = append(cmd, "--name", k.ClusterName)
	}
	cmd = append(cmd, args...)
	return gosh.Command(cmd...).UsingProcessGroup()
}

func (k *KinkFlags) CreateCluster(ku *kubectl.KubeFlags, targetKubeconfigPath string, chart *helm.ChartFlags, release *helm.ReleaseFlags) *gosh.Cmd {
	args := []string{"create", "cluster"}
	if chart.ChartName != "" {
		args = append(args, "--chart", chart.ChartName)
	}
	if chart.RepositoryURL != "" {
		args = append(args, "--repository-url", chart.RepositoryURL)
	}
	if chart.Version != "" {
		args = append(args, "--chart-version", chart.Version)
	}
	if targetKubeconfigPath != "" {
		args = append(args, "--out-kubeconfig", targetKubeconfigPath)
	}
	args = append(args, release.ValuesFlags()...)
	return k.Kink(ku, args...)
}

func (k *KinkFlags) DeleteCluster(ku *kubectl.KubeFlags) *gosh.Cmd {
	return k.Kink(ku, "delete", "cluster")
}

func (k *KinkFlags) Shell(ku *kubectl.KubeFlags, script string) *gosh.Cmd {
	return k.Kink(ku, "sh", "--", script)
}

func (k *KinkFlags) Load(ku *kubectl.KubeFlags, typ string, flags []string, flag string, items ...string) *gosh.Cmd {
	args := []string{"load", typ}
	args = append(args, flags...)
	for _, item := range items {
		args = append(args, "--"+flag, item)
	}
	return k.Kink(ku, args...)
}

func (k *KinkFlags) LoadDockerImage(ku *kubectl.KubeFlags, flags []string, images ...string) *gosh.Cmd {
	return k.Load(ku, "docker-image", flags, "image", images...)
}

func (k *KinkFlags) LoadDockerArchive(ku *kubectl.KubeFlags, flags []string, archives ...string) *gosh.Cmd {
	return k.Load(ku, "docker-archive", flags, "archive", archives...)
}

func (k *KinkFlags) LoadOCIArchive(ku *kubectl.KubeFlags, flags []string, archives ...string) *gosh.Cmd {
	return k.Load(ku, "oci-archive", flags, "archive", archives...)
}

func (k *KinkFlags) PortForward(ku *kubectl.KubeFlags) *gosh.Cmd {
	return k.Kink(ku, "port-forward")
}

func Case(name string, loadFlags []string, set map[string]string) bool {
	return Describe(name, func() {
		It("should work", func() {
			kinkOpts := KinkFlags{
				Command:     []string{"go", "run", "../main.go"},
				ConfigPath:  filepath.Join("../integration-test", "kink."+name+".config.yaml"),
				ClusterName: name,
			}
			if _, gconfig := GinkgoConfiguration(); gconfig.Verbosity().GTE(gtypes.VerbosityLevelVerbose) {
				kinkOpts.Command = append(kinkOpts.Command, "-v11")
			}
			kinkKubeconfigPath := filepath.Join("../integration-test", "kink."+name+".kubeconfig")

			By("Creating a cluster")
			ExpectRun(kinkOpts.CreateCluster(
				&kindKubeOpts,
				kinkKubeconfigPath,
				&helm.ChartFlags{
					ChartName: "../helm/kink",
				},
				&helm.ReleaseFlags{
					Set: map[string]string{
						"image.repository": imageRepo,
						"image.tag":        imageTag,
					},
				},
			).WithStreams(GinkgoOutErr))
			DeferCleanup(func() {
				ExpectRun(kinkOpts.DeleteCluster(
					&kindKubeOpts,
				).WithStreams(GinkgoOutErr))
			})

			kinkKubeOpts := kubectl.KubeFlags{
				Kubeconfig: kinkKubeconfigPath,
			}

			By("Connecting to the controlplane w/ kubectl within a shell script")
			ExpectRun(kinkOpts.Shell(
				&kindKubeOpts,
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

			wordpressLoadFlags := make([]string, 0, len(loadFlags)+2)
			wordpressLoadFlags = append(wordpressLoadFlags, loadFlags...)
			wordpressLoadFlags = append(wordpressLoadFlags, "--parallel-loads", "1")
			By("Loading an image from the docker daemon")
			ExpectRun(kinkOpts.LoadDockerImage(&kindKubeOpts, wordpressLoadFlags, wordpressImage).WithStreams(GinkgoOutErr))

			By("Loading an image from a docker archive")

			ExpectRun(gosh.Command(docker.Save(&dockerOpts, mariadbImage)...).WithStreams(gosh.FileOut(mariadbTarballPath), GinkgoErr))
			ExpectRun(kinkOpts.LoadDockerArchive(&kindKubeOpts, loadFlags, mariadbTarballPath).WithStreams(GinkgoOutErr))

			By("Loading an image from an OCI archive")

			ExpectRun(kinkOpts.LoadOCIArchive(&kindKubeOpts, loadFlags, memcachedTarballPath).WithStreams(GinkgoOutErr))

			By("Forwarding the controplane port")
			controlplanePortForward := kinkOpts.PortForward(&kindKubeOpts).WithStreams(GinkgoOutErr)
			ExpectStart(controlplanePortForward)
			DeferCleanup(func() {
				ExpectStop(controlplanePortForward)
			})
			Eventually(func() error {
				return gosh.Command(kubectl.Version(&kubectlOpts, &kinkKubeOpts)...).WithStreams(GinkgoOutErr).Run()
			}, "10s", "1s").Should(Succeed())

			By("Watching pods")
			podWatch := gosh.
				Command(kubectl.WatchPods(&kubectlOpts, &kinkKubeOpts, nil)...).
				WithStreams(GinkgoOutErr)
			ExpectStart(podWatch)
			DeferCleanup(func() {
				ExpectStop(podWatch)
			})

			By("Releasing a helm chart")

			wordpressChart := helm.ChartFlags{
				RepositoryURL: wordpressChartRepo,
				ChartName:     wordpressChartName,
				Version:       wordpressChartVersion,
			}
			wordpressRelease := helm.ReleaseFlags{
				Name:         "wordpress",
				UpgradeFlags: []string{"--debug"},
				Set:          set,
			}
			ExpectRun(gosh.Command(helm.RepoAdd(&helmOpts, &wordpressChart)...).WithStreams(GinkgoOutErr))
			ExpectRun(gosh.Command(helm.RepoUpdate(&helmOpts, wordpressChart.RepoName())...).WithStreams(GinkgoOutErr))
			ExpectRun(gosh.Command(helm.Upgrade(&helmOpts, &wordpressChart, &wordpressRelease, &kinkKubeOpts)...).WithStreams(GinkgoOutErr))

			By("Interacting with the released service")

			wordpressPortForward := gosh.Command(kubectl.PortForward(&kubectlOpts, &kinkKubeOpts, "svc/wordpress", map[string]string{"8080": "80"})...).WithStreams(GinkgoOutErr)
			ExpectStart(wordpressPortForward)
			DeferCleanup(func() {
				ExpectStop(wordpressPortForward)
			})

			Eventually(func() error { _, err := http.Get("http://localhost:8080"); return err }, "10s", "1s").Should(Succeed())
		})

	})
}

func CleanupPVCDirs() {
	pwd, err := os.Getwd()
	Expect(err).ToNot(HaveOccurred())
	ExpectRun(
		gosh.Command(docker.Run(&dockerOpts,
			[]string{
				"--rm",
				"-i",
				"-v", fmt.Sprintf("%s:%s", filepath.Join(pwd, localPathProvisionerStorageRoot), "/tmp/integration-test"),
			}, "centos:7", "bash", "-c", fmt.Sprintf("rm -rf /tmp/integration-test/%s /tmp/integration-test/%s", localPathProvisionerStorageRel, sharedLocalPathProvisionerStorageRel),
		)...).WithStreams(GinkgoOutErr),
	)
}

var _ = Case("k3s", []string{}, map[string]string{
	"persistence.enabled":        "true",
	"persistence.storageClass":   "shared-local-path",
	"persistence.accessModes":    "{ReadWriteMany}",
	"replicaCount":               "2",
	"podAntiAffinityPreset":      "hard",
	"mariadb.enabled":            "true",
	"memcached.enabled":          "true",
	"service.type":               "ClusterIP",
	"ingress.enabled":            "true",
	"image.pullPolicy":           "Never",
	"mariadb.image.pullPolicy":   "Never",
	"memcached.image.pullPolicy": "Never",
})

var _ = Case("k3s-single", []string{"--only-load-workers=false"}, map[string]string{
	"persistence.enabled":        "true",
	"persistence.storageClass":   "shared-local-path",
	"persistence.accessModes":    "{ReadWriteMany}",
	"replicaCount":               "1",
	"mariadb.enabled":            "true",
	"memcached.enabled":          "true",
	"service.type":               "ClusterIP",
	"ingress.enabled":            "true",
	"image.pullPolicy":           "Never",
	"mariadb.image.pullPolicy":   "Never",
	"memcached.image.pullPolicy": "Never",
})

var _ = Case("k3s-ha", []string{}, map[string]string{
	"persistence.enabled":        "true",
	"persistence.storageClass":   "shared-local-path",
	"persistence.accessModes":    "{ReadWriteMany}",
	"replicaCount":               "2",
	"podAntiAffinityPreset":      "hard",
	"mariadb.enabled":            "true",
	"memcached.enabled":          "true",
	"service.type":               "ClusterIP",
	"ingress.enabled":            "true",
	"image.pullPolicy":           "Never",
	"mariadb.image.pullPolicy":   "Never",
	"memcached.image.pullPolicy": "Never",
})

var _ = Case("rke2", []string{}, map[string]string{
	"persistence.enabled":        "true",
	"persistence.storageClass":   "shared-local-path",
	"persistence.accessModes":    "{ReadWriteMany}",
	"replicaCount":               "2",
	"podAntiAffinityPreset":      "hard",
	"mariadb.enabled":            "true",
	"memcached.enabled":          "true",
	"service.type":               "ClusterIP",
	"ingress.enabled":            "true",
	"image.pullPolicy":           "Never",
	"mariadb.image.pullPolicy":   "Never",
	"memcached.image.pullPolicy": "Never",
})
