package e2e_test

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	gtypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
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

	ingressNginxChartRepo    = "https://kubernetes.github.io/ingress-nginx"
	ingressNginxChartName    = "ingress-nginx"
	ingressNginxChartVersion = "4.4.2"

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
		Command(kubectl.WatchPods(&kubectlOpts, &kindKubeOpts, nil, true)...).
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
	imageTag = "it" //fmt.Sprintf("%d", time.Now().Unix())
	builtImage = fmt.Sprintf("%s:%s", imageRepo, imageTag)

	ExpectRun(
		/*
			gosh.
				Command(docker.Build(&dockerOpts, builtImage, "..")...).
				WithParentEnvAnd(map[string]string{"DOCKER_BUILDKIT": "1"}).
				WithStreams(GinkgoOutErr),
		*/
		gosh.And(
			gosh.
				Command("../build-env.sh", "make", "-C", "..", "bin/kink").
				WithParentEnvAnd(map[string]string{"IMAGE_TAG": imageTag}),
			gosh.
				Command(docker.Build(&dockerOpts, builtImage, "..", "-f", "../standalone.Dockerfile")...).
				WithParentEnvAnd(map[string]string{"DOCKER_BUILDKIT": "1"}),
		).WithStreams(GinkgoOutErr),
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
	kindConfig = []byte(strings.ReplaceAll(string(kindConfig), "${PWD}", filepath.Join(pwd, "..")))
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

type KinkFlags struct {
	Command     []string
	ConfigPath  string
	ClusterName string
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
	return gosh.Command(cmd...).UsingProcessGroup()
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
	return k.Kink(ku, chart, release, "delete", "cluster")
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

type ExtraChart struct {
	Chart   helm.ChartFlags
	Release helm.ReleaseFlags
}

type CaseIngressService struct {
	Namespace     string
	Name          string
	HTTPPortName  string
	HTTPSPortName string
	Hostname      string
}

type CaseControlplane struct {
	External bool
	NodePort bool
}

type CaseWordpress struct {
	Set     map[string]string
	Ingress CaseIngressService
}

type Case struct {
	Name         string
	LoadFlags    []string
	Wordpress    CaseWordpress
	ExtraCharts  []ExtraChart
	Controlplane CaseControlplane
}

func (c Case) Run() bool {
	return Describe(c.Name, func() {
		It("should work", func() {
			kinkOpts := KinkFlags{
				Command:     []string{"go", "run", "../main.go"},
				ConfigPath:  filepath.Join("../integration-test", "kink."+c.Name+".config.yaml"),
				ClusterName: c.Name,
			}
			if _, gconfig := GinkgoConfiguration(); gconfig.Verbosity().GTE(gtypes.VerbosityLevelVerbose) {
				kinkOpts.Command = append(kinkOpts.Command, "-v11")
			}
			kinkKubeconfigPath := filepath.Join("../integration-test", "kink."+c.Name+".kubeconfig")

			chart := helm.ChartFlags{
				ChartName: "../helm/kink",
			}
			release := helm.ReleaseFlags{
				Set: map[string]string{
					"image.repository": imageRepo,
					"image.tag":        imageTag,
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
				ExpectRun(kinkOpts.DeleteCluster(
					&kindKubeOpts,
					&chart,
					&release,
				).WithStreams(GinkgoOutErr))
			})

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

			wordpressLoadFlags := make([]string, 0, len(c.LoadFlags)+2)
			wordpressLoadFlags = append(wordpressLoadFlags, c.LoadFlags...)
			wordpressLoadFlags = append(wordpressLoadFlags, "--parallel-loads", "1")
			By("Loading an image from the docker daemon")
			ExpectRun(kinkOpts.LoadDockerImage(
				&kindKubeOpts,
				&chart,
				&release,
				wordpressLoadFlags,
				wordpressImage,
			).WithStreams(GinkgoOutErr))

			By("Loading an image from a docker archive")

			ExpectRun(gosh.Command(docker.Save(&dockerOpts, mariadbImage)...).WithStreams(gosh.FileOut(mariadbTarballPath), GinkgoErr))
			ExpectRun(kinkOpts.LoadDockerArchive(
				&kindKubeOpts,
				&chart,
				&release,
				c.LoadFlags,
				mariadbTarballPath,
			).WithStreams(GinkgoOutErr))

			By("Loading an image from an OCI archive")

			ExpectRun(kinkOpts.LoadOCIArchive(
				&kindKubeOpts,
				&chart,
				&release,
				c.LoadFlags, memcachedTarballPath,
			).WithStreams(GinkgoOutErr))

			if c.Controlplane.External {
				By("Using the external kubeconfig context")
				ExpectRun(gosh.Command(kubectl.Kubectl(&kubectlOpts, &kinkKubeOpts, "config", "use-context", "external")...).WithStreams(GinkgoOutErr))
			} else {
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
			}, "10s", "1s").Should(Succeed())

			By("Watching pods")
			podWatch := gosh.
				Command(kubectl.WatchPods(&kubectlOpts, &kinkKubeOpts, nil, true)...).
				WithStreams(GinkgoOutErr)
			ExpectStart(podWatch)
			DeferCleanup(func() {
				ExpectStop(podWatch)
			})

			for _, chart := range c.ExtraCharts {
				By(fmt.Sprintf("Releasing %s the helm charts", chart.Chart.ChartName))

				ExpectRun(gosh.Command(helm.RepoAdd(&helmOpts, &chart.Chart)...).WithStreams(GinkgoOutErr))
				ExpectRun(gosh.Command(helm.RepoUpdate(&helmOpts, chart.Chart.RepoName())...).WithStreams(GinkgoOutErr))
				ExpectRun(gosh.Command(helm.Upgrade(&helmOpts, &chart.Chart, &chart.Release, &kinkKubeOpts)...).WithStreams(GinkgoOutErr))
			}

			By("Releasing the main helm chart")

			wordpressChart := helm.ChartFlags{
				RepositoryURL: wordpressChartRepo,
				ChartName:     wordpressChartName,
				Version:       wordpressChartVersion,
			}
			wordpressRelease := helm.ReleaseFlags{
				Name:         "wordpress",
				UpgradeFlags: []string{"--debug"},
				Set:          c.Wordpress.Set,
			}
			ExpectRun(gosh.Command(helm.RepoAdd(&helmOpts, &wordpressChart)...).WithStreams(GinkgoOutErr))
			ExpectRun(gosh.Command(helm.RepoUpdate(&helmOpts, wordpressChart.RepoName())...).WithStreams(GinkgoOutErr))
			ExpectRun(gosh.Command(helm.Upgrade(&helmOpts, &wordpressChart, &wordpressRelease, &kinkKubeOpts)...).WithStreams(GinkgoOutErr))

			By("Interacting with the released service")

			func() {
				wordpressPortForward := gosh.Command(kubectl.PortForward(&kubectlOpts, &kinkKubeOpts, "svc/wordpress", map[string]string{"8080": "80"})...).WithStreams(GinkgoOutErr)
				ExpectStart(wordpressPortForward)
				defer ExpectStop(wordpressPortForward)

				Eventually(func() error { _, err := http.Get("http://localhost:8080"); return err }, "10s", "1s").Should(Succeed())
			}()

			if c.Wordpress.Ingress.Name == "" {
				return
			}

			svc := corev1.Service{}
			ExpectRun(gosh.
				Command(kubectl.Kubectl(&kubectlOpts, &kinkKubeOpts, "get", "service", "--namespace", c.Wordpress.Ingress.Namespace, c.Wordpress.Ingress.Name, "-o", "json")...).
				WithStreams(
					gosh.ForwardErr,
					gosh.FuncOut(gosh.SaveJSON(&svc)),
				),
			)

			httpPort := int32(0)
			httpsPort := int32(0)
			for _, port := range svc.Spec.Ports {
				if port.Name == c.Wordpress.Ingress.HTTPPortName {
					httpPort = port.NodePort
				}
				if port.Name == c.Wordpress.Ingress.HTTPSPortName {
					httpsPort = port.NodePort
				}
			}
			func() {
				kubeOpts := kindKubeOpts
				kubeOpts.ConfigOverrides.Context.Namespace = c.Name
				portForward := gosh.
					Command(kubectl.PortForward(&kubectlOpts, &kubeOpts, fmt.Sprintf("svc/kink-%s-lb", c.Name), map[string]string{"8080": fmt.Sprintf("%d", httpPort)})...).
					WithStreams(GinkgoOutErr)
				ExpectStart(portForward)
				defer ExpectStop(portForward)
				req, err := http.NewRequest("GET", "http://localhost:8080", nil)
				Expect(err).ToNot(HaveOccurred())
				req.Header.Set("Host", c.Wordpress.Ingress.Hostname)

				Eventually(func() error { _, err := http.DefaultClient.Do(req); return err }, "10s", "1s").Should(Succeed())
			}()
			func() {
				kubeOpts := kindKubeOpts
				kubeOpts.ConfigOverrides.Context.Namespace = c.Name
				portForward := gosh.
					Command(kubectl.PortForward(&kubectlOpts, &kubeOpts, fmt.Sprintf("svc/kink-%s-lb", c.Name), map[string]string{"8080": fmt.Sprintf("%d", httpsPort)})...).
					WithStreams(GinkgoOutErr)
				ExpectStart(portForward)
				defer ExpectStop(portForward)

				req, err := http.NewRequest("GET", "https://localhost:8080", nil)
				Expect(err).ToNot(HaveOccurred())
				req.Header.Set("Host", c.Wordpress.Ingress.Hostname)
				// TODO: Actually set up a cert for this
				http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
				Eventually(func() error { _, err := http.DefaultClient.Do(req); return err }, "10s", "1s").Should(Succeed())

			}()
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

var _ = Case{
	Name: "k3s",
	Wordpress: CaseWordpress{
		Set: map[string]string{
			"persistence.enabled":        "true",
			"persistence.storageClass":   "shared-local-path",
			"persistence.accessModes":    "{ReadWriteMany}",
			"replicaCount":               "2",
			"podAntiAffinityPreset":      "hard",
			"service.type":               "ClusterIP",
			"image.pullPolicy":           "Never",
			"ingress.enabled":            "true",
			"ingress.hostname":           "wordpress.ingress.local",
			"mariadb.enabled":            "true",
			"mariadb.image.pullPolicy":   "Never",
			"memcached.enabled":          "true",
			"memcached.image.pullPolicy": "Never",
		},
		Ingress: CaseIngressService{
			Namespace:     "default",
			Name:          "ingress-nginx-controller",
			HTTPPortName:  "http",
			HTTPSPortName: "https",
			Hostname:      "wordpress.ingress.local",
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
		},
	},
	Controlplane: CaseControlplane{
		External: true,
	},
}.Run()

var _ = Case{
	Name:      "k3s-single",
	LoadFlags: []string{"--only-load-workers=false"},
	Wordpress: CaseWordpress{
		Set: map[string]string{
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
		},
	},
}.Run()

var _ = Case{
	Name: "k3s-ha",
	Wordpress: CaseWordpress{
		Set: map[string]string{
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
		},
	},
	Controlplane: CaseControlplane{
		External: true,
		NodePort: true,
	},
}.Run()

var _ = Case{
	Name: "rke2",
	Wordpress: CaseWordpress{
		Set: map[string]string{
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
		},
	},
}.Run()
