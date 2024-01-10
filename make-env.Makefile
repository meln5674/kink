
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)
	touch $(LOCALBIN)
$(LOCALBIN)/: $(LOCALBIN)


GINKGO ?= $(LOCALBIN)/ginkgo
$(GINKGO):
	GOBIN=$(LOCALBIN)/.make-env/go/github.com/onsi/ginkgo/v2.ginkgo/v2.13.1 go install github.com/onsi/ginkgo/v2/ginkgo@v2.13.1
	rm -f $(GINKGO)
	ln -s $(LOCALBIN)/.make-env/go/github.com/onsi/ginkgo/v2.ginkgo/v2.13.1/ginkgo $(GINKGO)
.PHONY: ginkgo
ginkgo: $(GINKGO)


HELM ?= $(LOCALBIN)/helm
$(HELM):
	GOBIN=$(LOCALBIN)/.make-env/go/helm.sh/helm/v3.cmd/helm/v3.13.3 go install helm.sh/helm/v3/cmd/helm@v3.13.3
	rm -f $(HELM)
	ln -s $(LOCALBIN)/.make-env/go/helm.sh/helm/v3.cmd/helm/v3.13.3/helm $(HELM)
.PHONY: helm
helm: $(HELM)


KIND ?= $(LOCALBIN)/kind
$(KIND):
	GOBIN=$(LOCALBIN)/.make-env/go/sigs.k8s.io/kind/v0.17.0 go install sigs.k8s.io/kind@v0.17.0
	rm -f $(KIND)
	ln -s $(LOCALBIN)/.make-env/go/sigs.k8s.io/kind/v0.17.0/kind $(KIND)
.PHONY: kind
kind: $(KIND)


KUBECTL ?= $(LOCALBIN)/kubectl
KUBECTL_MIRROR ?= https://dl.k8s.io/release
KUBECTL_VERSION ?= v1.25.11

KUBECTL_URL ?= $(KUBECTL_MIRROR)/$(KUBECTL_VERSION)/bin/$(shell go env GOOS)/$(shell go env GOARCH)/kubectl
$(KUBECTL): 
	mkdir -p $(LOCALBIN)/.make-env/http
	curl -vfL $(KUBECTL_URL) -o $(LOCALBIN)/.make-env/http/$(shell base64 -w0 <<< $(KUBECTL_URL))
	chmod +x $(LOCALBIN)/.make-env/http/$(shell base64 -w0 <<< $(KUBECTL_URL))
	rm -f $(KUBECTL)
	ln -s $(LOCALBIN)/.make-env/http/$(shell base64 -w0 <<< $(KUBECTL_URL)) $(KUBECTL)
.PHONY: kubectl
kubectl: $(KUBECTL)


SETUP_ENVTEST ?= $(LOCALBIN)/setup-envtest
$(SETUP_ENVTEST):
	GOBIN=$(LOCALBIN)/.make-env/go/sigs.k8s.io/controller-runtime/tools/setup-envtest/latest go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	rm -f $(SETUP_ENVTEST)
	ln -s $(LOCALBIN)/.make-env/go/sigs.k8s.io/controller-runtime/tools/setup-envtest/latest/setup-envtest $(SETUP_ENVTEST)
.PHONY: setup-envtest
setup-envtest: $(SETUP_ENVTEST)
.PHONY: k8s-tools
k8s-tools: $(KIND) $(KUBECTL) $(HELM)
.PHONY: test-tools
test-tools: $(GINKGO) $(SETUP_ENVTEST)

make-env.Makefile: make-env.yaml
	make-env --config 'make-env.yaml' --out 'make-env.Makefile'
