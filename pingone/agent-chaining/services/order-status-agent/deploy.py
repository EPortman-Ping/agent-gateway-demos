"""Deploy the Order Status Agent to Agent Runtime."""

import os
from dotenv import load_dotenv
import agentplatform
from agentplatform import agent_engines, types

load_dotenv()
from agent import root_agent  # noqa: E402

project = os.environ["GC_PROJECT_ID"]
region = os.environ["GC_REGION"]
client = agentplatform.Client(project=project, location=region)
app = agent_engines.AdkApp(agent=root_agent)
config = {
    "requirements": ["google-cloud-aiplatform[agent_engines,adk]>=1.126.1", "google-adk>=1.18.0", "httpx", "python-dotenv", "cloudpickle"],
    "extra_packages": ["pingone.py", "../agent_chain_common/protocol.py"],
    "display_name": os.environ["AGENT_DISPLAY_NAME"],
    "identity_type": types.IdentityType.AGENT_IDENTITY,
    "agent_gateway_config": {"agent_to_anywhere_config": {"agent_gateway": os.environ["GC_AGENT_GATEWAY"]}},
    "env_vars": {key: value for key, value in os.environ.items() if key.startswith(("GC_", "A2A_", "MCP_", "AGENT_", "LOCAL_"))},
}
remote_agent = client.agent_engines.create(agent=app, config=config)
print("Deployed Order Status Agent:", remote_agent.api_resource.name)
