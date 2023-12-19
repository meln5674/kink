/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	goflag "flag"
	"time"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/meln5674/kink/pkg/lbmanager"
	"github.com/meln5674/rflag"
)

var (
	lbServiceCreated bool

	scheme = runtime.NewScheme()
)

// lbManagerCmd represents the lb-manager command
var lbManagerCmd = &cobra.Command{
	Use:   "lb-manager",
	Short: "Watch a guest cluster for NodePort and LoadBalancer services",
	Long: `While running, NodePort and LoadBalancer services in the guest cluster will
manifest as extra ports on a dynamically managed service within the host cluster. LoadBalancer
type services will also have their ingress IPs set to this service IP.
	`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&lbManagerArgs.zap)))
		ctx := ctrl.SetupSignalHandler()
		return runLBManager(ctx, &lbManagerArgs, &resolvedConfig)
	},
}

type lbManagerLeaderElectionArgsT struct {
	Identity string        `rflag:"name=id,usage=Identity for leader election"`
	Lease    time.Duration `rflag:"usage=Lease duration for leader election"`
	Renew    time.Duration `rflag:"usage=Renewal deadline for leader election"`
	Retry    time.Duration `rflag:"usage=Retry period for leader election"`
}

func (lbManagerLeaderElectionArgsT) Defaults() lbManagerLeaderElectionArgsT {
	return lbManagerLeaderElectionArgsT{
		Lease: 15 * time.Second,
		Renew: 10 * time.Second,
		Retry: 2 * time.Second,
	}
}

type lbManagerArgsT struct {
	GuestKubeconfig       string                       `rflag:"usage=Path to the kubeconfig file to use for accessing the guest cluster"`
	LeaderElectionEnabled bool                         `rflag:"name=leader-election,usage=Enable leader election. Required if more than one replica is running"`
	LeaderElection        lbManagerLeaderElectionArgsT `rflag:"prefix=leader-election-"`
	RequeueDelay          time.Duration                `rflag:"usage=Time to wait between retries for reconciliation errors due to e.g. kube api server errors"`

	MetricsAddr string `rflag:"name=metrics-bind-address,usage=The address the metric endpoint binds to."`
	ProbeAddr   string `rflag:"name=health-probe-bind-address,usage=The address the probe endpoint binds to."`

	zap                      zap.Options
	guestKubeconfigOverrides clientcmd.ConfigOverrides
}

func (lbManagerArgsT) Defaults() lbManagerArgsT {
	return lbManagerArgsT{
		LeaderElection: lbManagerLeaderElectionArgsT{}.Defaults(),
		MetricsAddr:    ":8080",
		ProbeAddr:      ":8081",
		RequeueDelay:   5 * time.Second,

		zap: zap.Options{
			Development: true,
		},
	}
}

var lbManagerArgs = lbManagerArgsT{}.Defaults()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	rootCmd.AddCommand(lbManagerCmd)
	rflag.MustRegister(rflag.ForPFlag(lbManagerCmd.Flags()), "", &lbManagerArgs)
	zapFlags := goflag.NewFlagSet("", goflag.PanicOnError)
	lbManagerArgs.zap.BindFlags(zapFlags)
	lbManagerCmd.Flags().AddGoFlagSet(zapFlags)

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

	clientcmd.BindOverrideFlags(&lbManagerArgs.guestKubeconfigOverrides, lbManagerCmd.Flags(), guestFlags)
}

func runLBManager(ctx context.Context, args *lbManagerArgsT, cfg *resolvedConfigT) error {

	setupLog := ctrl.Log.WithName("setup")

	guestConfigLoader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{
			ExplicitPath: args.GuestKubeconfig,
		},
		&args.guestKubeconfigOverrides,
	)

	guestKubeconfig, err := guestConfigLoader.RawConfig()
	if err != nil {
		return err
	}
	setupLog.Info("Resolved guest kubeconfig", "kubeconfig", guestKubeconfig)

	guestConfig, err := guestConfigLoader.ClientConfig()
	if err != nil {
		return err
	}

	mgr, err := ctrl.NewManager(guestConfig, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     args.MetricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: args.ProbeAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	hostClient, err := client.New(cfg.Kubeconfig, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	hostClient = client.NewNamespacedClient(hostClient, cfg.ReleaseNamespace)

	serviceController := lbmanager.ServiceController{
		Guest:            mgr.GetClient(),
		Host:             hostClient,
		Log:              ctrl.Log.WithName("svc-ctrl"),
		LBSvc:            &corev1.Service{},
		NodePorts:        make(map[int32]corev1.ServicePort),
		ServiceNodePorts: make(map[string]map[string][]int32),
		RequeueDelay:     args.RequeueDelay,
		ReleaseNamespace: cfg.ReleaseNamespace,
		ReleaseConfig:    cfg.ReleaseConfig,
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
		RequeueDelay:     args.RequeueDelay,
		ReleaseNamespace: cfg.ReleaseNamespace,
		ReleaseConfig:    cfg.ReleaseConfig,
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
}
