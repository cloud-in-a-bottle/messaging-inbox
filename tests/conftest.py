from collections.abc import Iterator

import pytest
from openhost_test_harness import OpenhostStack

# A real provider of the messaging service, deployed before the inbox so the inbox finds it when it starts. Instagram's
# app is the quickest to start: it doesn't log in until asked.
PROVIDER_REPO = "https://github.com/cloud-in-a-bottle/bottled-instagram"


@pytest.fixture(scope="session")
def provider_app_id() -> dict[str, str]:
    return {}


@pytest.fixture(scope="session")
def stack(provider_app_id: dict[str, str]) -> Iterator[OpenhostStack]:
    """Build the inbox's Dockerfile, run it under podman per openhost.toml beside a messaging app, and front both with
    the real OpenHost router. The inbox's manifest grants (read, write) are granted at install.

    - stack.url                   — through the router; requires owner auth
    - stack.owner_session         — a requests.Session authenticated as the zone owner
    - stack.playwright_login(page) — log a playwright page in as the owner for browser tests
    - stack.app_url               — direct to the container
    """

    def deploy_provider(s: OpenhostStack) -> None:
        provider_app_id["id"] = s.deploy_app(PROVIDER_REPO, timeout=900)

    with OpenhostStack(pre_deploy=deploy_provider) as s:
        yield s
