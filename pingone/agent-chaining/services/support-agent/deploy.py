"""Deploy the Support Agent to Agent Runtime."""

import os

from dotenv import load_dotenv
import agentplatform
from agentplatform import agent_engines, types

load_dotenv()
from agent import root_agent  # noqa: E402

PROJECT_ID = os.environ["GC_PROJECT_ID"]
REGION = os.environ["GC_REGION"]
AGENT_GATEWAY = os.environ["GC_AGENT_GATEWAY"]
AGENT_DISPLAY_NAME = os.environ["AGENT_DISPLAY_NAME"]

client = agentplatform.Client(project=PROJECT_ID, location=REGION)
app = agent_engines.AdkApp(agent=root_agent)

config = {
    "requirements": [
        "google-cloud-aiplatform[agent_engines,adk]>=1.126.1",
        "google-adk>=1.18.0",
        "httpx",
        "python-dotenv",
        "cloudpickle",
    ],
    "extra_packages": ["pingone.py", "../agent_chain_common/protocol.py"],
    "display_name": AGENT_DISPLAY_NAME,
    "identity_type": types.IdentityType.AGENT_IDENTITY,
    "agent_gateway_config": {"agent_to_anywhere_config": {"agent_gateway": AGENT_GATEWAY}},
    "env_vars": {
        key: value
        for key, value in os.environ.items()
        if key.startswith(("GC_", "A2A_", "SUPPORT_AGENT_", "AGENT_IDP_", "LOCAL_"))
    },
}

remote_agent = client.agent_engines.create(agent=app, config=config)
print("Deployed Support Agent:", remote_agent.api_resource.name)
