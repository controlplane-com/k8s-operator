IMG = gcr.io/cpln-build/cpln-operator:v0.0.1-kc-4

install-crds:
	@echo "==> Applying CRDs..."
	kubectl apply -f chart/crd

generate-rbac:
	@echo "==> Generating RBAC from CRD files..."
	go run scripts/generateRbac.go

install: generate-rbac install-crds
	@echo "==> Applying manifests..."
	helm install --set image=${IMG} cpln-operator ./chart

upgrade: generate-rbac install-crds
	helm upgrade cpln-operator --set image=${IMG} ./chart

build-image:
	#docker buildx build \
    #	--platform="linux/arm64,linux/amd64" \
    #	 -t ${IMG} .
	docker buildx build \
    	--platform="linux/amd64" \
    	 -t ${IMG} .

push-image:
	docker buildx build \
		--push \
    	--platform="linux/amd64" \
    	 -t ${IMG} .