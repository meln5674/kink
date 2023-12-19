all: lint bin/kink test

SHELL := /bin/bash

.PHONY: lint test

GO_FILES := $(shell find cmd/ -name '*.go') $(shell find pkg/ -name '*.go') go.mod go.sum

bin/kink: $(GO_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o bin/kink main.go

bin/kink.dev: $(GO_FILES)
	go build -o bin/kink.dev main.go

bin/kink.cover: $(GO_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build --cover -a -tags netgo -ldflags '-w -extldflags "-static"' -o bin/kink.cover main.go

vet:
	go vet ./cmd/... ./pkg/... ./e2e/...

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


test: envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" ginkgo run -v -r --coverprofile=cover.out --coverpkg=./pkg/... ./pkg
	go tool cover -html=cover.out -o cover.html

GOCOVERDIR=integration-test/gocov

.PHONY: e2e
e2e: bin/kink.cover
	./hack/inotify-check.sh
	rm -rf $(GOCOVERDIR)
	mkdir -p $(GOCOVERDIR)
	# Excessively long timeout is for github actions which are really slow
	set -o pipefail ; GOCOVERDIR=$(GOCOVERDIR) ginkgo run -vv --timeout=2h ./e2e/ 2>&1 | tee integration-test/log
	./hack/fix-coverage-permissions.sh
	go tool covdata percent -i=$(GOCOVERDIR)
	go tool covdata textfmt -i=$(GOCOVERDIR) -o cover.e2e.out
	go tool cover -html=cover.e2e.out -o cover.e2e.html

.PHONY: test
clean-tests:
	./hack/clean-tests.sh

ENVTEST = $(shell pwd)/bin/setup-envtest
.PHONY: envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)


# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef
