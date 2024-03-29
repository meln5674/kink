package e2e_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	gtypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"

	"github.com/meln5674/gosh"

	"github.com/meln5674/kink/pkg/flags"
	"github.com/meln5674/kink/pkg/helm"
	"github.com/meln5674/kink/pkg/kubectl"

	"github.com/meln5674/gingk8s"

	"github.com/meln5674/k8s-smoke-test/pkg/test"
	k8ssmoketest "github.com/meln5674/k8s-smoke-test/pkg/test"
)

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2e Suite")
}

var (
	GinkgoOutErr = gingk8s.GinkgoOutErr

	repoRoot = os.Getenv("KINK_IT_REPO_ROOT")

	gocoverdir = os.Getenv("GOCOVERDIR")

	runningInCluster = os.Getenv("KINK_IT_IN_CLUSTER") != ""

	localbin     = os.Getenv("LOCALBIN")
	localKubectl = gingk8s.KubectlCommand{
		Command: []string{filepath.Join(localbin, "kubectl")},
	}
	localHelm = gingk8s.HelmCommand{
		Command: []string{filepath.Join(localbin, "helm")},
	}
	localKind = gingk8s.KindCommand{
		Command: []string{filepath.Join(localbin, "kind")},
	}
	localDocker = gingk8s.DockerCommand{}

	devMode = os.Getenv("KINK_IT_DEV_MODE") != ""
	isCI    = os.Getenv("IS_CI") != ""

	gk8s     gingk8s.Gingk8s
	gk8sOpts = gingk8s.SuiteOpts{
		// KLogFlags:      []string{"-v=6"},
		Kubectl:        &localKubectl,
		Helm:           &localHelm,
		Manifests:      &localKubectl,
		NoSuiteCleanup: devMode,
		NoSpecCleanup:  devMode,
		NoCacheImages:  isCI,
		NoPull:         isCI,
		NoLoadPulled:   isCI,
		NoDeps:         false,
	}

	kindCluster = gingk8s.KindCluster{
		Name:                   "kink-it",
		KindCommand:            &localKind,
		TempDir:                filepath.Join(repoRoot, "integration-test/kind"),
		ConfigFileTemplatePath: filepath.Join(repoRoot, "integration-test/kind/config.yaml.tpl"),
		ConfigFilePath:         filepath.Join(repoRoot, "integration-test/kind/config.yaml"),
	}
	inClusterCluster = gingk8s.DummyCluster{}
	kindClusterID    gingk8s.ClusterID

	hostCluster gingk8s.Cluster

	certManagerImages = []*gingk8s.ThirdPartyImage{
		&gingk8s.ThirdPartyImage{Name: "quay.io/jetstack/cert-manager-cainjector:v1.11.1"},
		&gingk8s.ThirdPartyImage{Name: "quay.io/jetstack/cert-manager-controller:v1.11.1"},
		&gingk8s.ThirdPartyImage{Name: "quay.io/jetstack/cert-manager-webhook:v1.11.1"},
	}
	certManager = gingk8s.HelmRelease{
		Name: "cert-manager",
		Chart: &gingk8s.HelmChart{
			RemoteChartInfo: gingk8s.RemoteChartInfo{
				Repo: &gingk8s.HelmRepo{
					Name: "jetstack",
					URL:  "https://charts.jetstack.io",
				},
				Name:    "cert-manager",
				Version: "v1.11.1",
			},
		},
		Set: gingk8s.Object{
			"installCRDs":        true,
			"prometheus.enabled": false,
		},
		Wait: []gingk8s.WaitFor{
			{
				Resource: "deploy/cert-manager",
				For:      map[string]string{"condition": "Available"},
			},
			{
				Resource: "deploy/cert-manager-webhook",
				For:      map[string]string{"condition": "Available"},
			},
			{
				Resource: "deploy/cert-manager-cainjector",
				For:      map[string]string{"condition": "Available"},
			},
		},
	}
	secureProxyRegistryCerts = gingk8s.KubernetesManifests{
		Name: "Secure Proxy Registry Certificates",
		Resources: []string{`
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
`,
			`
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: secure-proxy-registry-cert
spec:
  commonName: 'secure-proxy-registry.default.svc.cluster.local'
  secretName: secure-proxy-registry-cert
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: selfsigned-issuer
    kind: Issuer
    group: cert-manager.io
  dnsNames:
  - secure-proxy-registry.default.svc.cluster.local
  - secure-proxy-registry.default
  - secure-proxy-registry
`,
		},
	}

	twuniRepo = gingk8s.HelmRepo{
		Name: "twuni",
		URL:  "https://helm.twun.io",
	}
	dockerRegistryChart = gingk8s.HelmChart{
		RemoteChartInfo: gingk8s.RemoteChartInfo{
			Repo:    &twuniRepo,
			Name:    "docker-registry",
			Version: "v2.2.2",
		},
	}
	dockerRegistryImage = gingk8s.ThirdPartyImage{
		Name: "docker.io/library/registry:2.7.1",
	}
	proxyRegistry = gingk8s.HelmRelease{
		Name:      "proxy-registry",
		Namespace: "default",
		Chart:     &dockerRegistryChart,
		Set: gingk8s.Object{
			"persistence.enabled":        true,
			"service.type":               "NodePort",
			"configData.proxy.remoteurl": "https://registry-1.docker.io",
			"fullnameOverride":           "proxy-registry",
		},
		Wait: []gingk8s.WaitFor{{
			Resource: "deploy/proxy-registry",
			For: gingk8s.StringObject{
				"condition": "Available=true",
			},
		}},
	}
	secureProxyRegistry = gingk8s.HelmRelease{
		Name:      "secure-proxy-registry",
		Namespace: "default",
		Chart:     &dockerRegistryChart,
		Set: gingk8s.Object{
			"persistence.enabled":        true,
			"service.type":               "NodePort",
			"configData.proxy.remoteurl": "http://proxy-registry:5000",
			"fullnameOverride":           "secure-proxy-registry",
			"tlsSecretName":              "secure-proxy-registry-cert",
			"secrets.htpasswd":           "totally-secure-username:$2y$05$U4s6hE0MMiJ268PfzZBS3udps3xiPGQIirCCRy24P6UkUbo0XGxBe",
		},
		Wait: []gingk8s.WaitFor{{
			Resource: "deploy/secure-proxy-registry",
			For: gingk8s.StringObject{
				"condition": "Available=true",
			},
		}},
	}
	dockerRegistry = gingk8s.HelmRelease{
		Name:      "docker-registry",
		Namespace: "default",
		Chart:     &dockerRegistryChart,
		Set: gingk8s.Object{
			"persistence.enabled": true,
			"service.type":        "NodePort",
		},
		Wait: []gingk8s.WaitFor{{
			Resource: "deploy/docker-registry",
			For: gingk8s.StringObject{
				"condition": "Available=true",
			},
		}},
	}

	chartmuseumRepo = gingk8s.HelmRepo{
		Name: "chartmuseum",
		URL:  "https://chartmuseum.github.io/charts",
	}
	chartmuseumChart = gingk8s.HelmChart{
		RemoteChartInfo: gingk8s.RemoteChartInfo{
			Repo:    &chartmuseumRepo,
			Name:    "chartmuseum",
			Version: "3.10.1",
		},
	}
	chartmuseumImage = gingk8s.ThirdPartyImage{
		Name: "ghcr.io/helm/chartmuseum:v0.13.1",
	}
	chartmuseum = gingk8s.HelmRelease{
		Name:      "chartmuseum",
		Namespace: "default",
		Chart:     &chartmuseumChart,
		Set: gingk8s.Object{
			"persistence.enabled":  true,
			"service.type":         "NodePort",
			"env.open.DISABLE_API": false,
		},
		Wait: []gingk8s.WaitFor{{
			Resource: "deploy/chartmuseum",
			For: gingk8s.StringObject{
				"condition": "Available=true",
			},
		}},
	}

	ingressNginxRepo = gingk8s.HelmRepo{
		Name: "ingress-nginx",
		URL:  "https://kubernetes.github.io/ingress-nginx",
	}
	ingressNginxChart = gingk8s.HelmChart{
		RemoteChartInfo: gingk8s.RemoteChartInfo{
			Repo:    &ingressNginxRepo,
			Name:    "ingress-nginx",
			Version: "4.6.0",
		},
	}
	ingressNginxImage = gingk8s.ThirdPartyImage{
		Name: "registry.k8s.io/ingress-nginx/controller:v1.7.0",
	}
	ingressNginx = gingk8s.HelmRelease{
		Name:      "ingress-nginx",
		Namespace: "default",
		Chart:     &ingressNginxChart,
		Set: gingk8s.Object{
			"controller.kind":                             "DaemonSet",
			"controller.service.type":                     "ClusterIP",
			"controller.hostPort.enabled":                 "true",
			"controller.extraArgs.enable-ssl-passthrough": "true",
		},
	}
	ingressNginxInner = gingk8s.HelmRelease{
		Name:       "ingress-nginx",
		Namespace:  "default",
		Chart:      &ingressNginxChart,
		SkipDelete: true,
	}
	ingressNginxInnerDS = gingk8s.HelmRelease{
		Name:      "ingress-nginx",
		Namespace: "default",
		Chart:     &ingressNginxChart,
		Set: gingk8s.Object{
			"controller.kind":             "DaemonSet",
			"controller.hostPort.enabled": "true",
		},
		// I don't know why, but deleting this chart fails consistently on the cases that use port-forwarding for the controlplane.
		// Need to investigate if it is related to the nodes using hostports, or if that's just a coincidence.
		// In any case, not deleting it lets the tests pass
		SkipDelete: true,
	}

	/*
		localPathProvisionerImage = gingk8s.CustomImage{
			Registry:   "local.host",
			Repository: "meln5674/local-path-provisioner",
			Dockerfile: filepath.Join(repoRoot, "charts/local-path-provisioner/package/Dockerfile"),
			ContextDir: filepath.Join(repoRoot, "charts/local-path-provisioner"),
		}
	*/
	sharedLocalPathProvisionerMount = "/opt/shared-local-path-provisioner"
	sharedLocalPathProvisioner      = gingk8s.HelmRelease{
		Name:      "local-path-provisioner",
		Namespace: "kube-system",
		Chart: &gingk8s.HelmChart{
			LocalChartInfo: gingk8s.LocalChartInfo{
				Path: filepath.Join(repoRoot, "charts/local-path-provisioner-0.0.24-dev.tgz"),
			},
		},
		Set: gingk8s.Object{
			"storageClass.name":    "shared-local-path",
			"nodePathMap":          "null",
			"sharedFileSystemPath": sharedLocalPathProvisionerMount,
			"fullnameOverride":     "shared-local-path-provisioner",
			"configmap.name":       "shared-local-path-provisioner",
		},
	}
	kinkImage = gingk8s.CustomImage{
		Registry:   "local.host",
		Repository: "meln5674/kink",
		Dockerfile: filepath.Join(repoRoot, "standalone.Dockerfile"),
		ContextDir: repoRoot,
		BuildArgs: map[string]string{
			"KINK_BINARY": "bin/kink.cover",
		},
	}

	k8sSmokeTestVersion    = "v0.2.0"
	k8sSmokeTestRegistry   = "ghcr.io"
	k8sSmokeTestRepository = "meln5674/k8s-smoke-test"
	k8sSmokeTestChart      = gingk8s.HelmChart{
		OCIChartInfo: gingk8s.OCIChartInfo{
			Registry: gingk8s.HelmRegistry{
				Hostname: k8sSmokeTestRegistry,
			},
			Repository: k8sSmokeTestRepository + "/charts/k8s-smoke-test",
			Version:    k8sSmokeTestVersion,
		},
	}

	k8sSmokeTestDeploymentImage = gingk8s.ThirdPartyImage{
		Name: k8sSmokeTestRegistry + "/" + k8sSmokeTestRepository + "/deployment" + ":" + k8sSmokeTestVersion,
	}
	k8sSmokeTestStatefulSetImage = gingk8s.ThirdPartyImage{
		Name: k8sSmokeTestRegistry + "/" + k8sSmokeTestRepository + "/statefulset" + ":" + k8sSmokeTestVersion,
	}
	k8sSmokeTestStatefulSetImageArchive = gingk8s.ImageArchive{
		Name:   k8sSmokeTestStatefulSetImage.Name,
		Path:   filepath.Join(repoRoot, "integration-test/k8s-smoke-test-statefulset.tar"),
		Format: gingk8s.DockerImageFormat,
		NoPull: true,
	}

	k8sSmokeTestJobImageArchive = gingk8s.ImageArchive{
		Name:   k8sSmokeTestRegistry + "/" + k8sSmokeTestRepository + "/job" + ":" + k8sSmokeTestVersion,
		Path:   filepath.Join(repoRoot, "integration-test/k8s-smoke-test-job.tar"),
		Format: gingk8s.OCIImageFormat,
	}

	buildEnvImage = gingk8s.CustomImage{
		Registry:   "local.host",
		Repository: "meln5674/kink/build-env",
		ContextDir: repoRoot,
		Dockerfile: filepath.Join(repoRoot, "build-env.Dockerfile"),
	}
	buildEnvChart = gingk8s.HelmChart{
		LocalChartInfo: gingk8s.LocalChartInfo{
			Path: filepath.Join(repoRoot, "helm/kink-test"),
		},
	}
	buildEnv = gingk8s.HelmRelease{
		Name:         "kink-build-env",
		Chart:        &buildEnvChart,
		UpgradeFlags: []string{"--create-namespace"},
		Set: gingk8s.Object{
			"gocoverage.dir": gocoverdir,

			"testOpts.ci":      isCI,
			"testOpts.devMode": devMode,

			"sourceMount.containerPath": filepath.Dir(repoRoot),

			"image.repository": buildEnvImage.WithTag(""),
			"image.tag":        gingk8s.DefaultExtraCustomImageTags[0],
			"kink.image.tag":   gingk8s.DefaultExtraCustomImageTags[0],

			"fullnameOverride": "kink-build-env",
		},
		SetJSON: gingk8s.NestedObject{
			"testOpts.state": &state,
		},
	}

	watchPods      = gingk8s.KubectlWatcher{Kind: "pod", Flags: []string{"-A"}}
	watchServices  = gingk8s.KubectlWatcher{Kind: "service", Flags: []string{"-A"}}
	watchEndpoints = gingk8s.KubectlWatcher{Kind: "endpoints", Flags: []string{"-A"}}
	watchIngresses = gingk8s.KubectlWatcher{Kind: "ingress", Flags: []string{"-A"}}
	watchPVCs      = gingk8s.KubectlWatcher{Kind: "pvc", Flags: []string{"-A"}}

	HTTP http.Client

	state suiteState
)

type suiteState struct {
	KindIP             string
	ChartVersion       string
	ChartmuseumPort    string
	DockerRegistryPort string
	Gingk8s            []byte
	KinkImageTag       string
}

var _ = SynchronizedBeforeSuite(beforeSuiteGlobal, beforeSuiteLocal)

func kindClusterSetup(ctx context.Context) {
	if !gk8sOpts.NoSuiteCleanup {
		DeferCleanup(CleanupPVCDirs)
	}

	state.ChartVersion = fmt.Sprintf("0.1.0-%s", gingk8s.DefaultExtraCustomImageTags[0])
	ExpectRun(localHelm.
		Helm(
			ctx, kindCluster.GetConnection(),
			"package", "../helm/kink",
			"--destination", filepath.Join(repoRoot, "bin"),
			"--version", state.ChartVersion,
		),
	)
	chartTarballPath := filepath.Join(repoRoot, fmt.Sprintf("bin/kink-%s.tgz", state.ChartVersion))

	kinkImageID := gk8s.CustomImage(&kinkImage)
	buildEnvImageID := gk8s.CustomImage(&buildEnvImage)
	ingressNginxImageID := gk8s.ThirdPartyImage(&ingressNginxImage)
	//gk8s.CustomImage(&localPathProvisionerImage)
	gk8s.ThirdPartyImage(&k8sSmokeTestDeploymentImage)
	gk8s.ThirdPartyImage(&k8sSmokeTestStatefulSetImage)
	dockerRegistryImageID := gk8s.ThirdPartyImage(&dockerRegistryImage)
	gk8s.ImageArchive(&k8sSmokeTestJobImageArchive)
	certManagerImageIDs := gk8s.ThirdPartyImages(certManagerImages...)
	kindClusterID = gk8s.Cluster(&kindCluster, kinkImageID, ingressNginxImageID, dockerRegistryImageID, buildEnvImageID, certManagerImageIDs)
	getKindIPID := gk8s.ClusterAction(kindClusterID, "Get KinD cluster IP", gingk8s.ClusterAction(func(g gingk8s.Gingk8s, ctx context.Context, c gingk8s.Cluster) error {
		err := localDocker.
			Docker(
				ctx,
				"inspect", fmt.Sprintf("%s-control-plane", kindCluster.Name),
				"-f", "{{ .NetworkSettings.Networks.kind.IPAddress }}",
			).
			WithStreams(gosh.FuncOut(gosh.SaveString(&state.KindIP))).
			Run()
		state.KindIP = strings.TrimSpace(state.KindIP)
		return err
	}))

	gk8s.Release(kindClusterID, &sharedLocalPathProvisioner)

	ingressNginxID := gk8s.Release(kindClusterID, &ingressNginx, ingressNginxImageID)
	gk8s.ClusterAction(kindClusterID, "Wait for ingress nginx", gingk8s.ClusterAction(func(g gingk8s.Gingk8s, ctx context.Context, c gingk8s.Cluster) error {
		return gk8s.Kubectl(ctx, c, "rollout", "status", "ds/ingress-nginx-controller").Run()
	}), ingressNginxID)

	gk8s.Release(kindClusterID, &proxyRegistry, dockerRegistryImageID)
	certManagerID := gk8s.Release(kindClusterID, &certManager, certManagerImageIDs)
	secureProxyRegistryCertsID := gk8s.Manifests(kindClusterID, &secureProxyRegistryCerts, certManagerID)
	gk8s.Release(kindClusterID, &secureProxyRegistry, dockerRegistryImageID, secureProxyRegistryCertsID)

	chartmuseumID := gk8s.Release(kindClusterID, &chartmuseum)
	gk8s.ClusterAction(
		kindClusterID, "Push chart to Chart Museum",
		gingk8s.ClusterAction(func(g gingk8s.Gingk8s, ctx context.Context, c gingk8s.Cluster) error {
			err := gk8s.
				Kubectl(ctx, c,
					"get", "svc/chartmuseum",
					"--template", "{{ (index .spec.ports 0).nodePort }}",
				).
				WithStreams(gosh.FuncOut(gosh.SaveString(&state.ChartmuseumPort))).
				Run()
			if err != nil {
				return err
			}

			By("Uploading chart to chartmuseum")
			Eventually(func() error {
				tarball, err := os.Open(chartTarballPath)
				if err != nil {
					return err
				}
				defer tarball.Close()
				resp, err := http.Post(fmt.Sprintf("http://%s:%s/api/charts", state.KindIP, state.ChartmuseumPort), "application/octet-stream", tarball)
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				_, err = io.Copy(GinkgoWriter, resp.Body)
				if resp.StatusCode != http.StatusCreated {
					return fmt.Errorf("Status code %d", resp.StatusCode)
				}
				return nil
			}, "30s", "500ms").Should(Succeed())
			return nil
		}),
		chartmuseumID,
		getKindIPID,
	)

	dockerRegistryID := gk8s.Release(kindClusterID, &dockerRegistry)
	gk8s.ClusterAction(
		kindClusterID, "Push chart to OCI registry",
		gingk8s.ClusterAction(func(g gingk8s.Gingk8s, ctx context.Context, c gingk8s.Cluster) error {
			err := gk8s.
				Kubectl(ctx, c,
					"get", "svc/docker-registry",
					"--template", "{{ (index .spec.ports 0).nodePort }}",
				).
				WithStreams(gosh.FuncOut(gosh.SaveString(&state.DockerRegistryPort))).
				Run()
			if err != nil {
				return err
			}
			Eventually(
				localHelm.
					Helm(
						ctx, c.GetConnection(),
						"push", chartTarballPath, fmt.Sprintf("oci://%s:%s/charts", state.KindIP, state.DockerRegistryPort),
						"--plain-http",
					).
					Run(),
				"15s", "1s",
			).Should(Succeed())
			return nil
		}),
		dockerRegistryID,
		getKindIPID,
	)

	gk8s.ClusterAction(kindClusterID, "Watch Pods", &watchPods)
	gk8s.ClusterAction(kindClusterID, "Watch Endpoints", &watchEndpoints)
	gk8s.ClusterAction(kindClusterID, "Watch Services", &watchServices)
	gk8s.ClusterAction(kindClusterID, "Watch Ingresses", &watchIngresses)
	gk8s.ClusterAction(kindClusterID, "Watch PVCs", &watchPVCs)
	state.KinkImageTag = gingk8s.DefaultExtraCustomImageTags[0]
	hostCluster = &kindCluster
}

func inClusterSetup() {
	Expect(json.Unmarshal([]byte(os.Getenv("KINK_IT_STATE")), &state)).To(Succeed())

	kindClusterID = gk8s.Cluster(&inClusterCluster)
	hostCluster = &inClusterCluster
	gingk8s.DefaultExtraCustomImageTags[0] = state.KinkImageTag
}

func beforeSuiteGlobal(ctx context.Context) []byte {
	gosh.GlobalLog = GinkgoLogr
	gosh.CommandLogLevel = 0

	gk8s = gingk8s.ForSuite(GinkgoT())
	gk8s.Options(gk8sOpts)

	if runningInCluster {
		By("Connecting to cluster from within")
		inClusterSetup()
	} else {
		By("Building host cluster")
		kindClusterSetup(ctx)
	}

	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	gk8s.Setup(ctx)

	if !gk8sOpts.NoPull {
		save, _ := localDocker.Save(ctx, []string{k8sSmokeTestStatefulSetImageArchive.Name}, k8sSmokeTestStatefulSetImageArchive.Path)
		ExpectRun(save)
	}

	state.Gingk8s = gk8s.Serialize(kindClusterID)
	out, err := json.Marshal(&state)
	Expect(err).ToNot(HaveOccurred())

	return out
}

func beforeSuiteLocal(ctx context.Context, in []byte) {
	gosh.GlobalLog = GinkgoLogr
	gosh.CommandLogLevel = 0

	err := json.Unmarshal(in, &state)
	Expect(err).ToNot(HaveOccurred())

	gk8s.Deserialize(state.Gingk8s, GinkgoT(), &kindClusterID)
	gk8s.Options(gk8sOpts)

	klog.InitFlags(flag.CommandLine)
	if _, gconfig := GinkgoConfiguration(); gconfig.Verbosity().GTE(gtypes.VerbosityLevelVerbose) {
		flag.Set("v", "11")
		klog.SetOutput(GinkgoWriter)
	}
	HTTP := *http.DefaultClient
	HTTP.Transport = transport.DebugWrappers(http.DefaultTransport)
	/*
		http.DefaultClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	*/

	if runningInCluster {
		gingk8s.DefaultExtraCustomImageTags[0] = state.KinkImageTag
		hostCluster = &inClusterCluster
	} else {
		hostCluster = &kindCluster
	}

}

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

type KindOpts struct {
	KindCommand       []string
	KubeconfigOutPath string
	ClusterName       string
}

type KinkFlags struct {
	Command     []string
	ConfigPath  string
	ClusterName string
	Env         map[string]string

	ControlplanePortForwardPort int
	FileGatewayPortForwardPort  int

	LogLevel int

	InCluster bool
}

func (k *KinkFlags) Kink(ctx context.Context, ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, args ...string) *gosh.Cmd {
	cmd := make([]string, 0, len(k.Command)+len(args))
	cmd = append(cmd, k.Command...)
	cmd = append(cmd, fmt.Sprintf("-v%d", k.LogLevel))
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
	if chart.PlainHTTP {
		cmd = append(cmd, "--registry-plain-http")
	}
	cmd = append(cmd, release.ValuesFlags()...)
	cmd = append(cmd, args...)
	command := gosh.Command(cmd...).
		WithContext(ctx).
		UsingProcessGroup().
		WithWorkingDir(repoRoot).
		WithStreams(GinkgoOutErr)
	if k.Env != nil {
		command = command.WithParentEnvAnd(k.Env)
	}
	return command
}

func (k *KinkFlags) portForwardFlags() []string {
	args := make([]string, 0, 2)
	if k.ControlplanePortForwardPort != 0 {
		args = append(args, fmt.Sprintf("--controlplane-port=%d", k.ControlplanePortForwardPort))
	}
	if k.FileGatewayPortForwardPort != 0 {
		args = append(args, fmt.Sprintf("--file-gateway-port=%d", k.FileGatewayPortForwardPort))
	}
	return args
}

func (k *KinkFlags) exportKubeconfigFlags() []string {
	args := k.portForwardFlags()
	if k.InCluster {
		args = append(args, "--in-cluster")
	}
	return args
}

func (k *KinkFlags) CreateCluster(ctx context.Context, ku *kubectl.KubeFlags, targetKubeconfigPath string, controlplaneIngressURL string, chart *helm.ChartFlags, release *helm.ReleaseFlags) *gosh.Cmd {
	args := []string{"create", "cluster"}
	if targetKubeconfigPath != "" {
		args = append(args, "--out-kubeconfig", targetKubeconfigPath)
	}
	if controlplaneIngressURL != "" {
		args = append(args, "--controlplane-ingress-url", controlplaneIngressURL)
	}
	args = append(args, k.exportKubeconfigFlags()...)
	args = append(args, release.UpgradeFlags...)
	return k.Kink(ctx, ku, chart, release, args...)
}

func (k *KinkFlags) DeleteCluster(ctx context.Context, ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags) *gosh.Cmd {
	return k.Kink(ctx, ku, chart, release, "delete", "cluster", "--delete-pvcs")
}

func (k *KinkFlags) Shell(ctx context.Context, ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, script string) *gosh.Cmd {
	args := []string{"sh"}
	args = append(args, k.exportKubeconfigFlags()...)
	args = append(args, "--", script)
	return k.Kink(ctx, ku, chart, release, args...)
}

func (k *KinkFlags) Load(ctx context.Context, ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, typ string, flags []string, flag string, items ...string) gosh.Commander {
	args := []string{"load", typ}
	args = append(args, flags...)
	for _, item := range items {
		args = append(args, "--"+flag, item)
	}
	// ctr is unreliable, so we retry up to 5 times
	return gosh.Or(
		k.Kink(ctx, ku, chart, release, args...),
		k.Kink(ctx, ku, chart, release, args...),
		k.Kink(ctx, ku, chart, release, args...),
		k.Kink(ctx, ku, chart, release, args...),
		k.Kink(ctx, ku, chart, release, args...),
	)
}

func (k *KinkFlags) LoadDockerImage(ctx context.Context, ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, flags []string, images ...string) gosh.Commander {
	return k.Load(ctx, ku, chart, release, "docker-image", flags, "image", images...)
}

func (k *KinkFlags) LoadDockerArchive(ctx context.Context, ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, flags []string, archives ...string) gosh.Commander {
	return k.Load(ctx, ku, chart, release, "docker-archive", flags, "archive", archives...)
}

func (k *KinkFlags) LoadOCIArchive(ctx context.Context, ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, flags []string, archives ...string) gosh.Commander {
	return k.Load(ctx, ku, chart, release, "oci-archive", flags, "archive", archives...)
}

func (k *KinkFlags) PortForward(ctx context.Context, ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags) *gosh.Cmd {
	args := []string{"port-forward"}
	args = append(args, k.portForwardFlags()...)
	return k.Kink(ctx, ku, chart, release, args...)
}

func (k *KinkFlags) FileGatewaySend(ctx context.Context, ku *kubectl.KubeFlags, chart *helm.ChartFlags, release *helm.ReleaseFlags, flags []string, paths ...string) *gosh.Cmd {
	args := []string{"file-gateway", "send"}
	args = append(args, k.exportKubeconfigFlags()...)
	args = append(args, flags...)
	args = append(args, paths...)
	return k.Kink(ctx, ku, chart, release, args...)
}

type KinkCluster struct {
	KinkFlags              KinkFlags
	KubectlFlags           kubectl.KubectlFlags
	KubeFlags              kubectl.KubeFlags
	ChartFlags             helm.ChartFlags
	ReleaseFlags           helm.ReleaseFlags
	ControlplaneIngressURL string
	TempDir                string
	Namespace              string
	LoadImageFlags         []string
	LoadArchiveFlags       []string
}

func (k *KinkCluster) KubeconfigPath() string {
	return filepath.Join(k.TempDir, "kubeconfig")
}

func (k *KinkCluster) Create(ctx context.Context, skipExisting bool) gosh.Commander {
	rolloutFlags := []string{}
	if k.Namespace != "" {
		rolloutFlags = append(rolloutFlags, "-n", k.Namespace)
	}
	return gosh.And(
		k.KinkFlags.CreateCluster(ctx, &k.KubeFlags, k.KubeconfigPath(), k.ControlplaneIngressURL, &k.ChartFlags, &k.ReleaseFlags),
		gosh.FanOut(
			gosh.
				Command(kubectl.Kubectl(
					&k.KubectlFlags,
					&k.KubeFlags,
					append([]string{"rollout", "status", fmt.Sprintf("sts/kink-%s-controlplane", k.KinkFlags.ClusterName)}, rolloutFlags...)...,
				)...),
			gosh.
				Command(kubectl.Kubectl(
					&k.KubectlFlags,
					&k.KubeFlags,
					append([]string{"rollout", "status", fmt.Sprintf("sts/kink-%s-worker", k.KinkFlags.ClusterName)}, rolloutFlags...)...,
				)...),
			gosh.
				Command(kubectl.Kubectl(
					&k.KubectlFlags,
					&k.KubeFlags,
					append([]string{"rollout", "status", fmt.Sprintf("deploy/kink-%s-lb-manager", k.KinkFlags.ClusterName)}, rolloutFlags...)...,
				)...),
		),
	).
		WithStreams(gingk8s.GinkgoOutErr)
}

func (k *KinkCluster) GetConnection() *gingk8s.KubernetesConnection {
	return &gingk8s.KubernetesConnection{
		Kubeconfig: k.KubeconfigPath(),
	}
}

func (k *KinkCluster) GetTempDir() string {
	return k.TempDir
}

func (k *KinkCluster) GetName() string {
	return k.KinkFlags.ClusterName
}

func (k *KinkCluster) LoadImages(ctx context.Context, from gingk8s.Images, format gingk8s.ImageFormat, images []string, noCache bool) gosh.Commander {
	return k.KinkFlags.LoadDockerImage(ctx, &k.KubeFlags, &k.ChartFlags, &k.ReleaseFlags, k.LoadImageFlags, images...)
}

func (k *KinkCluster) LoadImageArchives(ctx context.Context, format gingk8s.ImageFormat, archives []string) gosh.Commander {
	switch format {
	case gingk8s.DockerImageFormat:
		return k.KinkFlags.LoadDockerArchive(ctx, &k.KubeFlags, &k.ChartFlags, &k.ReleaseFlags, k.LoadArchiveFlags, archives...)
	case gingk8s.OCIImageFormat:
		return k.KinkFlags.LoadOCIArchive(ctx, &k.KubeFlags, &k.ChartFlags, &k.ReleaseFlags, k.LoadArchiveFlags, archives...)
	default:
		panic("Bad image format")
	}
}

func (k *KinkCluster) Delete(ctx context.Context) gosh.Commander {
	return k.KinkFlags.DeleteCluster(ctx, &k.KubeFlags, &k.ChartFlags, &k.ReleaseFlags).WithStreams(GinkgoOutErr)
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
	Set     gingk8s.Object
	Ingress CaseIngressService
}

type Case struct {
	Name               string
	LoadFlags          []string
	SmokeTest          CaseSmokeTest
	Controlplane       CaseControlplane
	FileGatewayEnabled bool
	GetChart           func() helm.ChartFlags
	InCluster          bool

	ExtraClusterSetup func(gingk8s.Gingk8s, gingk8s.ClusterID, []gingk8s.ResourceDependency) []gingk8s.ResourceDependency

	Focus    bool
	Disabled bool
}

type Void struct{}

var void = struct{}{}

func (c Case) Create() bool {
	if c.Disabled {
		return false
	}
	if runningInCluster && !c.InCluster {
		return false
	}
	if runningInCluster && os.Getenv("KINK_IT_IN_CLUSTER_CASE") != c.Name {
		return false
	}
	f := func() {
		if !runningInCluster && c.InCluster {
			It("should work when run within the cluster", func() {
				gk8s = gk8s.ForSpec()

				namespacedBuildEnv := buildEnv
				namespacedBuildEnv.Namespace = c.Name
				buildEnvID := gk8s.Release(kindClusterID, &namespacedBuildEnv)
				rolloutID := gk8s.ClusterAction(
					kindClusterID,
					fmt.Sprintf("Rollout %s build-env", c.Name),
					gingk8s.ClusterAction(func(gk8s gingk8s.Gingk8s, ctx context.Context, _ gingk8s.Cluster) error {
						Expect(gk8s.Kubectl(
							ctx, hostCluster,
							"-n", c.Name,
							"rollout", "status", "deploy/kink-build-env",
						).Run()).To(Succeed())

						return nil
					}),
					buildEnvID,
				)

				copySecretID := gk8s.ClusterAction(
					kindClusterID, "Copy secure proxy registry cert",
					gingk8s.ClusterAction(func(gk8s gingk8s.Gingk8s, ctx context.Context, _ gingk8s.Cluster) error {
						Eventually(func() error {
							gk8s.
								Kubectl(ctx, hostCluster, "delete", "secret", "secure-proxy-registry-cert", "-n", c.Name).
								Run()
							var secret map[string]interface{}
							err := gk8s.
								Kubectl(ctx, hostCluster, "get", "secret", "-o", "json", "secure-proxy-registry-cert").
								WithStreams(gosh.FuncOut(gosh.SaveJSON(&secret))).
								Run()
							if err != nil {
								return err
							}
							secret["metadata"].(map[string]interface{})["namespace"] = c.Name
							secret["type"] = "Opaque"
							delete(secret["data"].(map[string]interface{}), "tls.key")
							err = gk8s.
								Kubectl(ctx, hostCluster, "create", "-f", "-").
								WithStreams(gosh.FuncIn(gosh.JSONIn(&secret))).
								Run()
							if err != nil {
								return err
							}
							return nil
						}, "30s", "5s").Should(Succeed())
						return nil
					}),
				)

				gk8s.ClusterAction(
					kindClusterID, "Execute case in cluster",
					gingk8s.ClusterAction(func(gk8s gingk8s.Gingk8s, ctx context.Context, _ gingk8s.Cluster) error {
						return gk8s.KubectlExec(ctx, hostCluster, "deploy/kink-build-env", "make", []string{"e2e", fmt.Sprintf("KINK_IT_IN_CLUSTER_CASE=%s", c.Name)}, "-n", c.Name).Run()
					}),
					rolloutID, copySecretID,
				)

				ctx, cancel := context.WithCancel(context.Background())
				DeferCleanup(cancel)
				gk8s.Setup(ctx)
			})

			return
		}

		var kinkCluster KinkCluster
		var kinkOpts KinkFlags
		var release helm.ReleaseFlags
		var chart helm.ChartFlags
		var kindKubeOpts kubectl.KubeFlags
		var kinkClusterID gingk8s.ClusterID
		BeforeEach(func(ctx context.Context) {
			gingk8s.WithRandomPorts[Void](3, func(randPorts []int) Void {
				gk8s = gk8s.ForSpec()

				Expect(gocoverdir).ToNot(BeEmpty(), "GOCOVERDIR was not set")

				gocoverdirArgs := []string{
					"--set", "extraVolumes[0].name=src",
					"--set", "extraVolumes[0].hostPath.path=/src/kink/kink",
					"--set", "extraVolumes[0].hostPath.type=Directory",

					"--set", "extraVolumeMounts[0].name=src",
					"--set", "extraVolumeMounts[0].mountPath=" + repoRoot,

					"--set", "extraEnv[0].name=GOCOVERDIR",
					"--set", "extraEnv[0].value=" + gocoverdir,
				}

				kindKubeOpts = kubectl.KubeFlags{}
				if !c.InCluster {
					kindKubeOpts.Kubeconfig = filepath.Join(repoRoot, "integration-test/kind/kubeconfig") // TODO: Get this from sync state
				}

				kinkOpts = KinkFlags{
					//Command:     []string{"go", "run", "../main.go"},
					Command:     append([]string{filepath.Join(repoRoot, "bin/kink.cover")}, gocoverdirArgs...),
					ConfigPath:  filepath.Join(repoRoot, "integration-test/kink/", c.Name, "config.yaml"),
					ClusterName: c.Name,

					ControlplanePortForwardPort: randPorts[0],
					FileGatewayPortForwardPort:  randPorts[1],

					InCluster: c.InCluster,

					LogLevel: 10,
				}
				/*
					if _, gconfig := GinkgoConfiguration(); gconfig.Verbosity().GTE(gtypes.VerbosityLevelVerbose) {
						kinkOpts.Command = append(kinkOpts.Command, "-v11")
					}
				*/

				chart = c.GetChart()
				release = helm.ReleaseFlags{
					Set: map[string]string{
						"image.repository":          kinkImage.WithTag(""),
						"image.tag":                 state.KinkImageTag,
						"controlplane.nodeportHost": state.KindIP,
					},
				}

				kinkCluster = KinkCluster{
					KinkFlags: kinkOpts,
					KubectlFlags: kubectl.KubectlFlags{
						Command: localKubectl.Command,
					},
					KubeFlags:              kindKubeOpts,
					ChartFlags:             chart,
					ReleaseFlags:           release,
					ControlplaneIngressURL: fmt.Sprintf("https://%s", state.KindIP),
					Namespace:              c.Name,
					TempDir:                filepath.Join(repoRoot, "integration-test/kink/", c.Name),
					LoadImageFlags:         c.LoadFlags,
					LoadArchiveFlags:       c.LoadFlags,
				}

				if !runningInCluster {
					gk8s.ClusterAction(
						kindClusterID, "Copy secure proxy registry cert",
						gingk8s.ClusterAction(func(gk8s gingk8s.Gingk8s, ctx context.Context, _ gingk8s.Cluster) error {
							Eventually(func() error {
								gk8s.
									Kubectl(ctx, hostCluster, "delete", "secret", "secure-proxy-registry-cert", "-n", c.Name).
									Run()
								var secret map[string]interface{}
								err := gk8s.
									Kubectl(ctx, hostCluster, "get", "secret", "-o", "json", "secure-proxy-registry-cert").
									WithStreams(gosh.FuncOut(gosh.SaveJSON(&secret))).
									Run()
								if err != nil {
									return err
								}
								secret["metadata"].(map[string]interface{})["namespace"] = c.Name
								secret["type"] = "Opaque"
								delete(secret["data"].(map[string]interface{}), "tls.key")
								err = gk8s.
									Kubectl(ctx, hostCluster, "create", "-f", "-").
									WithStreams(gosh.FuncIn(gosh.JSONIn(&secret))).
									Run()
								if err != nil {
									return err
								}
								return nil
							}, "30s", "5s").Should(Succeed())
							return nil
						}),
					)
				}

				k8sSmokeTestImagesID := gk8s.ThirdPartyImage(&k8sSmokeTestDeploymentImage)
				k8sSmokeTestImageArchivesID := gk8s.ImageArchives(&k8sSmokeTestStatefulSetImageArchive, &k8sSmokeTestJobImageArchive)
				kinkImageID := gk8s.CustomImage(&kinkImage)
				kinkClusterID = gk8s.Cluster(&kinkCluster, k8sSmokeTestImagesID, k8sSmokeTestImageArchivesID, kinkImageID)

				gk8s.ClusterAction(
					kinkClusterID,
					"Connecting to the controlplane w/ kubectl within a shell script",
					gingk8s.ClusterAction(func(gk8s gingk8s.Gingk8s, ctx context.Context, c gingk8s.Cluster) error {
						return kinkOpts.Shell(
							ctx,
							&kindKubeOpts,
							&chart,
							&release,
							`
						set -xe
						echo "${KUBECONFIG}"
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
						).WithStreams(gingk8s.GinkgoOutErr).Run()
					}),
				)

				ctx, cancel := context.WithCancel(context.Background())
				DeferCleanup(cancel)
				gk8s.Setup(ctx)

				if !c.Controlplane.External && !c.InCluster {
					By("Waiting for port-forward to stop")
				}

				return void
			})
		})
		It("should work", func() {

			gingk8s.WithRandomPorts[Void](1, func(randPorts []int) Void {

				ctx, cancel := context.WithCancel(context.Background())
				DeferCleanup(cancel)
				gk8s = gk8s.ForSpec()

				fileGatewayHost := state.KindIP

				ExpectRun(gosh.And(
					gk8s.Kubectl(
						ctx, hostCluster,
						"-n", c.Name,
						"rollout", "restart", fmt.Sprintf("sts/kink-%s-controlplane", c.Name),
					),
					gk8s.Kubectl(
						ctx, hostCluster,
						"-n", c.Name,
						"rollout", "restart", fmt.Sprintf("sts/kink-%s-worker", c.Name),
					),
					gk8s.Kubectl(
						ctx, hostCluster,
						"-n", c.Name,
						"rollout", "status", fmt.Sprintf("sts/kink-%s-controlplane", c.Name),
					),
					gk8s.Kubectl(
						ctx, hostCluster,
						"-n", c.Name,
						"rollout", "status", fmt.Sprintf("sts/kink-%s-worker", c.Name),
					),
				))

				deps := []gingk8s.ResourceDependency{}
				if !c.Controlplane.External && !c.InCluster {
					fileGatewayHost = "localhost"

					By("Port-Forwarding to controlplane")
					portForwarder := kinkOpts.PortForward(
						ctx,
						&kindKubeOpts,
						&chart,
						&release,
					).WithStreams(gingk8s.GinkgoOutErr)
					ExpectStart(portForwarder)
					DeferCleanup(func() { portForwarder.Kill() })
				}

				By("Waiting for controlplane to be available")
				Eventually(func() error {
					return gk8s.Kubectl(ctx, &kinkCluster, "version").Run()
				}, "30s", "1s").Should(Succeed())

				By("Executing test")
				gk8s.ClusterAction(kinkClusterID, "Watch Pods", &watchPods, deps...)
				gk8s.ClusterAction(kinkClusterID, "Watch Endpoints", &watchEndpoints, deps...)
				gk8s.ClusterAction(kinkClusterID, "Watch Services", &watchServices, deps...)
				gk8s.ClusterAction(kinkClusterID, "Watch Ingresses", &watchIngresses, deps...)
				gk8s.ClusterAction(kinkClusterID, "Watch PVCs", &watchPVCs, deps...)

				insecureTransport := http.DefaultTransport.(*http.Transport).Clone()
				insecureTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

				insecureClient := HTTP
				insecureClient.Transport = transport.DebugWrappers(insecureTransport)

				if !gk8sOpts.NoDeps {
					if c.ExtraClusterSetup != nil {
						deps = append(deps, c.ExtraClusterSetup(gk8s, kinkClusterID, deps)...)
					}

					k8sSmokeTest := gingk8s.HelmRelease{
						Name:         "k8s-smoke-test",
						Namespace:    "default",
						Chart:        &k8sSmokeTestChart,
						UpgradeFlags: []string{"--debug", "--timeout=15m"},
						Set:          c.SmokeTest.Set,
						SkipDelete:   true,
					}
					k8sSmokeTestID := gk8s.Release(kinkClusterID, &k8sSmokeTest, deps...)

					k8sSmokeTestPatchID := gk8s.ClusterAction(
						kinkClusterID,
						"Patch smoke-test to include hostport",
						gingk8s.ClusterCommander(func(g gingk8s.Gingk8s, ctx context.Context, c gingk8s.Cluster) gosh.Commander {
							return gosh.And(
								g.Kubectl(ctx, c, "patch", "deploy/k8s-smoke-test", "-n", "default", "-p", `{
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
				    }`),
								gosh.FanOut(
									g.Kubectl(ctx, c, "rollout", "status", "-n", "default", "deploy/k8s-smoke-test"),
									g.Kubectl(ctx, c, "rollout", "status", "-n", "default", "sts/k8s-smoke-test"),
								),
							)
						}),
						k8sSmokeTestID,
					)
					deps = []gingk8s.ResourceDependency{k8sSmokeTestPatchID}
				}

				gk8s.Setup(ctx)

				var mergedValues k8ssmoketest.MergedValues
				ExpectRun(
					localHelm.
						Helm(ctx, kinkCluster.GetConnection(), "get", "values", "-n", "default", "--all", "-o", "json", "k8s-smoke-test").
						WithStreams(gosh.FuncOut(gosh.SaveJSON(&mergedValues))),
				)

				kinkKubeConfigLoader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
					&clientcmd.ClientConfigLoadingRules{
						ExplicitPath: kinkCluster.GetConnection().Kubeconfig,
					},
					&clientcmd.ConfigOverrides{},
				)

				kinkKubeconfig, err := kinkKubeConfigLoader.ClientConfig()
				Expect(err).ToNot(HaveOccurred())

				cfg := &test.Config{
					// TODO: Extract certs from inner and outter ingress and add to this client instead
					HTTP:                 &insecureClient,
					K8sConfig:            kinkKubeconfig,
					ReleaseNamespace:     "default",
					ReleaseName:          "k8s-smoke-test",
					MergedValues:         &mergedValues,
					PortForwardLocalPort: randPorts[0],
					IngressHostname:      state.KindIP,
					IngressTLS:           c.SmokeTest.Ingress.HTTPSOnly,
				}

				k8sClient, err := cfg.K8sClient()
				Expect(err).ToNot(HaveOccurred())

				fullname := cfg.Fullname()

				By("Finding pod to port-forward...")
				deploymentPod, err := cfg.PickDeploymentPod(ctx, k8sClient, fullname)
				Expect(err).ToNot(HaveOccurred())

				By("Testing Port-Forwarding...")
				Eventually(k8ssmoketest.TestPortForward(ctx, cfg, deploymentPod), "30s", "500ms").Should(Succeed())

				By("Testing Ingress...")
				Eventually(func() error { return k8ssmoketest.TestIngress(ctx, cfg) }, "30s", "500ms").Should(Succeed())

				By("Getting StatefulSet Service...")
				statefulSetService, err := cfg.GetStatefulSetService(ctx, k8sClient, fullname)
				Expect(err).ToNot(HaveOccurred())

				// The smoke test expects a "real" cluster, so it expects that nodeports are available on their own ports on a given host,
				// and that loadbalancers are available on their ingresses.
				// This is true within the cluster, but to test it outside, we have to replace the inner nodeport with the outer nodeport that
				// maps to it, and replace the load balancer ingress IP's (which are the pod IPs of the guest nodes) with the host's node IP
				var loadBalancerService corev1.Service

				var outerNodePort int32
				Eventually(func() int32 {

					ExpectRun(gk8s.Kubectl(ctx, hostCluster, "get", "svc", "-n", "default", "-o", "json", fmt.Sprintf("kink-%s-lb", c.Name), "-n", c.Name).WithStreams(gosh.FuncOut(gosh.SaveJSON(&loadBalancerService))))
					innerNodePort := statefulSetService.Spec.Ports[0].NodePort
					for _, port := range loadBalancerService.Spec.Ports {
						if port.TargetPort.IntVal == innerNodePort {
							outerNodePort = port.NodePort
							break
						}
						GinkgoLogr.Info("Ignoring non-matching port", "port", port, "expecting", statefulSetService.Spec.Ports[0])
					}
					return outerNodePort
				}, "30s", "500ms").ShouldNot(BeZero(), "Load balancer service did not contain expected port for smoke test nodeport service")
				statefulSetService.Spec.Ports = []corev1.ServicePort{statefulSetService.Spec.Ports[0]}
				statefulSetService.Spec.Ports[0].NodePort = outerNodePort
				for ix := range statefulSetService.Status.LoadBalancer.Ingress {
					statefulSetService.Status.LoadBalancer.Ingress[ix].Hostname = ""
					statefulSetService.Status.LoadBalancer.Ingress[ix].IP = state.KindIP
					statefulSetService.Status.LoadBalancer.Ingress[ix].Ports = []corev1.PortStatus{{Port: outerNodePort}}
				}

				By("Testing NodePort...")
				Expect(k8ssmoketest.TestNodePort(ctx, cfg, statefulSetService)).To(Succeed())

				By("Testing LoadBalancer...")
				Expect(k8ssmoketest.TestLoadBalancer(ctx, cfg, statefulSetService)).To(Succeed())

				By("Testing Logs...")
				Expect(k8ssmoketest.TestLogs(ctx, cfg, k8sClient, deploymentPod, os.Stdout)).To(Succeed())

				if c.SmokeTest.Ingress.StaticHostname != "" {
					By("Interacting with the released service over a static ingress (HTTP)")
					req, err := http.NewRequest("GET", fmt.Sprintf("http://%s:80/rwx/test-file", state.KindIP), nil)
					Expect(err).ToNot(HaveOccurred())
					req.Host = c.SmokeTest.Ingress.StaticHostname
					Eventually(func() error { _, err := insecureClient.Do(req); return err }, "30s", "1s").Should(Succeed())
					Eventually(func() int {
						resp, err := insecureClient.Do(req)
						Expect(err).ToNot(HaveOccurred())
						return resp.StatusCode
					}, "30s", "1s").Should(Equal(http.StatusOK))

					By("Interacting with the released service over a static ingress (HTTPS)")
					req, err = http.NewRequest("GET", fmt.Sprintf("https://%s:443/rwx/test-file", state.KindIP), nil)
					Expect(err).ToNot(HaveOccurred())
					req.Host = c.SmokeTest.Ingress.StaticHostname
					Eventually(func() error { _, err := insecureClient.Do(req); return err }, "30s", "1s").Should(Succeed())
					Eventually(func() int {
						resp, err := insecureClient.Do(req)
						Expect(err).ToNot(HaveOccurred())
						return resp.StatusCode
					}, "30s", "1s").Should(Equal(http.StatusOK))
				}

				if c.FileGatewayEnabled {

					kubeOpts := kindKubeOpts
					kubeOpts.ConfigOverrides.Context.Namespace = c.Name

					By("Sending a file through the file gateway")
					ExpectRun(kinkOpts.
						FileGatewaySend(
							ctx,
							&kindKubeOpts,
							&chart,
							&release,
							[]string{
								"--send-dest", sharedLocalPathProvisionerMount,
								"--send-exclude", "integration-test/volumes", // This will cause an infinite loop of copying to itself
								"--send-exclude", "integration-test/log", // This is being written to while the test is running, meaning it will be bigger than its header, thus fail
								"--send-exclude", "integration-test/**/images/**", // These are just large, so copying them will slow down the tests
								"--send-exclude", "integration-test/**.tar",
								"--file-gateway-ingress-url", fmt.Sprintf("https://%s", fileGatewayHost),
								"--port-forward=false",
								// "-v11",
							},
							"Makefile",
							"integration-test",
						).
						WithStreams(gingk8s.GinkgoOutErr),
					)

					By("Checking the files were received")
					ExpectRun(gosh.FanOut(
						gk8s.KubectlExec(
							ctx, hostCluster,
							fmt.Sprintf("kink-%s-controlplane-0", c.Name),
							"cat", []string{filepath.Join(sharedLocalPathProvisionerMount, "Makefile")},
							"-n", c.Name,
						),
						gk8s.KubectlExec(
							ctx, hostCluster,
							fmt.Sprintf("kink-%s-controlplane-0", c.Name),
							"ls", []string{filepath.Join(sharedLocalPathProvisionerMount, "Makefile")},
							"-n", c.Name,
						),
					))

				}

				return void
			})
		})

	}
	if c.Focus && !runningInCluster {
		return FDescribe(c.Name, Label(c.Name), f)
	} else {
		return Describe(c.Name, Label(c.Name), f)
	}
}

func CleanupPVCDirs() {
	cleaner := gosh.Command("./hack/clean-tests-afterwards.sh").WithStreams(GinkgoOutErr)
	cleaner.Cmd.Dir = repoRoot
	ExpectRun(cleaner)
}

var k3sCase = Case{
	Name: "k3s",
	GetChart: func() helm.ChartFlags {
		return helm.ChartFlags{
			ChartName:     "kink",
			RepositoryURL: fmt.Sprintf("http://%s:%s", state.KindIP, state.ChartmuseumPort),
			Version:       state.ChartVersion,
		}
	},
	SmokeTest: CaseSmokeTest{
		Set: gingk8s.Object{
			"persistence.rwo.storageClassName": "standard", // default
			"persistence.rwx.storageClassName": "shared-local-path",
			"deployment.ingress.hostname":      "smoke-test.k3s.ingress.local",
			"deployment.ingress.className":     "nginx",
			"statefulset.nodePortHostname":     func() string { return state.KindIP },
		},
		Ingress: CaseIngressService{
			StaticHostname: "smoke-test.k3s.ingress.outer",
		},
	},
	ExtraClusterSetup: func(gk8s gingk8s.Gingk8s, c gingk8s.ClusterID, deps []gingk8s.ResourceDependency) []gingk8s.ResourceDependency {
		ingressNginxID := gk8s.Release(c, &ingressNginxInner, deps...)
		rolloutID := gk8s.ClusterAction(c, "Wait for ingress nginx", gingk8s.ClusterAction(func(g gingk8s.Gingk8s, ctx context.Context, c gingk8s.Cluster) error {
			return gk8s.Kubectl(ctx, c, "rollout", "status", "deploy/ingress-nginx-controller", "-n", "default").Run()
		}), append([]gingk8s.ResourceDependency{ingressNginxID}, deps...)...)
		return []gingk8s.ResourceDependency{rolloutID}
	},
	FileGatewayEnabled: true,
	LoadFlags:          []string{"--parallel-loads=3"},
}

var k3sHACase = Case{
	Name: "k3s-ha",
	GetChart: func() helm.ChartFlags {
		return helm.ChartFlags{
			ChartName: fmt.Sprintf("oci://%s:%s/charts/kink", state.KindIP, state.DockerRegistryPort),
			PlainHTTP: true,
			Version:   state.ChartVersion,
		}
	},
	SmokeTest: CaseSmokeTest{
		Set: gingk8s.Object{
			"persistence.rwo.storageClassName": "standard", // default
			"persistence.rwx.storageClassName": "shared-local-path",
			"deployment.ingress.hostname":      "smoke-test.k3s-ha.ingress.local",
			"deployment.ingress.className":     "nginx",
			"statefulset.nodePortHostname":     func() string { return state.KindIP },
		},
		Ingress: CaseIngressService{
			Namespace:      "default",
			Name:           "ingress-nginx-controller",
			HTTPPortName:   "http",
			HTTPSPortName:  "https",
			Hostname:       "smoke-test.k3s-ha.ingress.local",
			StaticHostname: "smoke-test.k3s-ha.ingress.outer",
			HTTPSOnly:      true,
		},
	},
	ExtraClusterSetup: func(gk8s gingk8s.Gingk8s, c gingk8s.ClusterID, deps []gingk8s.ResourceDependency) []gingk8s.ResourceDependency {
		ingressNginxID := gk8s.Release(c, &ingressNginxInner, deps...)
		rolloutID := gk8s.ClusterAction(c, "Wait for ingress nginx", gingk8s.ClusterAction(func(g gingk8s.Gingk8s, ctx context.Context, c gingk8s.Cluster) error {
			return gk8s.Kubectl(ctx, c, "rollout", "status", "deploy/ingress-nginx-controller", "-n", "default").Run()
		}), append([]gingk8s.ResourceDependency{ingressNginxID}, deps...)...)
		return []gingk8s.ResourceDependency{rolloutID}
	},
	LoadFlags: []string{"--parallel-loads=3"},

	Controlplane: CaseControlplane{
		External: true,
		NodePort: true,
	},

	FileGatewayEnabled: true,
}

var k3sSingleCase = Case{
	Name:      "k3s-single",
	InCluster: true,
	GetChart: func() helm.ChartFlags {
		return helm.ChartFlags{
			ChartName: filepath.Join(repoRoot, "helm/kink"),
		}
	},
	LoadFlags: []string{"--only-load-workers=false"},
	SmokeTest: CaseSmokeTest{
		Set: gingk8s.Object{
			"persistence.rwo.storageClassName": "standard", // default
			"persistence.rwx.storageClassName": "shared-local-path",
			"deployment.ingress.hostname":      "smoke-test.k3s-single.ingress.local",
			"deployment.ingress.className":     "nginx",
			"statefulset.nodePortHostname":     func() string { return state.KindIP },
		},
		Ingress: CaseIngressService{
			Namespace:      "default",
			Name:           "ingress-nginx-controller",
			HTTPPortName:   "http",
			HTTPSPortName:  "https",
			Hostname:       "smoke-test.k3s-single.ingress.local",
			StaticHostname: "smoke-test.k3s-single.ingress.outer",
		},
	},
	ExtraClusterSetup: func(gk8s gingk8s.Gingk8s, c gingk8s.ClusterID, deps []gingk8s.ResourceDependency) []gingk8s.ResourceDependency {
		ingressNginxID := gk8s.Release(c, &ingressNginxInnerDS, deps...)
		rolloutID := gk8s.ClusterAction(c, "Wait for ingress nginx", gingk8s.ClusterAction(func(g gingk8s.Gingk8s, ctx context.Context, c gingk8s.Cluster) error {
			return gk8s.Kubectl(ctx, c, "rollout", "status", "ds/ingress-nginx-controller", "-n", "default").Run()
		}), append([]gingk8s.ResourceDependency{ingressNginxID}, deps...)...)
		return []gingk8s.ResourceDependency{rolloutID}
	},

	FileGatewayEnabled: true,
}

var rke2Case = Case{
	Name: "rke2",
	GetChart: func() helm.ChartFlags {
		return helm.ChartFlags{
			ChartName: filepath.Join(repoRoot, fmt.Sprintf("bin/kink-%s.tgz", state.ChartVersion)),
		}
	},
	SmokeTest: CaseSmokeTest{
		Set: gingk8s.Object{
			"persistence.rwo.storageClassName":              "standard", // default
			"persistence.rwx.storageClassName":              "shared-local-path",
			"deployment.ingress.hostname":                   "smoke-test.rke2.ingress.local",
			"deployment.ingress.className":                  "nginx",
			"deployment.ingress.tls[0].hosts[0].secretName": "",
			"statefulset.nodePortHostname":                  func() string { return state.KindIP },
		},
		Ingress: CaseIngressService{
			Namespace:     "default",
			Name:          "ingress-nginx-controller",
			HTTPPortName:  "http",
			HTTPSPortName: "https",
			Hostname:      "smoke-test.rke2.ingress.local",
			HTTPSOnly:     true,
		},
	},
	ExtraClusterSetup: func(gk8s gingk8s.Gingk8s, c gingk8s.ClusterID, deps []gingk8s.ResourceDependency) []gingk8s.ResourceDependency {
		ingressNginxID := gk8s.Release(c, &ingressNginxInnerDS, deps...)
		rolloutID := gk8s.ClusterAction(c, "Wait for ingress nginx", gingk8s.ClusterAction(func(g gingk8s.Gingk8s, ctx context.Context, c gingk8s.Cluster) error {
			return gk8s.Kubectl(ctx, c, "rollout", "status", "ds/ingress-nginx-controller", "-n", "default").Run()
		}), append([]gingk8s.ResourceDependency{ingressNginxID}, deps...)...)
		return []gingk8s.ResourceDependency{rolloutID}
	},
	Controlplane: CaseControlplane{
		External: true,
	},
	LoadFlags: []string{"--parallel-loads=3"},
}

var cases = []Case{
	k3sCase,
	k3sSingleCase,
	k3sHACase,
	rke2Case,
}

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

func init() {
	for _, c := range cases {
		c.Create()
	}
}
