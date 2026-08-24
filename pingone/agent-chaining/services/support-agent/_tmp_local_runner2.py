import asyncio
import os
os.environ.setdefault("GOOGLE_GENAI_USE_VERTEXAI", "1")
os.environ.setdefault("GOOGLE_CLOUD_PROJECT", "project-cc769feb-5628-4231-883")
os.environ.setdefault("GOOGLE_CLOUD_LOCATION", "us-central1")

from dotenv import load_dotenv
load_dotenv()

from google.adk.runners import InMemoryRunner
from google.genai import types

from agent import root_agent


async def run_once(i):
    runner = InMemoryRunner(agent=root_agent, app_name="debug")
    session = await runner.session_service.create_session(
        app_name="debug", user_id=f"debug-user-{i}", state={"user_token": "fake-token-for-local-test"}
    )
    content = types.Content(role="user", parts=[types.Part(text="What is the status of order ORD-123?")])
    count = 0
    try:
        async for event in runner.run_async(user_id=f"debug-user-{i}", session_id=session.id, new_message=content):
            count += 1
            print(f"[run {i}] EVENT {count}", event.content, event.error_code, event.error_message)
    except Exception as exc:
        print(f"[run {i}] EXCEPTION {type(exc).__name__}: {exc}")
    print(f"[run {i}] TOTAL EVENTS {count}")


async def main():
    for i in range(5):
        await run_once(i)

asyncio.run(main())
