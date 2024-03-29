all: lint bin/kink test

include make-env.Makefile

SHELL := /bin/bash


GO_FILES := $(shell find cmd/ -name '*.go') $(shell find pkg/ -name '*.go') go.mod go.sum
HELM_FILES := $(shell find helm/kink/) 

bin/kink: $(GO_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o bin/kink main.go

bin/kink.dev: $(GO_FILES)
	go build -o bin/kink.dev main.go

bin/kink.cover: $(GO_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build --cover -a -tags netgo -ldflags '-w -extldflags "-static"' -o bin/kink.cover main.go

ifndef CHART_VERSION
RAW_CHART_VERSION=$(shell helm show chart ./helm/kink/ | grep -E '^version: ' | awk '{ print $$2 }')
ifdef CHART_GIT_SHA
CHART_VERSION=$(RAW_CHART_VERSION)-$(CHART_GIT_SHA)
else
CHART_VERSION=$(RAW_CHART_VERSION)
endif
endif

ifndef CHART_APP_VERSION
RAW_CHART_VERSION=$(shell helm show chart ./helm/kink/ | grep -E '^version: ' | awk '{ print $$2 }')
ifdef CHART_GIT_SHA
CHART_APP_VERSION=sha-$(CHART_GIT_SHA)
else
CHART_APP_VERSION=v$(RAW_CHART_VERSION)
endif
endif

helm/kink/Chart.lock: $(HELM) helm/kink/Chart.yaml
	$(HELM) dependency update helm/kink/
	touch helm/kink/Chart.lock

bin/charts/kink-$(CHART_VERSION).tgz: $(HELM) $(HELM_FILES) helm/kink/Chart.lock
	mkdir -p bin/charts
	$(HELM) package ./helm/kink/ --version '$(CHART_VERSION)' --app-version '$(CHART_APP_VERSION)' --destination ./bin/charts/

.PHONY: chart
chart: bin/charts/kink-${CHART_VERSION}.tgz

vet:
	go vet ./cmd/... ./pkg/... ./e2e/...

.PHONY: lint
lint:
	# yq integration-test/*.yaml >/dev/null
	kind create cluster --name=helm-hog || true
	go vet ./cmd/... ./pkg/... ./e2e/...
	shopt -s globstar ; \
	if ! ( cd helm/kink ; helm-hog test --batch --parallel=0 --kubectl-flags=--context=kind-helm-hog,-v4 --keep-reports); then \
		for x in /tmp/helm-hog-*/**; do \
			echo "$$x"; \
			cat "$$x"; \
		done ; \
		exit 1 ; \
	fi

.PHONY: test
test: $(SETUP_ENVTEST) $(GINKGO)
	KUBEBUILDER_ASSETS="$(shell $(SETUP_ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" $(GINKGO) run -v -r --coverprofile=cover.out --coverpkg=./pkg/... ./pkg
	go tool cover -html=cover.out -o cover.html

GOCOVERDIR=$(shell pwd)/integration-test/gocov

E2E_FLAGS ?= -vv -p --trace --timeout=2h --flake-attempts=3
# E2E_FLAGS ?= -vv --trace --timeout=2h
E2E_IN_CLUSTER_FLAGS ?= -vv --trace --timeout=2h
.PHONY: e2e
e2e: bin/kink.cover $(GINKGO) $(KIND) $(KUBECTL) $(HELM) $(SETUP_ENVTEST)
ifndef KINK_IT_IN_CLUSTER
	./hack/inotify-check.sh
	rm -rf $(GOCOVERDIR)
	mkdir -p $(GOCOVERDIR)
	# Excessively long timeout is for github actions which are really slow
	set -o pipefail ; KINK_IT_REPO_ROOT=$(shell pwd) LOCALBIN=$(LOCALBIN) GOCOVERDIR=$(GOCOVERDIR) $(GINKGO) run $(E2E_FLAGS) ./e2e/ 2>&1 | tee integration-test/log
	./hack/fix-coverage-permissions.sh
	go tool covdata percent -i=$(GOCOVERDIR)
	go tool covdata textfmt -i=$(GOCOVERDIR) -o cover.e2e.out
	go tool cover -html=cover.e2e.out -o cover.e2e.html
else
	KINK_IT_REPO_ROOT=$(shell pwd) LOCALBIN=$(LOCALBIN) GOCOVERDIR=$(GOCOVERDIR) $(GINKGO) run $(E2E_IN_CLUSTER_FLAGS) ./e2e/
endif

.PHONY: test
clean-tests:
	./hack/clean-tests.sh
