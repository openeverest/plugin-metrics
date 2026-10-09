IMG ?= ghcr.io/openeverest/plugin-metrics:dev

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

##@ Build

.PHONY: build-frontend
build-frontend: ## Build the frontend bundle into dist/main.js.
	npm run build

.PHONY: docker-build
docker-build: build-frontend ## Build the plugin image (IMG) for linux/amd64.
	docker buildx build --platform linux/amd64 -t $(IMG) .

.PHONY: test
test: ## Run backend and frontend tests.
	cd backend && go test ./...
	npm test
