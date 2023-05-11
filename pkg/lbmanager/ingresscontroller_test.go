package lbmanager_test

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	gomega "github.com/onsi/gomega/types"
)

func dummySingletonRule(host, svcName, svcPort, path string, pathType netv1.PathType) []netv1.IngressRule {
	return []netv1.IngressRule{
		{
			Host: host,
			IngressRuleValue: netv1.IngressRuleValue{
				HTTP: &netv1.HTTPIngressRuleValue{
					Paths: []netv1.HTTPIngressPath{
						{
							Path:     path,
							PathType: &pathType,
							Backend: netv1.IngressBackend{
								Service: &netv1.IngressServiceBackend{
									Name: svcName,
									Port: netv1.ServiceBackendPort{
										Name: svcPort,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789-")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		max := len(letterRunes)
		if i == 0 {
			max--
		}
		b[i] = letterRunes[rand.Intn(max)]
	}
	return string(b)
}

func createAndCleanup(ctx context.Context, obj client.Object) {
	GinkgoHelper()
	Expect(testGuest.k8sClient.Create(ctx, obj)).To(Succeed())
	DeferCleanup(func(ctx context.Context) {
		Expect(client.IgnoreNotFound(testGuest.k8sClient.Delete(ctx, obj))).To(Succeed())
	})
}

func matchBackendService(name, port string, portIsNumber bool) gomega.GomegaMatcher {
	expected := netv1.IngressServiceBackend{
		Name: name,
	}
	if portIsNumber {
		p, err := strconv.Atoi(port)
		Expect(err).ToNot(HaveOccurred())
		expected.Port.Number = int32(p)
	} else {
		expected.Port.Name = port
	}
	return Equal(expected)
}

func matchPath(path, svcName, svcPort string, pathType netv1.PathType, portIsNumber bool) gomega.GomegaMatcher {
	return And(
		WithTransform(
			func(p netv1.HTTPIngressPath) string { return p.Path },
			Equal(path),
		),
		WithTransform(
			func(p netv1.HTTPIngressPath) *netv1.PathType { return p.PathType },
			And(
				Not(BeNil()),
				WithTransform(
					func(t *netv1.PathType) netv1.PathType { return *t },
					Equal(pathType),
				),
			),
		),
		WithTransform(
			func(p netv1.HTTPIngressPath) *netv1.IngressServiceBackend { return p.Backend.Service },
			And(
				Not(BeNil()),
				WithTransform(
					func(b *netv1.IngressServiceBackend) netv1.IngressServiceBackend { return *b },
					matchBackendService(svcName, svcPort, portIsNumber),
				),
			),
		),
	)
}

func matchRule(host, path, svcName, svcPort string, pathType netv1.PathType, portIsNumber bool) gomega.GomegaMatcher {
	return And(
		WithTransform(
			func(rule netv1.IngressRule) string {
				return rule.Host
			},
			Equal(host),
		),
		WithTransform(
			func(rule netv1.IngressRule) []netv1.HTTPIngressPath {
				return rule.IngressRuleValue.HTTP.Paths
			},
			ContainElement(matchPath(path, svcName, svcPort, pathType, portIsNumber)),
		),
	)
}

func matchIngress(class, host, path, svcName, svcPort string, pathType netv1.PathType, portIsNumber bool) gomega.GomegaMatcher {
	return And(
		WithTransform(
			func(i *netv1.Ingress) *string { return i.Spec.IngressClassName },
			And(
				Not(BeNil()),
				WithTransform(
					func(s *string) string { return *s },
					Equal(class),
				),
			),
		),
		WithTransform(
			func(i *netv1.Ingress) []netv1.IngressRule { return i.Spec.Rules },
			ContainElement(matchRule(host, path, svcName, svcPort, pathType, portIsNumber)),
		),
	)
}

var _ = Describe("Ingress Controller", func() {
	unmappedClassName := "unmapped"
	httpHostNameClassName := "guest-http-host-name"

	var ns string

	BeforeEach(func(ctx context.Context) {
		ns = RandStringRunes(8)
		nsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}
		createAndCleanup(ctx, nsObj)
	})

	When("an unmapped guest ingress is created", func() {
		It("Should do nothing", func(ctx context.Context) {
			ing := &netv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-unmapped",
					Namespace: ns,
				},
				Spec: netv1.IngressSpec{
					IngressClassName: &unmappedClassName,
					Rules:            dummySingletonRule("test.unmapped", "test-unmapped", "test-unmapped", "/", netv1.PathTypePrefix),
				},
			}
			createAndCleanup(ctx, ing)
			Consistently(func(g Gomega) []netv1.Ingress {
				ings := &netv1.IngressList{}
				g.Expect(testHost.k8sClient.List(ctx, ings, client.InNamespace("default"))).To(Succeed())
				return ings.Items
			}, "10s").Should(HaveLen(0))
		})
	})

	When("a mapped guest ingress is created and deleted", func() {
		It("Should create and then delete the matching host ingress", func(ctx context.Context) {
			ingMeta := metav1.ObjectMeta{
				Name:      "test-create-delete",
				Namespace: ns,
			}
			ing := &netv1.Ingress{
				ObjectMeta: ingMeta,
				Spec: netv1.IngressSpec{
					IngressClassName: &httpHostNameClassName,
					Rules:            dummySingletonRule("test.create-delete", "test-create-delete", "http", "/", netv1.PathTypePrefix),
				},
			}
			rules := ing.Spec.Rules
			paths := ing.Spec.Rules[0].IngressRuleValue.HTTP.Paths
			By("Creating Guest Ingress")
			createAndCleanup(ctx, ing)

			hostIngKey := client.ObjectKey{
				Namespace: "default",
				Name:      fmt.Sprintf("%s-%s", releaseConfig.LoadBalancerFullname, httpHostNameClassName),
			}
			hostIng := &netv1.Ingress{}

			By("Waiting for Host Ingress to be Created")
			Eventually(func() error {
				return testHost.k8sClient.Get(ctx, hostIngKey, hostIng)
			}, "5s").Should(Succeed())

			savedMaxLength := format.MaxLength
			format.MaxLength = 10000
			DeferCleanup(func() {
				format.MaxLength = savedMaxLength
			})

			By("Validating Host Ingress")
			Expect(hostIng).To(matchIngress("host-http-host-name", rules[0].Host, paths[0].Path, "test-lb", "http", *paths[0].PathType, false))

			By("Deleting Guest Ingress")
			Expect(testGuest.k8sClient.Delete(ctx, ing)).To(Succeed())

			By("Waiting for Host Ingress to be Deleted")
			Eventually(func() error {
				return testHost.k8sReader.Get(ctx, hostIngKey, hostIng)
			}, "1s").ShouldNot(Succeed())

			By("Waiting for Guest Ingress to no longer Exist")
			Eventually(func() error {
				return testGuest.k8sReader.Get(ctx, hostIngKey, hostIng)
			}, "1s").ShouldNot(Succeed())

		})
	})

	When("a mapped guest ingress is created, deleted, and re-created and deleted", func() {
		It("Should create and then delete the matching host ingress both times", func(ctx context.Context) {
			ingMeta := metav1.ObjectMeta{
				Name:      "test-create-delete-again",
				Namespace: ns,
			}
			ing := &netv1.Ingress{
				ObjectMeta: ingMeta,
				Spec: netv1.IngressSpec{
					IngressClassName: &httpHostNameClassName,
					Rules:            dummySingletonRule("test.create-delete-again", "test-create-delete-again", "http", "/", netv1.PathTypePrefix),
				},
			}
			rules := ing.Spec.Rules
			paths := ing.Spec.Rules[0].IngressRuleValue.HTTP.Paths
			By("Creating Guest Ingress")
			createAndCleanup(ctx, ing)

			hostIngKey := client.ObjectKey{
				Namespace: "default",
				Name:      fmt.Sprintf("%s-%s", releaseConfig.LoadBalancerFullname, httpHostNameClassName),
			}
			hostIng := &netv1.Ingress{}

			By("Waiting for Host Ingress to be Created")
			Eventually(func() error {
				return testHost.k8sClient.Get(ctx, hostIngKey, hostIng)
			}, "5s").Should(Succeed())

			savedMaxLength := format.MaxLength
			format.MaxLength = 10000
			DeferCleanup(func() {
				format.MaxLength = savedMaxLength
			})

			By("Validating Host Ingress")
			Expect(hostIng).To(matchIngress("host-http-host-name", rules[0].Host, paths[0].Path, "test-lb", "http", *paths[0].PathType, false))

			By("Deleting Guest Ingress")
			Expect(testGuest.k8sClient.Delete(ctx, ing)).To(Succeed())

			By("Waiting for Host Ingress to be Deleted")
			Eventually(func() error {
				return testHost.k8sReader.Get(ctx, hostIngKey, hostIng)
			}, "5s").ShouldNot(Succeed())

			By("Recreating Guest Ingress")
			ing.ObjectMeta = ingMeta
			createAndCleanup(ctx, ing)

			By("Waiting for Host Ingress to be Recreated")
			Eventually(func() error {
				return testHost.k8sClient.Get(ctx, hostIngKey, hostIng)
			}, "5s").Should(Succeed())

			By("Redeleting Guest Ingress")
			Expect(testGuest.k8sClient.Delete(ctx, ing)).To(Succeed())

			By("Waiting for Host Ingress to be Redeleted")
			Eventually(func() error {
				return testHost.k8sReader.Get(ctx, hostIngKey, hostIng)
			}, "1s").ShouldNot(Succeed())
		})
	})
})
