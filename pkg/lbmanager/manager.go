package lbmanager

import (
	"context"
	"fmt"
	"hash/adler32"
	"strconv"
	"time"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	cfg "github.com/meln5674/kink/pkg/config"
)

const (
	IngressClassNameAnnotation = "kubernetes.io/ingress.class"
	GuestClassLabel            = "kink.meln5674.github.com/guest-ingress-class"

	ServiceFinalizer = "kink.meln5674.github.com/lb-manager-svc"
	IngressFinalizer = "kink.meln5674.github.com/lb-manager-ingress"
)

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
	ServiceType      corev1.ServiceType
	ServiceNodePorts map[string]map[string][]int32
	LBSvc            *corev1.Service
	RequeueDelay     time.Duration
	ReleaseNamespace string
	ReleaseConfig    cfg.ReleaseConfig
}

func (s *ServiceController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	key := client.ObjectKey(req.NamespacedName)
	svc := &corev1.Service{}
	err := s.Guest.Get(ctx, key, svc)
	if kerrors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: s.RequeueDelay}, err
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
		return ctrl.Result{Requeue: true, RequeueAfter: s.RequeueDelay}, err
	}
	if deleted {
		return ctrl.Result{}, nil
	}

	run.UpsertPorts()
	err = run.CreateOrUpdateHostLB()
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: s.RequeueDelay}, err
	}
	err = run.SetLBIngress()
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: s.RequeueDelay}, err
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
	s.LBSvc.Name = s.ReleaseConfig.LoadBalancerFullname
	s.LBSvc.Namespace = s.ReleaseNamespace
	if s.LBSvc.Labels == nil {
		s.LBSvc.Labels = make(map[string]string, len(s.ReleaseConfig.LoadBalancerLabels))
	}
	for k, v := range s.ReleaseConfig.LoadBalancerLabels {
		s.LBSvc.Labels[k] = v
	}
	if s.LBSvc.Annotations == nil {
		s.LBSvc.Annotations = make(map[string]string, len(s.ReleaseConfig.LoadBalancerLabels))
	}
	for k, v := range s.ReleaseConfig.LoadBalancerServiceAnnotations {
		s.LBSvc.Annotations[k] = v
	}
}

func (s *ServiceControllerRun) GenerateHostLB() error {
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

	s.LBSvc.Spec.Type = s.ServiceType
	s.LBSvc.Spec.Ports = ports
	s.LBSvc.Spec.Selector = s.ReleaseConfig.LoadBalancerSelectorLabels

	return nil
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
	_, err := controllerutil.CreateOrUpdate(s.Ctx, s.Host, s.LBSvc, s.GenerateHostLB)
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
	IngressPaths     map[string]map[string]map[string][]netv1.HTTPIngressPath
	RequeueDelay     time.Duration
	ReleaseNamespace string
	ReleaseConfig    cfg.ReleaseConfig
}

func (i *IngressController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	key := client.ObjectKey(req.NamespacedName)
	ing := &netv1.Ingress{}
	err := i.Guest.Get(ctx, key, ing)
	if kerrors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: i.RequeueDelay}, err
	}

	guestClass, ok := GetClassName(ing)
	var mappedClass *cfg.LoadBalancerIngressClassMapping
	if ok {
		mappedClassT, ok := i.ReleaseConfig.LoadBalancerIngress.ClassMappings[guestClass]
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

	if mappedClass == nil {
		return ctrl.Result{}, nil
	}

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
		return ctrl.Result{Requeue: true, RequeueAfter: i.RequeueDelay}, err
	}
	if deleted {
		return ctrl.Result{}, nil
	}

	oldClass := run.UpsertPaths()
	if run.GuestClass != "" {
		err = run.CreateOrUpdateHostIngress(run.GuestClass)
		if err != nil {
			return ctrl.Result{Requeue: true, RequeueAfter: i.RequeueDelay}, err
		}
	}
	if oldClass != "" && oldClass != run.GuestClass {
		log.Info("Class has changed, need to update old ingress as well")
		err = run.CreateOrUpdateHostIngress(oldClass)
		if err != nil {
			return ctrl.Result{Requeue: true, RequeueAfter: i.RequeueDelay}, err
		}
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
		i.Log.Info("Removing paths for ingress being deleted")
		i.RemovePaths()
		if i.GuestClass != "" {
			i.Log.Info("Regenerating host ingress")
			err = i.CreateOrUpdateHostIngress(i.GuestClass)
			if err != nil {
				return false, nil
			}
		}
		i.Log.Info("Removing finalizer")
		controllerutil.RemoveFinalizer(i.Ingress, IngressFinalizer)
		err = i.Guest.Update(i.Ctx, i.Ingress)
		if err != nil {
			return false, nil
		}
		delete(i.Targets, i.GuestClass)
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

func (i *IngressControllerRun) UpsertPaths() (oldClass string) {
	nsClasses, ok := i.IngressClasses[i.Ingress.Namespace]
	if !ok {
		nsClasses = make(map[string]string)
		i.IngressClasses[i.Ingress.Namespace] = nsClasses
	}
	oldClass, ok = nsClasses[i.Ingress.Name]
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
		return oldClass
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

	return oldClass
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
		i.Log.Info("Generating fresh host ingress for new guest class")
		target = &netv1.Ingress{}
		i.Targets[i.GuestClass] = target
	}
	target.Name = fmt.Sprintf("%s-%s", i.ReleaseConfig.LoadBalancerFullname, i.GuestClass)
	target.Namespace = i.ReleaseNamespace
	if target.Annotations == nil {
		target.Annotations = make(map[string]string, len(i.MappedClass.Annotations))
	}
	for k, v := range i.MappedClass.Annotations {
		target.Annotations[k] = v
	}
	if target.Labels == nil {
		target.Labels = make(map[string]string, len(i.ReleaseConfig.LoadBalancerLabels)+1)
	}
	for k, v := range i.ReleaseConfig.LoadBalancerLabels {
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
				Name: i.ReleaseConfig.LoadBalancerFullname,
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
				Name: i.ReleaseConfig.LoadBalancerIngress.HostPortTargetFullname,
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
func (i *IngressControllerRun) CreateOrUpdateHostIngress(guestClass string) error {
	err := i.GenerateHostIngress()
	if err != nil {
		return err
	}
	i.Log.Info("Generated ingress", "host-ingress", *i.Targets[guestClass])

	if len(i.Targets[guestClass].Spec.Rules) != 0 {
		i.Log.Info("Upserting host ingress", "host-ingress", *i.Targets[guestClass])
		_, err = controllerutil.CreateOrUpdate(i.Ctx, i.Host, i.Targets[guestClass], i.GenerateHostIngress)
		return err
	}

	i.Log.Info("Removing host ingress with no rules", "host-ingress", *i.Targets[guestClass])
	err = i.Host.Delete(i.Ctx, i.Targets[guestClass])
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}
		i.Log.Info("Host ingress was already deleted?")
	}
	for {
		err = i.Host.Get(i.Ctx, client.ObjectKeyFromObject(i.Targets[guestClass]), i.Targets[guestClass])
		if kerrors.IsNotFound(err) {
			i.Log.Info("Host ingress was removed")
			return nil
		}
		if err != nil {
			return err
		}
		i.Log.Info("Host ingress still exists after deletion", "host-ingress", *i.Targets[guestClass])
	}
}
