/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	goflag "flag"
	"fmt"
	"hash/adler32"
	"strconv"
	"time"

	"github.com/go-logr/logr"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	cfg "github.com/meln5674/kink/pkg/config"
	"github.com/meln5674/kink/pkg/kubectl"
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

const (
	IngressClassNameAnnotation = "kubernetes.io/ingress.class"
	GuestClassLabel            = "kink.meln5674.github.com/guest-ingress-class"

	ServiceFinalizer = "kink.meln5674.github.com/lb-manager-svc"
	IngressFinalizer = "kink.meln5674.github.com/lb-manager-ingress"
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

			serviceController := ServiceController{
				Guest:            mgr.GetClient(),
				Host:             hostClient,
				Log:              ctrl.Log.WithName("svc-ctrl"),
				LBSvc:            &corev1.Service{},
				NodePorts:        make(map[int32]corev1.ServicePort),
				ServiceNodePorts: make(map[string]map[string][]int32),
			}
			serviceController.SetHostLBMetadata()
			err = builder.
				ControllerManagedBy(mgr).
				For(&corev1.Service{}).
				Complete(&serviceController)
			if err != nil {
				return err
			}

			ingressController := IngressController{
				Guest:          mgr.GetClient(),
				Host:           hostClient,
				Log:            ctrl.Log.WithName("ingress-ctrl"),
				Targets:        make(map[string]*netv1.Ingress),
				IngressClasses: make(map[string]map[string]string),
				ClassIngresses: make(map[string]map[string]map[string]struct{}),
				IngressPaths:   make(map[string]map[string]map[string][]netv1.HTTPIngressPath),
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

func objString(i interface{}) string {
	obj, ok := i.(metav1.Object)
	if !ok {
		return fmt.Sprintf("%#v", i)
	}
	typ, ok := i.(metav1.Type)
	if !ok {
		return fmt.Sprintf("%#v", i)
	}
	return fmt.Sprintf("%s/%s/%s/%s", typ.GetAPIVersion(), typ.GetKind(), obj.GetNamespace(), obj.GetName())
}

type ServiceController struct {
	Host             client.Client
	Guest            client.Client
	Log              logr.Logger
	NodePorts        map[int32]corev1.ServicePort
	ServiceNodePorts map[string]map[string][]int32
	LBSvc            *corev1.Service
}

func (s *ServiceController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	key := client.ObjectKey(req.NamespacedName)
	svc := &corev1.Service{}
	err := s.Guest.Get(ctx, key, svc)
	if kerrors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueDelay}, err
	}

	log := s.Log.WithValues("svc", key)
	log.Info("Received event")
	run := ServiceControllerRun{
		ServiceController: s,
		Log:               log,
		Svc:               svc,
		Ctx:               ctx,
	}
	deleted, err := run.HandleFinalizer()
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueDelay}, err
	}
	if deleted {
		return ctrl.Result{}, nil
	}

	run.UpsertPorts()
	err = run.CreateOrUpdateHostLB()
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueDelay}, err
	}
	err = run.SetLBIngress()
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueDelay}, err
	}
	return ctrl.Result{}, nil
}

type ServiceControllerRun struct {
	*ServiceController
	Log logr.Logger
	Svc *corev1.Service
	Ctx context.Context
}

func (s *ServiceControllerRun) HandleFinalizer() (deleted bool, err error) {
	if s.Svc.DeletionTimestamp != nil {
		s.Log.Info("Removing ports for service being deleted")
		s.RemovePorts()
		err := s.CreateOrUpdateHostLB()
		if err != nil {
			return false, nil
		}
		s.Log.Info("Removing finalizer")
		controllerutil.RemoveFinalizer(s.Svc, ServiceFinalizer)
		err = s.Guest.Update(s.Ctx, s.Svc)
		if err != nil {
			return false, nil
		}
		return true, nil
	}
	if s.Svc.Spec.Type == corev1.ServiceTypeNodePort || s.Svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		s.Log.Info("Ensuring finalizer present")
		controllerutil.AddFinalizer(s.Svc, ServiceFinalizer)
	} else {
		s.Log.Info("Removing finalizer from non-NodePort service")
		controllerutil.RemoveFinalizer(s.Svc, ServiceFinalizer)
	}
	err = s.Guest.Update(s.Ctx, s.Svc)
	if err != nil {
		return false, err
	}
	return false, nil
}

func (s *ServiceController) SetHostLBMetadata() {
	s.LBSvc.Name = releaseConfig.LoadBalancerFullname
	s.LBSvc.Namespace = releaseNamespace
	if s.LBSvc.Labels == nil {
		s.LBSvc.Labels = make(map[string]string, len(releaseConfig.LoadBalancerLabels))
	}
	for k, v := range releaseConfig.LoadBalancerLabels {
		s.LBSvc.Labels[k] = v
	}
	if s.LBSvc.Annotations == nil {
		s.LBSvc.Annotations = make(map[string]string, len(releaseConfig.LoadBalancerLabels))
	}
	for k, v := range releaseConfig.LoadBalancerServiceAnnotations {
		s.LBSvc.Annotations[k] = v
	}
}

func (s *ServiceControllerRun) GenerateHostLB() {
	s.SetHostLBMetadata()

	ports := make([]corev1.ServicePort, 0, len(s.NodePorts))
	for _, port := range s.NodePorts {
		ports = append(ports, port)
	}
	if len(ports) == 0 {
		ports = []corev1.ServicePort{
			{
				Name:       "tmp",
				Port:       1,
				TargetPort: intstr.FromInt(1),
			},
		}
	}

	s.LBSvc.Spec.Type = corev1.ServiceTypeClusterIP // TODO: Should this be configurable?
	s.LBSvc.Spec.Ports = ports
	s.LBSvc.Spec.Selector = releaseConfig.LoadBalancerSelectorLabels
}

func (s *ServiceControllerRun) UpsertPorts() {
	nsPorts, ok := s.ServiceNodePorts[s.Svc.Namespace]
	if !ok {
		nsPorts = make(map[string][]int32)
		s.ServiceNodePorts[s.Svc.Namespace] = nsPorts
	}
	svcPorts, ok := nsPorts[s.Svc.Name]
	if ok {
		for _, nodePort := range svcPorts {
			delete(s.NodePorts, nodePort)
		}
	}
	svcPorts = make([]int32, len(s.Svc.Spec.Ports))
	for _, port := range s.Svc.Spec.Ports {
		if port.NodePort == 0 {
			continue
		}
		s.NodePorts[port.NodePort] = ConvertPort(s.Svc.Namespace, s.Svc.Name, &port)
		svcPorts = append(svcPorts, port.NodePort)
	}
	nsPorts[s.Svc.Name] = svcPorts
}

func (s *ServiceControllerRun) RemovePorts() {
	nsPorts, ok := s.ServiceNodePorts[s.Svc.Namespace]
	if !ok {
		return
	}
	svcPorts, ok := nsPorts[s.Svc.Name]
	if !ok {
		return
	}
	delete(nsPorts, s.Svc.Name)
	for _, port := range svcPorts {
		delete(s.NodePorts, port)
	}
}

func (s *ServiceControllerRun) CreateOrUpdateHostLB() error {
	s.Log.Info("Regenerating host LB service")
	_, err := controllerutil.CreateOrUpdate(s.Ctx, s.Host, s.LBSvc, func() error {
		s.GenerateHostLB()
		return nil
	})
	if err != nil {
		return err
	}
	for s.LBSvc.Spec.ClusterIP == "" {
		s.Log.Info("LB Service has no ClusterIP, waiting...")
		time.Sleep(5 * time.Second) // TODO: Make this polling configurable
		err := s.Host.Get(s.Ctx, client.ObjectKeyFromObject(s.LBSvc), s.LBSvc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ServiceControllerRun) SetLBIngress() error {
	if s.Svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return nil
	}
	s.Log.Info("Setting LoadBalancer IP", "ip", s.LBSvc.Spec.ClusterIP)
	s.Svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
		{
			IP: s.LBSvc.Spec.ClusterIP,
		},
	}
	return s.Guest.Status().Update(s.Ctx, s.Svc)
}

// PortName produces a predictable port name from a guest service.
// This is done in a way that can be matched in the helm chart, allowing us to create
// static ingresses for guest NodePort services without knowing their assigned nodeports
// ahead of time, and without imposing further name length restrictions.
// This works by taking a 32-bit checksum of the "namespace/name/portname" of the port,
// then formatting as hex, padding to a max of 8 characters with a prefix
func PortName(namespace, name string, port *corev1.ServicePort) string {
	// TODO: Have some way of adding a nonce if somehow there is a has collision
	var toSum string
	if port.Name == "" {
		toSum = fmt.Sprintf("%s/%s/%d", namespace, name, port.Port)
	} else {
		toSum = fmt.Sprintf("%s/%s/%s", namespace, name, port.Name)
	}
	// This may seem redundant, but we need to match the behavior of the helm chart exactly
	x, err := strconv.Atoi(fmt.Sprintf("%d", adler32.Checksum([]byte(toSum))))
	if err != nil {
		panic(fmt.Sprintf("BUG: Failed to produce PortName, this shouldn't be possible: %s", err))
	}
	return fmt.Sprintf("np-0x%08x", x)
}

func ConvertPort(namespace, name string, port *corev1.ServicePort) corev1.ServicePort {
	return corev1.ServicePort{
		Name:        PortName(namespace, name, port),
		Protocol:    port.Protocol,
		AppProtocol: port.AppProtocol,
		Port:        port.NodePort,
		TargetPort:  intstr.FromInt(int(port.NodePort)),
	}
}

type IngressController struct {
	Host    client.Client
	Guest   client.Client
	Log     logr.Logger
	Targets map[string]*netv1.Ingress
	// namespace - name -> class
	IngressClasses map[string]map[string]string
	// class -> namespace -> name -> unit
	ClassIngresses map[string]map[string]map[string]struct{}
	// namespace -> name -> host -> paths
	IngressPaths map[string]map[string]map[string][]netv1.HTTPIngressPath
}

func (i *IngressController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	key := client.ObjectKey(req.NamespacedName)
	ing := &netv1.Ingress{}
	err := i.Guest.Get(ctx, key, ing)
	if kerrors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueDelay}, err
	}

	guestClass, ok := GetClassName(ing)
	var mappedClass *cfg.LoadBalancerIngressClassMapping
	if ok {
		mappedClassT, ok := releaseConfig.LoadBalancerIngress.ClassMappings[guestClass]
		if ok {
			mappedClass = &mappedClassT
		}
	}

	log := i.Log.WithValues("ingress", key, "guestClass", guestClass)
	if mappedClass != nil {
		log = log.WithValues("hostClass", mappedClass.ClassName)
	} else {
		log = log.WithValues("hostClass", nil)
	}
	log.Info("Received event")

	run := IngressControllerRun{
		IngressController: i,
		Log:               log,
		Ingress:           ing,
		Ctx:               ctx,
		GuestClass:        guestClass,
		MappedClass:       mappedClass,
	}

	run.GenerateHostIngressMetadata()

	deleted, err := run.HandleFinalizer()
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueDelay}, err
	}
	if deleted {
		return ctrl.Result{}, nil
	}

	run.UpsertPaths()
	err = run.CreateOrUpdateHostIngress()
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueDelay}, err
	}
	return ctrl.Result{}, nil
}

func GetClassName(ing *netv1.Ingress) (string, bool) {
	if ing.Spec.IngressClassName != nil && *ing.Spec.IngressClassName != "" {
		return *ing.Spec.IngressClassName, true
	}
	if ingressClassName, ok := ing.Annotations[IngressClassNameAnnotation]; ok {
		return ingressClassName, true
	}
	return "", false
}

type IngressControllerRun struct {
	*IngressController
	Ingress     *netv1.Ingress
	GuestClass  string
	MappedClass *cfg.LoadBalancerIngressClassMapping
	Log         logr.Logger
	Ctx         context.Context
}

func (i *IngressControllerRun) HandleFinalizer() (deleted bool, err error) {
	if i.Ingress.DeletionTimestamp != nil {
		i.Log.Info("Removing paths for service being deleted")
		i.RemovePaths()
		i.Log.Info("Regenerating host ingress")
		err = i.CreateOrUpdateHostIngress()
		if err != nil {
			return false, nil
		}
		i.Log.Info("Removing finalizer")
		controllerutil.RemoveFinalizer(i.Ingress, IngressFinalizer)
		err = i.Guest.Update(i.Ctx, i.Ingress)
		if err != nil {
			return false, nil
		}
		return true, nil
	}
	i.Log.Info("Ensuring finalizer present")
	controllerutil.AddFinalizer(i.Ingress, IngressFinalizer)
	err = i.Guest.Update(i.Ctx, i.Ingress)
	if err != nil {
		return false, err
	}
	return false, nil
}

func (i *IngressControllerRun) UpsertPaths() {
	nsClasses, ok := i.IngressClasses[i.Ingress.Namespace]
	if !ok {
		nsClasses = make(map[string]string)
		i.IngressClasses[i.Ingress.Namespace] = nsClasses
	}
	oldClass, ok := nsClasses[i.Ingress.Name]
	if ok && oldClass != i.GuestClass {
		oldClassIngresses, ok := i.ClassIngresses[oldClass]
		if ok {
			oldClassNsIngresses, ok := oldClassIngresses[i.Ingress.Namespace]
			if ok {
				delete(oldClassNsIngresses, i.Ingress.Name)
			}
		}
	}
	if i.GuestClass == "" {
		return
	}
	nsClasses[i.Ingress.Name] = i.GuestClass
	classIngresses, ok := i.ClassIngresses[i.GuestClass]
	if !ok {
		classIngresses = make(map[string]map[string]struct{})
		i.ClassIngresses[i.GuestClass] = classIngresses
	}
	classNsIngresses, ok := classIngresses[i.Ingress.Namespace]
	if !ok {
		classNsIngresses = make(map[string]struct{})
		classIngresses[i.Ingress.Namespace] = classNsIngresses
	}
	classNsIngresses[i.Ingress.Name] = struct{}{}

	nsPaths, ok := i.IngressPaths[i.Ingress.Namespace]
	if !ok {
		nsPaths = make(map[string]map[string][]netv1.HTTPIngressPath)
		i.IngressPaths[i.Ingress.Namespace] = nsPaths
	}
	hostPaths := make(map[string][]netv1.HTTPIngressPath, len(i.Ingress.Spec.Rules))
	nsPaths[i.Ingress.Name] = hostPaths

	for _, rule := range i.Ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		paths, ok := hostPaths[rule.Host]
		if !ok {
			paths = make([]netv1.HTTPIngressPath, 0, len(rule.HTTP.Paths))
		}
		for _, path := range rule.HTTP.Paths {
			paths = append(paths, path)
		}
		hostPaths[rule.Host] = paths
	}
}

func (i *IngressControllerRun) RemovePaths() {
	if i.GuestClass == "" {
		return
	}
	classIngresses, ok := i.ClassIngresses[i.GuestClass]
	if ok {
		classNsIngresses, ok := classIngresses[i.Ingress.Namespace]
		if ok {
			delete(classNsIngresses, i.Ingress.Name)
		}
	}
	nsClasses, ok := i.IngressClasses[i.Ingress.Namespace]
	if ok {
		delete(nsClasses, i.Ingress.Name)
	}
	nsPaths, ok := i.IngressPaths[i.Ingress.Namespace]
	if ok {
		delete(nsPaths, i.Ingress.Name)
	}
}

func (i *IngressControllerRun) GenerateHostIngressMetadata() {
	if i.GuestClass == "" || i.MappedClass == nil {
		return
	}
	target := i.Targets[i.GuestClass]
	if target == nil {
		target = &netv1.Ingress{}
		i.Targets[i.GuestClass] = target
	}
	target.Name = fmt.Sprintf("%s-%s", releaseConfig.LoadBalancerFullname, i.GuestClass)
	target.Namespace = releaseNamespace
	if target.Annotations == nil {
		target.Annotations = make(map[string]string, len(i.MappedClass.Annotations))
	}
	for k, v := range i.MappedClass.Annotations {
		target.Annotations[k] = v
	}
	if target.Labels == nil {
		target.Labels = make(map[string]string, len(releaseConfig.LoadBalancerLabels)+1)
	}
	for k, v := range releaseConfig.LoadBalancerLabels {
		target.Labels[k] = v
	}
	target.Labels[GuestClassLabel] = i.GuestClass
}

func (i *IngressControllerRun) GenerateHostIngress() error {
	if i.GuestClass == "" || i.MappedClass == nil {
		return nil
	}
	//i.Log.Info("Generating ingress", "ctrl", i)

	i.GenerateHostIngressMetadata()

	portStr, isHttps := i.MappedClass.Port()
	port := intstr.Parse(portStr)
	var backend netv1.IngressBackend
	if i.MappedClass.NodePort != nil {
		svc := &corev1.Service{}
		svcKey := client.ObjectKey{Namespace: i.MappedClass.NodePort.Namespace, Name: i.MappedClass.NodePort.Name}
		err := i.Guest.Get(i.Ctx, svcKey, svc)
		if err != nil {
			return err
		}
		backend = netv1.IngressBackend{
			Service: &netv1.IngressServiceBackend{
				Name: releaseConfig.LoadBalancerFullname,
			},
		}
		for _, svcPort := range svc.Spec.Ports {
			if (port.Type == intstr.String && svcPort.Name == port.StrVal) || (port.Type == intstr.Int && svcPort.Port == port.IntVal) {
				if svcPort.NodePort == 0 {
					i.Log.Info("Guest NodePort service does not have a node port set yet, ignoring")
					return nil
				}
				backend.Service.Port.Number = svcPort.NodePort
				break
			}
		}
		if backend.Service.Port.Number == 0 {
			return fmt.Errorf("Guest NodePort service does not have a port named/numbered %s", portStr)
		}
	} else if i.MappedClass.HostPort != nil {
		backend = netv1.IngressBackend{
			Service: &netv1.IngressServiceBackend{
				Name: releaseConfig.LoadBalancerIngress.HostPortTargetFullname,
			},
		}
		if port.Type == intstr.String {
			backend.Service.Port.Name = port.StrVal
		}
		if port.Type == intstr.Int {
			backend.Service.Port.Number = port.IntVal
		}
	} else {
		return fmt.Errorf("Guest class %s has neither hostPort nor nodePort set", i.GuestClass)
	}

	target := i.Targets[i.GuestClass]
	if target == nil {
		target = &netv1.Ingress{}
		i.Targets[i.GuestClass] = target
	}

	target.Spec.IngressClassName = &i.MappedClass.ClassName

	if isHttps {
		target.Spec.TLS = make([]netv1.IngressTLS, 0)
	}

	allHosts := make(map[string]struct{})
	rules := make([]netv1.IngressRule, 0)
	target.Spec.Rules = rules
	classIngresses, ok := i.ClassIngresses[i.GuestClass]
	if !ok {
		return nil
	}

	for ns, nsIngresses := range classIngresses {
		for name := range nsIngresses {
			for hostname, paths := range i.IngressPaths[ns][name] {
				if len(paths) == 0 {
					continue
				}
				allHosts[hostname] = struct{}{}
				rule := netv1.IngressRule{
					Host: hostname,
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: make([]netv1.HTTPIngressPath, 0, len(paths)),
						},
					},
				}

				for _, path := range paths {
					path.Backend = backend
					rule.HTTP.Paths = append(rule.HTTP.Paths, path)
				}

				rules = append(rules, rule)
			}
		}
	}
	target.Spec.Rules = rules

	if isHttps {
		target.Spec.TLS = append(target.Spec.TLS, netv1.IngressTLS{
			Hosts: []string{},
		})
		for host := range allHosts {
			target.Spec.TLS[0].Hosts = append(target.Spec.TLS[0].Hosts, host)
		}
	}

	return nil
}
func (i *IngressControllerRun) CreateOrUpdateHostIngress() error {
	i.GenerateHostIngress()
	if len(i.Targets[i.GuestClass].Spec.Rules) == 0 {
		i.Log.Info("Removing host ingress with no rules")
		err := i.Host.Delete(i.Ctx, i.Targets[i.GuestClass])
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	i.Log.Info("Regenerating host ingress")
	_, err := controllerutil.CreateOrUpdate(i.Ctx, i.Host, i.Targets[i.GuestClass], func() error {
		i.GenerateHostIngress()
		return nil
	})
	return err
}
