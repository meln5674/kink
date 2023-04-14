all: lint bin/kink test

.PHONY: lint test

GO_FILES := $(shell find cmd/ -name '*.go') $(shell find pkg/ -name '*.go') go.mod go.sum

bin/kink: $(GO_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o bin/kink main.go

bin/kink.dev: $(GO_FILES)
	go build -o bin/kink.dev main.go

lint:
	kind create cluster --name=helm-hog || true
	go vet ./cmd/... ./pkg/... ./e2e/...
	( cd helm/kink ; helm-hog test --batch --parallel=0 --kubectl-flags=--context=kind-helm-hog,-v4 --keep-reports -v11)


test:
	# Excessively long timeout is for github actions which are really slow
	ginkgo run -p -vv --timeout=2h ./e2e/ 2>&1 | tee integration-test/log

.PHONY: test
clean-tests:
	./hack/clean-tests.sh
