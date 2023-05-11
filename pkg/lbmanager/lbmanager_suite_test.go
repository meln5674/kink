package lbmanager_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	cfg "github.com/meln5674/kink/pkg/config"
	"github.com/meln5674/kink/pkg/lbmanager"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLbmanager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Lbmanager Suite")
}

type env struct {
	name    string
	cfg     *rest.Config
	testEnv *envtest.Environment
	mgr     manager.Manager

	k8sClient client.Client
	k8sReader client.Reader
}

func (e *env) start() {
	var err error
	By(fmt.Sprintf("bootstrapping %s environment", e.name))
	e.cfg, err = e.testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(e.cfg).ToNot(BeNil())
	DeferCleanup(func() {
		Expect(e.testEnv.Stop()).To(Succeed())
	})
	// e.k8sClient, err = client.New(e.cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	e.mgr, err = ctrl.NewManager(e.cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
	})
	Expect(err).ToNot(HaveOccurred())
	e.k8sClient = e.mgr.GetClient()
	e.k8sReader = e.mgr.GetAPIReader()
	GinkgoWriter.Printf("%s config:\n%#v\n", e.name, e.cfg)
}

var (
	testHost      = env{name: "host", testEnv: &envtest.Environment{}}
	testGuest     = env{name: "guest", testEnv: &envtest.Environment{}}
	releaseConfig cfg.ReleaseConfig
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testHost.start()
	testGuest.start()

	httpPortNumber := "80"
	httpPortName := "http"
	httpsPortNumber := "443"
	httpsPortName := "https"

	mappings := map[string]cfg.LoadBalancerIngressClassMapping{}
	for _, isNodePort := range []bool{true, false} {
		for _, isHttps := range []bool{true, false} {
			for _, useName := range []bool{true, false} {
				portType := "host"
				if isNodePort {
					portType = "node"
				}
				proto := "http"
				if isHttps {
					proto = "https"
				}
				var portString *string
				switch {
				case isHttps && useName:
					portString = &httpsPortName
				case isHttps && !useName:
					portString = &httpsPortNumber
				case !isHttps && useName:
					portString = &httpPortName
				case !isHttps && !useName:
					portString = &httpPortNumber
				}
				portID := "number"
				if useName {
					portID = "name"
				}
				classID := fmt.Sprintf("%s-%s-%s", proto, portType, portID)
				mapping := cfg.LoadBalancerIngressClassMapping{
					ClassName: fmt.Sprintf("host-%s", classID),
					Annotations: map[string]string{
						fmt.Sprintf("test-annotation-%s", classID): fmt.Sprintf("test-annotation-value-%s", classID),
					},
				}

				if isNodePort {
					mapping.NodePort = &cfg.LoadBalancerIngressNodePortClassMapping{
						Name:      fmt.Sprintf("ns-%s", classID),
						Namespace: fmt.Sprintf("svc-%s", classID),
					}
					if isHttps {
						mapping.NodePort.HttpPort = portString
					} else {
						mapping.NodePort.HttpsPort = portString
					}
				} else {
					mapping.HostPort = &cfg.LoadBalancerIngressHostPortClassMapping{}
					if isHttps {
						mapping.HostPort.HttpPort = portString
					} else {
						mapping.HostPort.HttpsPort = portString
					}
				}
				mappings[fmt.Sprintf("guest-%s", classID)] = mapping
			}
		}
	}

	releaseConfig = cfg.ReleaseConfig{
		Fullname:             "test",
		LoadBalancerFullname: "test-lb",
		LoadBalancerLabels: map[string]string{
			"testkey": "testvalue",
		},
		LoadBalancerSelectorLabels: map[string]string{
			"testkey":      "testvalue",
			"testselector": "testselectorvalue",
		},
		LoadBalancerServiceAnnotations: map[string]string{
			"testannotation": "testannotationvalue",
		},
		LoadBalancerIngress: cfg.LoadBalancerIngress{
			LoadBalancerIngressInner: cfg.LoadBalancerIngressInner{
				Enabled:                true,
				HostPortTargetFullname: "test-lb",
				ClassMappings:          mappings,
			},
		},
		LBManagerFullname: "test",
	}

	serviceController := lbmanager.ServiceController{
		Guest:            testGuest.k8sClient,
		Host:             testHost.k8sClient,
		Log:              ctrl.Log.WithName("svc-ctrl"),
		LBSvc:            &corev1.Service{},
		NodePorts:        make(map[int32]corev1.ServicePort),
		ServiceNodePorts: make(map[string]map[string][]int32),
		RequeueDelay:     1 * time.Second,
		ReleaseNamespace: "default",
		ReleaseConfig:    releaseConfig,
	}
	serviceController.SetHostLBMetadata()
	Expect(
		builder.
			ControllerManagedBy(testGuest.mgr).
			For(&corev1.Service{}).
			Complete(&serviceController),
	).To(Succeed())

	ingressController := lbmanager.IngressController{
		Guest:            testGuest.k8sClient,
		Host:             testHost.k8sClient,
		Log:              ctrl.Log.WithName("ingress-ctrl"),
		Targets:          make(map[string]*netv1.Ingress),
		IngressClasses:   make(map[string]map[string]string),
		ClassIngresses:   make(map[string]map[string]map[string]struct{}),
		IngressPaths:     make(map[string]map[string]map[string][]netv1.HTTPIngressPath),
		RequeueDelay:     1 * time.Second,
		ReleaseNamespace: "default",
		ReleaseConfig:    releaseConfig,
	}
	Expect(
		builder.
			ControllerManagedBy(testGuest.mgr).
			For(&netv1.Ingress{}).
			Complete(&ingressController),
	).To(Succeed())

	mgrCtx, stopMgr := context.WithCancel(context.Background())
	go func() {
		GinkgoRecover()
		testGuest.mgr.Start(mgrCtx)
	}()
	go func() {
		GinkgoRecover()
		testHost.mgr.Start(mgrCtx)
	}()
	DeferCleanup(stopMgr)
})
