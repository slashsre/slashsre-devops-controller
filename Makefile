IMG ?= ghcr.io/slashsre/slashsre-devops-controller:latest
REVISION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
VERSION ?= $(shell sed -n 's/^var VERSION = "\(.*\)"/\1/p' internal/version/version.go)

LOCALBIN ?= $(shell pwd)/bin
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
CONTROLLER_TOOLS_VERSION ?= v0.18.0
SWAG_VERSION ?= v1.16.4

LDFLAGS := -s -w -X github.com/slashsre/slashsre-devops-controller/internal/version.REVISION=$(REVISION)

.PHONY: help
help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

##@ Development

.PHONY: generate
generate: controller-gen ## Generate DeepCopy methods
	$(CONTROLLER_GEN) object paths="./api/..."

.PHONY: manifests
manifests: controller-gen ## Generate CRDs and RBAC
	$(CONTROLLER_GEN) rbac:roleName=slashsre-devops-controller crd paths="./..." \
		output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac

.PHONY: swagger
swagger: ## Regenerate Swagger docs
	GOTOOLCHAIN=go1.25.4 go run github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION) init -g server.go -d internal/server,internal/remediation,internal/posttest -o internal/server/docs

.PHONY: fmt
fmt: ## go fmt
	go fmt ./...

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: test
test: generate manifests fmt vet ## Run unit tests
	go test ./... -coverprofile cover.out

.PHONY: verify
verify: generate manifests ## Fail if generated files are out of date
	git diff --exit-code -- api config

##@ Build

.PHONY: ui
ui: ## Build the Svelte UI and copy it into the Go embed directory
	./build-ui.sh

.PHONY: build
build: ## Build the manager binary
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/manager ./cmd

.PHONY: run
run: ## Run against the current kubeconfig context
	go run ./cmd --level=debug

.PHONY: docker-build
docker-build: ## Build the container image
	docker build --build-arg REVISION=$(REVISION) --build-arg VERSION=$(VERSION) -t $(IMG) .

##@ Deployment

.PHONY: install
install: manifests ## Install CRDs into the current cluster
	kubectl apply --server-side -k config/crd

.PHONY: uninstall
uninstall: ## Remove CRDs (and every custom resource!) from the current cluster
	kubectl delete -k config/crd

.PHONY: deploy
deploy: manifests ## Deploy the controller to the current cluster
	cd config/manager && kubectl kustomize edit set image controller=$(IMG) 2>/dev/null || true
	kubectl apply --server-side -k config/default

.PHONY: undeploy
undeploy: ## Remove the controller from the current cluster
	kubectl delete -k config/default

##@ Tools

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN)
$(CONTROLLER_GEN):
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)
