/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	goflag "flag"
	"time"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/meln5674/kink/pkg/kubectl"
	"github.com/meln5674/kink/pkg/lbmanager"
)

var (
	guestKubeConfig             kubectl.KubeFlags
	lbSvcLeaderElectionEnabled  bool
	lbSvcLeaderElectionIdentity string
	lbSvcLeaderElectionLease    time.Duration
	lbSvcLeaderElectionRenew    time.Duration
	lbSvcLeaderElectionRetry    time.Duration

	lbServiceCreated bool

	metricsAddr string
	probeAddr   string
	zapOpts     = zap.Options{
		Development: true,
	}

	scheme       = runtime.NewScheme()
	requeueDelay = 5 * time.Second // TODO: Flag for this
)

// lbManagerCmd represents the lbManager command
var lbManagerCmd = &cobra.Command{
	Use:   "lb-manager",
	Short: "Watch a guest cluster for NodePort and LoadBalancer services",
	Long: `While running, NodePort and LoadBalancer services in the guest cluster will
manifest as extra ports on a dynamically managed service within the host cluster. LoadBalancer
type services will also have their ingress IPs set to this service IP.
	`,
	Run: func(cmd *cobra.Command, args []string) {
		err := func() error {

			setupLog := ctrl.Log.WithName("setup")

			ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))
			ctx := ctrl.SetupSignalHandler()

			guestConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				&clientcmd.ClientConfigLoadingRules{
					ExplicitPath: guestKubeConfig.Kubeconfig,
				},
				&guestKubeConfig.ConfigOverrides,
			).ClientConfig()
			if err != nil {
				return err
			}

			mgr, err := ctrl.NewManager(guestConfig, ctrl.Options{
				Scheme:                 scheme,
				MetricsBindAddress:     metricsAddr,
				Port:                   9443,
				HealthProbeBindAddress: probeAddr,
			})
			if err != nil {
				setupLog.Error(err, "unable to start manager")
				return err
			}

			hostClient, err := client.New(kubeconfig, client.Options{Scheme: scheme})
			if err != nil {
				return err
			}

			serviceController := lbmanager.ServiceController{
				Guest:            mgr.GetClient(),
				Host:             hostClient,
				Log:              ctrl.Log.WithName("svc-ctrl"),
				LBSvc:            &corev1.Service{},
				NodePorts:        make(map[int32]corev1.ServicePort),
				ServiceNodePorts: make(map[string]map[string][]int32),
				RequeueDelay:     requeueDelay,
				ReleaseNamespace: releaseNamespace,
				ReleaseConfig:    releaseConfig,
			}
			serviceController.SetHostLBMetadata()
			err = builder.
				ControllerManagedBy(mgr).
				For(&corev1.Service{}).
				Complete(&serviceController)
			if err != nil {
				return err
			}

			ingressController := lbmanager.IngressController{
				Guest:            mgr.GetClient(),
				Host:             hostClient,
				Log:              ctrl.Log.WithName("ingress-ctrl"),
				Targets:          make(map[string]*netv1.Ingress),
				IngressClasses:   make(map[string]map[string]string),
				ClassIngresses:   make(map[string]map[string]map[string]struct{}),
				IngressPaths:     make(map[string]map[string]map[string][]netv1.HTTPIngressPath),
				RequeueDelay:     requeueDelay,
				ReleaseNamespace: releaseNamespace,
				ReleaseConfig:    releaseConfig,
			}
			err = builder.
				ControllerManagedBy(mgr).
				For(&netv1.Ingress{}).
				Complete(&ingressController)
			if err != nil {
				return err
			}

			if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up health check")
				return err
			}
			if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up ready check")
				return err
			}

			setupLog.Info("starting manager")
			if err = mgr.Start(ctx); err != nil {
				setupLog.Error(err, "problem running manager")
				return err
			}

			return nil
		}()

		if err != nil {
			klog.Fatal(err)
		}
	},
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	rootCmd.AddCommand(lbManagerCmd)
	lbManagerCmd.PersistentFlags().StringVar(&guestKubeConfig.Kubeconfig, "guest-kubeconfig", "", "Path to the kubeconfig file to use for accessing the guest cluster")
	lbManagerCmd.PersistentFlags().BoolVar(&lbSvcLeaderElectionEnabled, "leader-election", false, "Enable leader election. Required if more than one replica is running")
	lbManagerCmd.PersistentFlags().StringVar(&lbSvcLeaderElectionIdentity, "leader-election-id", "", "Identity for leader election")
	lbManagerCmd.PersistentFlags().DurationVar(&lbSvcLeaderElectionLease, "leader-election-lease", 15*time.Second, "Lease duration for leader election")
	lbManagerCmd.PersistentFlags().DurationVar(&lbSvcLeaderElectionRenew, "leader-election-renew", 10*time.Second, "Renewal deadline for leader election")
	lbManagerCmd.PersistentFlags().DurationVar(&lbSvcLeaderElectionRetry, "leader-election-retry", 2*time.Second, "Retry period for leader election")

	lbManagerCmd.PersistentFlags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	lbManagerCmd.PersistentFlags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	zapFlags := goflag.NewFlagSet("", goflag.PanicOnError)
	zapOpts.BindFlags(zapFlags)
	lbManagerCmd.PersistentFlags().AddGoFlagSet(zapFlags)

	// If we don't do this, the short names overlap with the host k8s flags
	guestFlags := clientcmd.RecommendedConfigOverrideFlags("guest-")
	guestFlagPtrs := []*clientcmd.FlagInfo{
		&guestFlags.AuthOverrideFlags.ClientCertificate,
		&guestFlags.AuthOverrideFlags.ClientKey,
		&guestFlags.AuthOverrideFlags.Token,
		&guestFlags.AuthOverrideFlags.Impersonate,
		&guestFlags.AuthOverrideFlags.ImpersonateUID,
		&guestFlags.AuthOverrideFlags.ImpersonateGroups,
		&guestFlags.AuthOverrideFlags.Username,
		&guestFlags.AuthOverrideFlags.Password,
		&guestFlags.ClusterOverrideFlags.APIServer,
		&guestFlags.ClusterOverrideFlags.APIVersion,
		&guestFlags.ClusterOverrideFlags.CertificateAuthority,
		&guestFlags.ClusterOverrideFlags.InsecureSkipTLSVerify,
		&guestFlags.ClusterOverrideFlags.TLSServerName,
		&guestFlags.ClusterOverrideFlags.ProxyURL,
		//&guestFlags.ClusterOverrideFlags.DisableCompression,
		&guestFlags.ContextOverrideFlags.ClusterName,
		&guestFlags.ContextOverrideFlags.AuthInfoName,
		&guestFlags.ContextOverrideFlags.Namespace,
		&guestFlags.CurrentContext,
		&guestFlags.Timeout,
	}

	for _, ptr := range guestFlagPtrs {
		ptr.ShortName = ""
	}

	clientcmd.BindOverrideFlags(&guestKubeConfig.ConfigOverrides, lbManagerCmd.PersistentFlags(), guestFlags)
}
