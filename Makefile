all: lint bin/kink test

.PHONY: lint test

GO_FILES := $(shell find cmd/ -name '*.go') $(shell find pkg/ -name '*.go') go.mod go.sum

bin/kink: $(GO_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o bin/kink main.go

bin/kink.dev: $(GO_FILES)
	go build -o bin/kink.dev main.go

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
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" ginkgo run -v -r --coverpkg=./pkg/... ./pkg

.PHONY: e2e
e2e:
	if [ "$$(cat /proc/sys/fs/inotify/max_user_instances)" -lt 512 ]; then \
		echo "/proc/sys/fs/inotify/max_user_instances is set to $$(cat /proc/sys/fs/inotify/max_user_instances), please set to at least 512, otherwise, tests will fail" ; \
		exit 1 ; \
	fi
	# Excessively long timeout is for github actions which are really slow
	ginkgo run -p -vv --timeout=2h ./e2e/ 2>&1 | tee integration-test/log

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
