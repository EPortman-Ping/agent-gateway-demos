BAATT := baseline-autonomous-agent-to-tool/services/agent
AOBOU := agent-on-behalf-of-user/services/agent

# Google enforces a project-level limit of one gateway-bound Reasoning Engine
# per direction. Both demos use Agent-to-Anywhere egress on the same project,
# so only one can be active at a time. These targets delete the active agent
# and deploy the other. Expect ~2-5 minutes for Agent Runtime to come up and
# another ~3 minutes for iap.egressor propagation before the new agent is live.

.PHONY: switch-to-baatt switch-to-aobou

switch-to-baatt:
	@echo "==> Tearing down aobou agent..."
	$(MAKE) -C $(AOBOU) teardown
	@echo "==> Deploying baatt agent..."
	$(MAKE) -C $(BAATT) deploy

switch-to-aobou:
	@echo "==> Tearing down baatt agent..."
	$(MAKE) -C $(BAATT) teardown
	@echo "==> Deploying aobou agent..."
	$(MAKE) -C $(AOBOU) deploy
