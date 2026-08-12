-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
IMAGE      := gcr.io/$(PROJECT_ID)/$(GC_CLOUD_RUN_SERVICE_NAME):latest
SA         := hr-mcp-svc@$(PROJECT_ID).iam.gserviceaccount.com
MGMT_SECRET := pingone-mgmt-client-secret

.PHONY: build push setup deploy require-env

require-env:
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)"  || { echo "GC_CLOUD_RUN_SERVICE_NAME not set"; exit 1; }
	@test -n "$(GC_REGION)"                  || { echo "GC_REGION not set"; exit 1; }
	@test -n "$(IDP_ISSUER)"                 || { echo "IDP_ISSUER not set"; exit 1; }
	@test -n "$(IDP_REQUIRED_SCOPE)"         || { echo "IDP_REQUIRED_SCOPE not set"; exit 1; }
	@test -n "$(IDP_REQUIRED_AUDIENCE)"      || { echo "IDP_REQUIRED_AUDIENCE not set"; exit 1; }
	@test -n "$(PINGONE_MGMT_CLIENT_ID)"     || { echo "PINGONE_MGMT_CLIENT_ID not set"; exit 1; }
	@test -n "$(PINGONE_MGMT_CLIENT_SECRET)" || { echo "PINGONE_MGMT_CLIENT_SECRET not set"; exit 1; }

build:
	go build ./...

push:
	docker build -t $(IMAGE) .
	docker push $(IMAGE)

setup: require-env
	gcloud services enable run.googleapis.com secretmanager.googleapis.com iam.googleapis.com
	@gcloud iam service-accounts describe $(SA) >/dev/null 2>&1 \
		|| gcloud iam service-accounts create hr-mcp-svc --display-name "hr-mcp-svc"
	@gcloud secrets describe $(MGMT_SECRET) >/dev/null 2>&1 \
		|| gcloud secrets create $(MGMT_SECRET) --replication-policy automatic
	@printf '%s' "$(PINGONE_MGMT_CLIENT_SECRET)" | gcloud secrets versions add $(MGMT_SECRET) --data-file=-
	gcloud secrets add-iam-policy-binding $(MGMT_SECRET) \
		--member "serviceAccount:$(SA)" \
		--role roles/secretmanager.secretAccessor

deploy: require-env setup push
	gcloud run deploy $(GC_CLOUD_RUN_SERVICE_NAME) \
		--image $(IMAGE) \
		--region $(GC_REGION) \
		--service-account $(SA) \
		--allow-unauthenticated \
		--set-env-vars "IDP_ISSUER=$(IDP_ISSUER),IDP_REQUIRED_SCOPE=$(IDP_REQUIRED_SCOPE),IDP_REQUIRED_AUDIENCE=$(IDP_REQUIRED_AUDIENCE),PINGONE_MGMT_CLIENT_ID=$(PINGONE_MGMT_CLIENT_ID)" \
		--set-secrets "PINGONE_MGMT_CLIENT_SECRET=$(MGMT_SECRET):latest"
