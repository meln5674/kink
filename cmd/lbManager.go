/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"

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

			handler := ServiceEventHandler{
				Host:  hostClient.CoreV1(),
				Guest: guestClient.CoreV1(),
				Ctx:   ctx,
				Target: corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:        releaseConfig.LoadBalancerFullname,
						Namespace:   releaseNamespace,
						Labels:      releaseConfig.LoadBalancerLabels,
						Annotations: releaseConfig.LoadBalancerAnnotations,
					},
					Spec: corev1.ServiceSpec{
						Type:     corev1.ServiceTypeClusterIP, // TODO: Should this be configurable?
						Ports:    make([]corev1.ServicePort, 0),
						Selector: releaseConfig.WorkerSelectorLabels,
					},
				},
				NodePorts: make(map[int32]corev1.ServicePort),
			}

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
					Name:          handler.Target.Name,
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

			err = handler.InitNodePorts()
			if err != nil {
				return err
			}
			err = handler.CreateOrUpdateHostLB()
			if err != nil {
				return err
			}

			guestInformer.Core().V1().Services().Informer().AddEventHandler(&handler)
			klog.Info("Starting guest cluster service watch")
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
		s.NodePorts[port.NodePort] = ConvertPort(&port)
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
	klog.Infof("Generated Service: %#v", s.Target)

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
		return
	}
	klog.Info("Got new guest service %s", objString(obj))

	s.AddPortsFor(svc)

	err := s.CreateOrUpdateHostLB()
	if err != nil {
		klog.Error(err)
		return
	}

	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		klog.Info("Ignoring %s guest service", svc.Spec.Type)
		return
	}

	err = s.SetLBIngress(svc)
	if err != nil {
		klog.Error(err)
		return
	}
}

func ConvertPort(port *corev1.ServicePort) corev1.ServicePort {
	return corev1.ServicePort{
		Name:        fmt.Sprintf("%d", port.NodePort),
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

	klog.Info("Got updated guest service %s", objString(newObj))

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

	s.RemovePortsFor(svc)

	err := s.CreateOrUpdateHostLB()
	if err != nil {
		klog.Error(err)
		return
	}
}
