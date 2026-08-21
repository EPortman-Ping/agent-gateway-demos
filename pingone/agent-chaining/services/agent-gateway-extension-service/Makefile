-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
IMAGE := gcr.io/$(PROJECT_ID)/$(GC_CLOUD_RUN_SERVICE_NAME):latest
SA := agent-chain-ext-svc@$(PROJECT_ID).iam.gserviceaccount.com
SECRET := agent-chain-idp-client-secret
AUTHZ_SECRET := agent-chain-authz-client-secret

.PHONY: build push setup deploy require-env

require-env:
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set"; exit 1; }
	@test -n "$(GC_REGION)" || { echo "GC_REGION not set"; exit 1; }
	@test -n "$(IDP_TOKEN_ENDPOINT)" || { echo "IDP_TOKEN_ENDPOINT not set"; exit 1; }
	@test -n "$(IDP_CLIENT_ID)" || { echo "IDP_CLIENT_ID not set"; exit 1; }
	@test -n "$(IDP_CLIENT_SECRET)" || { echo "IDP_CLIENT_SECRET not set"; exit 1; }
	@test -n "$(A2A_TARGET_URL)" || { echo "A2A_TARGET_URL not set"; exit 1; }
	@test -n "$(MCP_TARGET_URL)" || { echo "MCP_TARGET_URL not set"; exit 1; }
	@test "$(AUTHZ_MODE)" = "permit-all" || { test -n "$(AUTHZ_DECISION_ENDPOINT)" && test -n "$(AUTHZ_CLIENT_ID)" && test -n "$(AUTHZ_CLIENT_SECRET)" || { echo "Set AUTHZ_MODE=permit-all or configure PingOne Authorize"; exit 1; }; }

build:
	go build ./...

push:
	docker build -t $(IMAGE) .
	docker push $(IMAGE)

setup: require-env
	gcloud services enable run.googleapis.com secretmanager.googleapis.com iam.googleapis.com
	@gcloud iam service-accounts describe $(SA) >/dev/null 2>&1 || gcloud iam service-accounts create agent-chain-ext-svc --display-name "Agent Chaining Extension Service"
	@gcloud secrets describe $(SECRET) >/dev/null 2>&1 || gcloud secrets create $(SECRET) --replication-policy automatic
	@printf '%s' "$(IDP_CLIENT_SECRET)" | gcloud secrets versions add $(SECRET) --data-file=-
	gcloud secrets add-iam-policy-binding $(SECRET) --member "serviceAccount:$(SA)" --role roles/secretmanager.secretAccessor
	@gcloud secrets describe $(AUTHZ_SECRET) >/dev/null 2>&1 || gcloud secrets create $(AUTHZ_SECRET) --replication-policy automatic
	@printf '%s' "$(AUTHZ_CLIENT_SECRET)" | gcloud secrets versions add $(AUTHZ_SECRET) --data-file=-
	gcloud secrets add-iam-policy-binding $(AUTHZ_SECRET) --member "serviceAccount:$(SA)" --role roles/secretmanager.secretAccessor

deploy: require-env setup push
	gcloud run deploy $(GC_CLOUD_RUN_SERVICE_NAME) \
		--image $(IMAGE) --region $(GC_REGION) --service-account $(SA) \
		--allow-unauthenticated --ingress=all --port 50051 --use-http2 \
		--set-env-vars "GRPC_PORT=50051,AUTHZ_MODE=$(AUTHZ_MODE),IDP_TOKEN_ENDPOINT=$(IDP_TOKEN_ENDPOINT),IDP_CLIENT_ID=$(IDP_CLIENT_ID),A2A_TARGET_URL=$(A2A_TARGET_URL),A2A_REQUIRED_AUDIENCE=$(A2A_REQUIRED_AUDIENCE),A2A_REQUIRED_SCOPE=$(A2A_REQUIRED_SCOPE),MCP_TARGET_URL=$(MCP_TARGET_URL),MCP_REQUIRED_AUDIENCE=$(MCP_REQUIRED_AUDIENCE),MCP_REQUIRED_SCOPE=$(MCP_REQUIRED_SCOPE),AUTHZ_DECISION_ENDPOINT=$(AUTHZ_DECISION_ENDPOINT),AUTHZ_CLIENT_ID=$(AUTHZ_CLIENT_ID)" \
		--set-secrets "IDP_CLIENT_SECRET=$(SECRET):latest,AUTHZ_CLIENT_SECRET=$(AUTHZ_SECRET):latest"
