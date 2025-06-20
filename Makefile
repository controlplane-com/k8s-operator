VERSION ?= 0.4.0
IMG ?= us-docker.pkg.dev/controlplane/internal/cpln-operator:v${VERSION}
PLATFORM ?= linux/arm64,linux/amd64
.PHONY: generate-rbac
generate-rbac:
	@echo "==> Generating RBAC from CRD files..."
	go run scripts/generateRbac.go

.PHONY: generate-argo-config
generate-argo-config:
	@echo "==> Generating ArcoCD config from CRD files..."
	go run scripts/generateArgoConfig.go

.PHONY: generate
generate: generate-rbac generate-argo-config

.PHONY: deploy-hack-version
deploy-hack-version:
	@if [ ! -f hack-version.txt ]; then echo "0" > hack-version.txt; fi; \
	HACK_VERSION=$$(expr $$(cat hack-version.txt) + 1); \
	echo $$HACK_VERSION > hack-version.txt; \
	HACK_IMAGE=${IMG}-hack-$$HACK_VERSION; \
	IMG=$$HACK_IMAGE make push-image; \
	helm upgrade cpln-operator --set image=$${HACK_IMAGE} ./chart; \

.PHONY: install
install: generate
	@echo "==> Applying manifests..."
	helm install --set image=${IMG} cpln-operator ./chart

.PHONY: upgrade
upgrade: generate
	helm upgrade cpln-operator --set image=${IMG} ./chart

.PHONY: build-image
build-image:
	docker buildx build \
    	--platform="${PLATFORM}" \
    	 -t ${IMG} .

.PHONY: push-image
push-image:
	docker buildx build \
		--push \
    	--platform="${PLATFORM}" \
    	 -t ${IMG} .


.PHONY: install-secret
install-secret:
	@if [ -z "$(org)" ] || [ -z "$(key)" ]; then \
		echo "Error: Required parameters missing"; \
		echo "Usage: make install-secret org=<org-name> key=<org-key>"; \
		exit 1; \
	fi
	bash scripts/install-secret.sh "$(org)" "$(key)"

.PHONY: cluster-quickstart
cluster-quickstart:
	bash scripts/cluster-quickstart.sh;

#Before making this recipe, update Chart.yaml with the new version and, if necessary, change the operator image referenced in values.yaml to the correct tag.
#Typically this is run this only in the gh-pages branch
.PHONY: publish-chart
publish-chart:
	helm package chart/ --destination published-charts
	helm repo index published-charts \
      --merge index.yaml \
      --url https://controlplane-com.github.io/k8s-operator/published-charts
	mv published-charts/index.yaml index.yaml
