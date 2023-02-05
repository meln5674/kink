all: lint bin/kink test

.PHONY: lint test

GO_FILES := $(shell find cmd/ -name '*.go') $(shell find pkg/ -name '*.go') go.mod go.sum

bin/kink: $(GO_FILES)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o bin/kink main.go

lint:
	go vet ./cmd/... ./pkg/... ./e2e/...
	helm lint ./helm/kink/
	helm lint ./helm/kink/ \
		--set worker.replicaCount=0 
	helm lint ./helm/kink/ \
		--set rke2.enabled=true \
		--set controlplane.replicaCount=3
	helm lint ./helm/kink/ \
		--set loadBalancer.enabled=true
	helm lint ./helm/kink/ \
		--set loadBalancer.enabled=true \
		--set worker.replicaCount=0 
	helm lint ./helm/kink/ \
		--set loadBalancer.enabled=true \
		--set loadBalancer.ingress.enabled=true \
		--set loadBalancer.ingress.classMappings[0].guestClassName=guestClassName1 \
		--set loadBalancer.ingress.classMappings[0].className=className \
		--set loadBalancer.ingress.classMappings[0].nodePort.namespace=ns \
		--set loadBalancer.ingress.classMappings[0].nodePort.name=name \
		--set loadBalancer.ingress.classMappings[0].nodePort.nodePort.httpPort=http \
		--set loadBalancer.ingress.classMappings[1].guestClassName=guestClassName2 \
		--set loadBalancer.ingress.classMappings[1].className=className \
		--set loadBalancer.ingress.classMappings[1].nodePort.namespace=ns \
		--set loadBalancer.ingress.classMappings[1].nodePort.name=name \
		--set loadBalancer.ingress.classMappings[1].nodePort.nodePort.httpsPort=https \
		--set loadBalancer.ingress.classMappings[2].guestClassName=guestClassName3 \
		--set loadBalancer.ingress.classMappings[2].className=className \
		--set loadBalancer.ingress.classMappings[2].nodePort.namespace=ns \
		--set loadBalancer.ingress.classMappings[2].nodePort.name=name \
		--set loadBalancer.ingress.classMappings[2].nodePort.hostPort.httpPort=80 \
		--set loadBalancer.ingress.classMappings[3].guestClassName=guestClassName4 \
		--set loadBalancer.ingress.classMappings[3].className=className \
		--set loadBalancer.ingress.classMappings[3].nodePort.namespace=ns \
		--set loadBalancer.ingress.classMappings[3].nodePort.name=name \
		--set loadBalancer.ingress.classMappings[3].nodePort.hostPort.httpsPort=443
	helm lint ./helm/kink/ \
		--set controlplane.ingress.enabled=true \
		--set controlplane.ingress.hosts[0]=host
	helm lint ./helm/kink/ \
		--set controlplane.service.type=NodePort \
		--set controlplane.nodePortHost=host

test:
	# Excessively long timeout is for github actions which are really slow
	ginkgo run -vv --timeout=2h ./e2e/ 2>&1 | tee integration-test/log
