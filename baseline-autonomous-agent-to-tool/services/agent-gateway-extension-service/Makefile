# Agent Gateway Extension Service (ext_proc gRPC) — self-contained build/deploy.
# `make deploy` creates this service's own service account, stores the IdP
# client secret in Secret Manager, then deploys. No shared bootstrap required.

# Pull config from .env (copy from .env.sample) FIRST. GC_CLOUD_RUN_SERVICE_NAME
# and GC_REGION are REQUIRED from there — no defaults. Absent for `build`.
-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)

# Derived — expand after .env so GC_CLOUD_RUN_SERVICE_NAME/PROJECT_ID flow through.
IMAGE      := gcr.io/$(PROJECT_ID)/$(GC_CLOUD_RUN_SERVICE_NAME):latest
# This service's own workload identity. Fixed short id (SA account_id max 30
# chars), independent of the Cloud Run service name.
SA         := ext-svc@$(PROJECT_ID).iam.gserviceaccount.com
SECRET     := idp-client-secret

.PHONY: build push setup deploy require-env

## Fail fast unless the required deploy config is present in .env
require-env:
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(GC_REGION)"                 || { echo "GC_REGION not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_TOKEN_ENDPOINT)"        || { echo "IDP_TOKEN_ENDPOINT not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_CLIENT_ID)"             || { echo "IDP_CLIENT_ID not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_CLIENT_SECRET)"         || { echo "IDP_CLIENT_SECRET not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_AUDIENCE)"              || { echo "IDP_AUDIENCE not set — fill in .env (copy from .env.sample)"; exit 1; }

## Compile the binary locally
build:
	go build ./...

## Build and push the container image to GCR
push:
	docker build -t $(IMAGE) .
	docker push $(IMAGE)

## Ensure APIs, this service's SA, and the IdP secret exist (idempotent).
## The secret value comes from IDP_CLIENT_SECRET in .env.
setup: require-env
	gcloud services enable run.googleapis.com secretmanager.googleapis.com iam.googleapis.com
	@gcloud iam service-accounts describe $(SA) >/dev/null 2>&1 \
		|| gcloud iam service-accounts create ext-svc --display-name "ext-svc"
	@gcloud secrets describe $(SECRET) >/dev/null 2>&1 \
		|| gcloud secrets create $(SECRET) --replication-policy automatic
	@printf '%s' "$(IDP_CLIENT_SECRET)" | gcloud secrets versions add $(SECRET) --data-file=-
	gcloud secrets add-iam-policy-binding $(SECRET) \
		--member "serviceAccount:$(SA)" \
		--role roles/secretmanager.secretAccessor

## Deploy to Cloud Run. Non-secret config comes from .env; the IdP client secret
## is mounted from Secret Manager, never passed as a plain env var.
deploy: require-env setup push
	gcloud run deploy $(GC_CLOUD_RUN_SERVICE_NAME) \
		--image $(IMAGE) \
		--region $(GC_REGION) \
		--service-account $(SA) \
		--no-allow-unauthenticated \
		--port 50051 \
		--use-http2 \
		--set-env-vars "GRPC_PORT=50051,IDP_TOKEN_ENDPOINT=$(IDP_TOKEN_ENDPOINT),IDP_CLIENT_ID=$(IDP_CLIENT_ID),IDP_AUDIENCE=$(IDP_AUDIENCE)" \
		--set-secrets "IDP_CLIENT_SECRET=$(SECRET):latest"
