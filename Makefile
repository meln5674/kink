all: lint bin/kink test

.PHONY: lint test

GO_FILES := $(shell find cmd/ -name '*.go') $(shell find pkg/ -name '*.go') go.mod go.sum

bin/kink: $(GO_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o bin/kink main.go

lint:
	go vet ./cmd/... ./pkg/... ./e2e/...
	helm lint ./helm/kink/
	helm lint ./helm/kink/ --set rke2.enabled=true --set controlplane.replicaCount=3
	helm lint ./helm/kink/ --set loadBalancer.enabled=true
	helm lint ./helm/kink/ --set controlplane.ingress.enabled=true --set controlplane.ingress.hosts[0]=foo

test:
	# Excessively long timeout is for github actions which are really slow
	ginkgo run -vv --timeout=2h ./e2e/ 2>&1 | tee integration-test/log
