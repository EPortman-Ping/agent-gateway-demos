-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)

IMAGE := gcr.io/$(PROJECT_ID)/$(GC_CLOUD_RUN_SERVICE_NAME):latest
SA    := ext-svc@$(PROJECT_ID).iam.gserviceaccount.com

.PHONY: build push setup deploy require-env

require-env:
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set"; exit 1; }
	@test -n "$(GC_REGION)"                 || { echo "GC_REGION not set"; exit 1; }
	@test -n "$(IDP_TOKEN_ENDPOINT)"        || { echo "IDP_TOKEN_ENDPOINT not set"; exit 1; }
	@test -n "$(IDP_REQUIRED_AUDIENCE)"     || { echo "IDP_REQUIRED_AUDIENCE not set"; exit 1; }
	@test -n "$(IDP_SCOPE)"                 || { echo "IDP_SCOPE not set"; exit 1; }
	@test -n "$(TOOL_URL)"                  || { echo "TOOL_URL not set"; exit 1; }

build:
	go build ./...

push:
	docker build -t $(IMAGE) .
	docker push $(IMAGE)

setup: require-env
	gcloud services enable run.googleapis.com secretmanager.googleapis.com iam.googleapis.com
	@gcloud iam service-accounts describe $(SA) >/dev/null 2>&1 \
		|| gcloud iam service-accounts create ext-svc --display-name "ext-svc"

deploy: require-env setup push
	gcloud run deploy $(GC_CLOUD_RUN_SERVICE_NAME) \
		--image $(IMAGE) \
		--region $(GC_REGION) \
		--service-account $(SA) \
		--allow-unauthenticated \
		--ingress=all \
		--port 50051 \
		--use-http2 \
		--set-env-vars "GRPC_PORT=50051,IDP_TOKEN_ENDPOINT=$(IDP_TOKEN_ENDPOINT),IDP_REQUIRED_AUDIENCE=$(IDP_REQUIRED_AUDIENCE),IDP_SCOPE=$(IDP_SCOPE),TOOL_URL=$(TOOL_URL)"
