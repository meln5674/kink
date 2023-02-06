/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"hash/adler32"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	netv1client "k8s.io/client-go/kubernetes/typed/networking/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"

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
)

const (
	IngressClassNameAnnotation = "kubernetes.io/ingress.class"
	GuestClassLabel            = "kink.meln5674.github.com/guest-ingress-class"
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
			var err error
			ctx, stop := context.WithCancel(context.TODO())
			defer stop()

			guestConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				&clientcmd.ClientConfigLoadingRules{
					ExplicitPath: guestKubeConfig.Kubeconfig,
				},
				&guestKubeConfig.ConfigOverrides,
			).ClientConfig()
			if err != nil {
				return err
			}

			hostClient, err := kubernetes.NewForConfig(kubeconfig)
			if err != nil {
				return err
			}

			guestClient, err := kubernetes.NewForConfig(guestConfig)
			if err != nil {
				return err
			}

			guestInformer := informers.NewSharedInformerFactory(guestClient, 5*time.Minute)

			if lbSvcLeaderElectionEnabled {
				leaderChan := make(chan struct{})
				leaderLock, err := resourcelock.NewFromKubeconfig(
					resourcelock.LeasesResourceLock,
					releaseNamespace,
					releaseConfig.LBManagerFullname,
					resourcelock.ResourceLockConfig{Identity: lbSvcLeaderElectionIdentity},
					kubeconfig,
					lbSvcLeaderElectionRenew,
				)
				if err != nil {
					return err
				}
				elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
					Lock:          leaderLock,
					Name:          releaseConfig.LoadBalancerFullname,
					LeaseDuration: lbSvcLeaderElectionLease,
					RenewDeadline: lbSvcLeaderElectionRenew,
					RetryPeriod:   lbSvcLeaderElectionRetry,
					Callbacks: leaderelection.LeaderCallbacks{
						OnStartedLeading: func(context.Context) {
							klog.Info("Became Leader")
							leaderChan <- struct{}{}
						},
						OnStoppedLeading: func() {
							klog.Info("No longer the leader")
							stop()
						},
					},
					ReleaseOnCancel: true,
				})
				if err != nil {
					return err
				}
				go elector.Run(ctx)

				_ = <-leaderChan
			}

			svcHandler := ServiceEventHandler{
				Host:  hostClient.CoreV1(),
				Guest: guestClient.CoreV1(),
				Ctx:   ctx,
				Target: corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:        releaseConfig.LoadBalancerFullname,
						Namespace:   releaseNamespace,
						Labels:      releaseConfig.LoadBalancerLabels,
						Annotations: releaseConfig.LoadBalancerServiceAnnotations,
					},
					Spec: corev1.ServiceSpec{
						Type:     corev1.ServiceTypeClusterIP, // TODO: Should this be configurable?
						Ports:    make([]corev1.ServicePort, 0),
						Selector: releaseConfig.LoadBalancerSelectorLabels,
					},
				},
				NodePorts: make(map[int32]corev1.ServicePort),
			}

			err = svcHandler.InitNodePorts()
			if err != nil {
				return err
			}
			err = svcHandler.CreateOrUpdateHostLB()
			if err != nil {
				return err
			}

			guestInformer.Core().V1().Services().Informer().AddEventHandler(&svcHandler)
			klog.Info("Starting guest cluster service watch")

			if releaseConfig.LoadBalancerIngress.Enabled {
				ingHandler := IngressEventHandler{
					HostSvc:  hostClient.CoreV1(),
					GuestSvc: guestClient.CoreV1(),
					HostIng:  hostClient.NetworkingV1(),
					GuestIng: guestClient.NetworkingV1(),
					Ctx:      ctx,
					Targets:  make(map[string]netv1.Ingress, len(releaseConfig.LoadBalancerIngress.ClassMappings)),
					Paths:    make(map[string]map[string]map[HashableHTTPIngressPath]netv1.HTTPIngressPath, len(releaseConfig.LoadBalancerIngress.ClassMappings)),
				}
				for class, mapping := range releaseConfig.LoadBalancerIngress.ClassMappings {
					ingHandler.Targets[class] = netv1.Ingress{
						ObjectMeta: metav1.ObjectMeta{
							Name:        fmt.Sprintf("%s-%s", releaseConfig.LoadBalancerFullname, class),
							Namespace:   releaseNamespace,
							Labels:      make(map[string]string, len(releaseConfig.LoadBalancerLabels)+1),
							Annotations: mapping.Annotations,
						},
						Spec: netv1.IngressSpec{
							IngressClassName: new(string),
						},
					}
					for k, v := range releaseConfig.LoadBalancerLabels {
						ingHandler.Targets[class].Labels[k] = v
					}
					ingHandler.Targets[class].Labels[GuestClassLabel] = class
					*ingHandler.Targets[class].Spec.IngressClassName = mapping.ClassName
					ingHandler.Paths[class] = make(map[string]map[HashableHTTPIngressPath]netv1.HTTPIngressPath)
				}

				guestInformer.Networking().V1().Ingresses().Informer().AddEventHandler(&ingHandler)
				klog.Info("Starting guest cluster ingress watch")
			}
			guestInformer.Start(ctx.Done())

			sigChan := make(chan os.Signal, 2)

			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			_ = <-sigChan

			klog.Info("Exiting")
			return nil
		}()

		if err != nil {
			klog.Fatal(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(lbManagerCmd)
	lbManagerCmd.PersistentFlags().StringVar(&guestKubeConfig.Kubeconfig, "guest-kubeconfig", "", "Path to the kubeconfig file to use for accessing the guest cluster")
	lbManagerCmd.PersistentFlags().BoolVar(&lbSvcLeaderElectionEnabled, "leader-election", false, "Enable leader election. Required if more than one replica is running")
	lbManagerCmd.PersistentFlags().StringVar(&lbSvcLeaderElectionIdentity, "leader-election-id", "", "Identity for leader election")
	lbManagerCmd.PersistentFlags().DurationVar(&lbSvcLeaderElectionLease, "leader-election-lease", 15*time.Second, "Lease duration for leader election")
	lbManagerCmd.PersistentFlags().DurationVar(&lbSvcLeaderElectionRenew, "leader-election-renew", 10*time.Second, "Renewal deadline for leader election")
	lbManagerCmd.PersistentFlags().DurationVar(&lbSvcLeaderElectionRetry, "leader-election-retry", 2*time.Second, "Retry period for leader election")
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

type ServiceEventHandler struct {
	Host      corev1client.ServicesGetter
	Guest     corev1client.ServicesGetter
	Ctx       context.Context
	Target    corev1.Service
	NodePorts map[int32]corev1.ServicePort
}

func (s *ServiceEventHandler) SetPorts() {
	s.Target.Labels = releaseConfig.LoadBalancerLabels
	s.Target.Annotations = releaseConfig.LoadBalancerServiceAnnotations
	s.Target.Spec.Selector = releaseConfig.LoadBalancerSelectorLabels
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
	s.Target.Spec.Ports = ports
}

func (s *ServiceEventHandler) AddPortsFor(svc *corev1.Service) {
	for _, port := range svc.Spec.Ports {
		s.NodePorts[port.NodePort] = ConvertPort(svc.Namespace, svc.Name, &port)
	}
}

func (s *ServiceEventHandler) RemovePortsFor(svc *corev1.Service) {
	if svc.Spec.Type == corev1.ServiceTypeNodePort || svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		for _, port := range svc.Spec.Ports {
			delete(s.NodePorts, port.NodePort)
		}
	}
}

func (s *ServiceEventHandler) InitNodePorts() error {
	klog.Info("Fetching initial state of LB service")
	svc, err := s.Host.Services(s.Target.Namespace).Get(s.Ctx, s.Target.Name, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	s.Target = *svc
	for _, port := range s.Target.Spec.Ports {
		s.NodePorts[int32(port.TargetPort.IntValue())] = port
	}
	return nil
}

func (s *ServiceEventHandler) CreateOrUpdateHostLB() error {
	svc, err := s.Host.Services(s.Target.Namespace).Get(s.Ctx, s.Target.Name, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		s.SetPorts()
		klog.Infof("Generated Service: %#v", s.Target)
		svc, err := s.Host.Services(s.Target.Namespace).Create(s.Ctx, &s.Target, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		s.Target = *svc
		return nil
	}
	if err != nil {
		return err
	}

	s.SetPorts()
	klog.Infof("Generated Service: %#v", s.Target)
	svc, err = s.Host.Services(s.Target.Namespace).Update(s.Ctx, &s.Target, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	s.Target = *svc
	return nil
}

func (s *ServiceEventHandler) SetLBIngress(svc *corev1.Service) error {
	klog.Infof("Setting LoadBalancer IP for %s/%s", svc.Namespace, svc.Name)
	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
		{
			IP: s.Target.Spec.ClusterIP,
		},
	}
	newSvc, err := s.Guest.Services(svc.Namespace).UpdateStatus(s.Ctx, svc, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*svc = *newSvc
	return nil
}

func (s *ServiceEventHandler) OnAdd(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		klog.Warning("Got unexpected guest resource %s", objString(obj))
		return
	}

	if !(svc.Spec.Type == corev1.ServiceTypeNodePort || svc.Spec.Type == corev1.ServiceTypeLoadBalancer) {
		klog.V(1).Info("Ignoring %s guest service", svc.Spec.Type)
		return
	}
	klog.Info("Got new guest service", objString(obj))

	s.AddPortsFor(svc)

	err := s.CreateOrUpdateHostLB()
	if err != nil {
		klog.Error(err)
		return
	}

	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		klog.V(1).Info("Not setting LB ingress IP for %s guest service", svc.Spec.Type)
		return
	}

	err = s.SetLBIngress(svc)
	if err != nil {
		klog.Error(err)
		return
	}
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

func (s *ServiceEventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldSvc, ok := oldObj.(*corev1.Service)
	if !ok {
		klog.Warning("Got unexpected guest resource %s", objString(oldObj))
		return
	}
	newSvc, ok := newObj.(*corev1.Service)
	if !ok {
		klog.Warning("Got unexpected guest resource %s", objString(newObj))
		return
	}

	klog.Info("Got updated guest service ", objString(newObj))

	if oldSvc.Spec.Type == corev1.ServiceTypeNodePort || oldSvc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		s.RemovePortsFor(oldSvc)
	}

	if !(newSvc.Spec.Type == corev1.ServiceTypeNodePort || newSvc.Spec.Type == corev1.ServiceTypeLoadBalancer) {
		return
	}

	s.AddPortsFor(newSvc)

	err := s.CreateOrUpdateHostLB()
	if err != nil {
		klog.Error(err)
		return
	}

	if newSvc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		klog.Info("Ignoring/removing ports for %s service", newSvc.Spec.Type)
		return
	}

	err = s.SetLBIngress(newSvc)
	if err != nil {
		klog.Error(err)
	}
}

func (s *ServiceEventHandler) OnDelete(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		klog.Warning("Got unexpected guest resource %s", objString(obj))
		return
	}
	klog.Info("Got deleted guest service %s", objString(obj))

	s.RemovePortsFor(svc)

	err := s.CreateOrUpdateHostLB()
	if err != nil {
		klog.Error(err)
		return
	}
}

// Same fields as netv1.HTTPIngressPath, but fixes them not hashing to the same even when the fields are the same due to pointers
type HashableHTTPIngressPath struct {
	Path                     string
	PathType                 netv1.PathType
	BackendServiceName       string
	BackendServicePortNumber int32
	BackendServicePortName   string
}

func ToHashable(path *netv1.HTTPIngressPath) HashableHTTPIngressPath {
	hashable := HashableHTTPIngressPath{
		Path: path.Path,
	}
	if path.PathType != nil {
		hashable.PathType = *path.PathType
	}
	if path.Backend.Service != nil {
		hashable.BackendServiceName = path.Backend.Service.Name
		hashable.BackendServicePortNumber = path.Backend.Service.Port.Number
		hashable.BackendServicePortName = path.Backend.Service.Port.Name
	}
	return hashable
}

type IngressEventHandler struct {
	HostSvc  corev1client.ServicesGetter
	GuestSvc corev1client.ServicesGetter
	HostIng  netv1client.IngressesGetter
	GuestIng netv1client.IngressesGetter
	Ctx      context.Context
	Targets  map[string]netv1.Ingress
	Paths    map[string]map[string]map[HashableHTTPIngressPath]netv1.HTTPIngressPath
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

func (i *IngressEventHandler) AddPathsFor(guestClass string, ing *netv1.Ingress) error {
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		pathset, ok := i.Paths[guestClass][rule.Host]
		if !ok {
			pathset = make(map[HashableHTTPIngressPath]netv1.HTTPIngressPath)
			i.Paths[guestClass][rule.Host] = pathset
		}
		for _, path := range rule.HTTP.Paths {
			pathset[ToHashable(&path)] = path
		}
	}
	return nil
}

func (i *IngressEventHandler) RemovePathsFor(guestClass string, ing *netv1.Ingress) error {
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		pathset, ok := i.Paths[guestClass][rule.Host]
		if !ok {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			delete(pathset, ToHashable(&path))
		}
		if len(pathset) == 0 {
			delete(i.Paths[guestClass], rule.Host)
		}
	}
	return nil
}

func (i *IngressEventHandler) SetRules(guestClass string, mappedClass *cfg.LoadBalancerIngressClassMapping, target *netv1.Ingress) error {
	target.Annotations = mappedClass.Annotations
	target.Labels = make(map[string]string, len(releaseConfig.LoadBalancerLabels)+1)
	for k, v := range releaseConfig.LoadBalancerLabels {
		target.Labels[k] = v
	}
	target.Labels[GuestClassLabel] = guestClass
	target.Spec.Rules = make([]netv1.IngressRule, 0, len(i.Paths[guestClass]))
	portStr, isHttps := mappedClass.Port()
	if isHttps {
		target.Spec.TLS = make([]netv1.IngressTLS, 0, len(i.Paths[guestClass]))
	}
	port := intstr.Parse(portStr)
	var backend netv1.IngressBackend
	if mappedClass.NodePort != nil {
		svc, err := i.GuestSvc.Services(mappedClass.NodePort.Namespace).Get(i.Ctx, mappedClass.NodePort.Name, metav1.GetOptions{})
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
					klog.Warningf("Guest NodePort service %s/%s for guest class %s does not have a node port set yet, ignoring", svc.Namespace, svc.Name, guestClass)
					return nil
				}
				backend.Service.Port.Number = svcPort.NodePort
				break
			}
		}
		if backend.Service.Port.Number == 0 {
			return fmt.Errorf("Guest NodePort service %s/%s for guest class %s does not have a port named/numbered %s", svc.Namespace, svc.Name, guestClass, portStr)
		}
	} else if mappedClass.HostPort != nil {
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
		return fmt.Errorf("Guest class %s has neither hostPort nor nodePort set", guestClass)
	}

	for hostname, paths := range i.Paths[guestClass] {
		if len(paths) == 0 {
			continue
		}
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

		target.Spec.Rules = append(target.Spec.Rules, rule)

		if isHttps {
			target.Spec.TLS = append(target.Spec.TLS, netv1.IngressTLS{
				Hosts: []string{rule.Host},
			})
		}
	}
	return nil
}
func (i *IngressEventHandler) CreateOrUpdateHostIngress(guestClass string, mappedClass *cfg.LoadBalancerIngressClassMapping, target *netv1.Ingress) error {

	ing, err := i.HostIng.Ingresses(target.Namespace).Get(i.Ctx, target.Name, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		err = i.SetRules(guestClass, mappedClass, target)
		if err != nil {
			return err
		}
		klog.Infof("Generated Ingress for guest class %s: %#v", guestClass, target)
		if len(target.Spec.Rules) == 0 {
			klog.Info("Not creating ingress with no rules")
			return nil
		}
		ing, err = i.HostIng.Ingresses(target.Namespace).Create(i.Ctx, target, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		*target = *ing
		return nil
	}
	if err != nil {
		return err
	}

	err = i.SetRules(guestClass, mappedClass, target)
	if err != nil {
		return err
	}

	klog.Infof("Generated Ingress for guest class %s: %#v", guestClass, target)
	if len(target.Spec.Rules) == 0 {
		klog.Info("Deleting ingress with no rules")
		err = i.HostIng.Ingresses(target.Namespace).Delete(i.Ctx, target.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
		return nil
	}

	ing, err = i.HostIng.Ingresses(target.Namespace).Update(i.Ctx, target, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*target = *ing
	return nil
}

func (i *IngressEventHandler) OnAdd(obj interface{}) {
	ing, ok := obj.(*netv1.Ingress)
	if !ok {
		klog.Warning("Got unexpected guest resource %s", objString(obj))
		return
	}
	klog.Info("Got new guest ingress", objString(obj))

	className, ok := GetClassName(ing)
	if !ok {
		klog.Info("Ignoring class-less guest ingress")
		return
	}
	mappedClass, ok := releaseConfig.LoadBalancerIngress.ClassMappings[className]
	if !ok {
		klog.Infof("Ignoring guest ingress with unmapped class %s", className)
		return
	}
	i.AddPathsFor(className, ing)

	target := i.Targets[className]
	err := i.CreateOrUpdateHostIngress(className, &mappedClass, &target)
	if err != nil {
		klog.Error(err)
		return
	}
	i.Targets[className] = target
}

func (i *IngressEventHandler) OnUpdate(oldObj, newObj interface{}) {
	oldIng, ok := oldObj.(*netv1.Ingress)
	if !ok {
		klog.Warning("Got unexpected guest resource %s", objString(oldObj))
		return
	}
	newIng, ok := newObj.(*netv1.Ingress)
	if !ok {
		klog.Warning("Got unexpected guest resource %s", objString(newObj))
		return
	}
	klog.Info("Got updated guest ingress ", objString(newObj))

	oldClassName, hadClass := GetClassName(oldIng)
	var oldMappedClass cfg.LoadBalancerIngressClassMapping
	var hadKnownClass bool
	if !hadClass {
		klog.Info("Not removing paths for previously class-less guest ingress")
	} else if oldMappedClass, hadKnownClass = releaseConfig.LoadBalancerIngress.ClassMappings[oldClassName]; !ok {
		klog.Infof("Not removing paths for guest ingress with previously unknown class %s", oldClassName)
	} else {
		i.RemovePathsFor(oldClassName, oldIng)
	}

	newClassName, hasClass := GetClassName(newIng)
	var newMappedClass cfg.LoadBalancerIngressClassMapping
	var hasKnownClass bool
	if !hasClass {
		klog.Info("Not removing paths for previously class-less guest ingress")
	} else if newMappedClass, hadKnownClass = releaseConfig.LoadBalancerIngress.ClassMappings[newClassName]; !ok {
		klog.Infof("Not removing paths for guest ingress with unknown class %s", newClassName)
	} else {
		i.AddPathsFor(newClassName, newIng)
	}

	if hadKnownClass {
		target := i.Targets[oldClassName]
		err := i.CreateOrUpdateHostIngress(oldClassName, &oldMappedClass, &target)
		if err != nil {
			klog.Error(err)
			return
		}
		i.Targets[oldClassName] = target
	}

	if (!hadKnownClass && hasKnownClass) || (hadKnownClass && hasKnownClass && oldClassName != newClassName) {
		target := i.Targets[newClassName]
		err := i.CreateOrUpdateHostIngress(newClassName, &newMappedClass, &target)
		if err != nil {
			klog.Error(err)
			return
		}
		i.Targets[newClassName] = target

	}
}

func (i *IngressEventHandler) OnDelete(obj interface{}) {
	ing, ok := obj.(*netv1.Ingress)
	if !ok {
		klog.Warning("Got unexpected guest resource %s", objString(obj))
		return
	}
	klog.Info("Got deleted guest ingress %s", objString(obj))

	className, ok := GetClassName(ing)
	if !ok {
		klog.Info("Ignoring class-less guest ingress")
		return
	}
	mappedClass, ok := releaseConfig.LoadBalancerIngress.ClassMappings[className]
	if !ok {
		klog.Infof("Ingoring guest ingress with unknown class %s", className)
	}

	i.RemovePathsFor(className, ing)

	target := i.Targets[className]
	err := i.CreateOrUpdateHostIngress(className, &mappedClass, &target)
	if err != nil {
		klog.Error(err)
		return
	}
	i.Targets[className] = target
}
