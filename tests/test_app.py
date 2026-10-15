import time

import httpx
from openhost_test_harness import OpenhostStack
from playwright.sync_api import Page
from playwright.sync_api import expect

MESSAGING = "github.com/cloud-in-a-bottle/messaging-service-spec"


def wait_for_live_provider(stack: OpenhostStack) -> dict:
    deadline = time.time() + 60
    while True:
        providers = stack.owner_session.get(f"{stack.url}/api/state").json()["providers"]
        if providers and providers[0]["live"] and providers[0]["account"]:
            return providers[0]
        if time.time() > deadline:
            listing = stack.owner_session.get(
                f"{stack.router_url}/api/services/v2/providers", params={"service": MESSAGING}
            ).json()
            raise AssertionError(
                f"provider never came up: {providers}\nrouter's providers: {listing}\n"
                f"inbox logs:\n{stack.app_logs()[-3000:]}"
            )
        time.sleep(1)


def test_health_endpoint(stack: OpenhostStack) -> None:
    response = httpx.get(f"{stack.app_url}/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_follows_the_provider_through_the_router(stack: OpenhostStack, provider_app_id: dict[str, str]) -> None:
    provider = wait_for_live_provider(stack)
    assert provider["app_id"] == provider_app_id["id"]
    assert provider["account"]["network"] == "instagram"
    assert provider["account"]["state"] == "logged_out"
    assert provider["ui_url"].startswith("http://bottled-instagram.harness.")
    assert stack.owner_session.get(f"{stack.url}/api/chats").json() == []


def test_proxies_to_the_provider(stack: OpenhostStack, provider_app_id: dict[str, str]) -> None:
    app_id = provider_app_id["id"]
    response = stack.owner_session.get(f"{stack.url}/api/p/{app_id}/v1/chats")
    assert response.status_code == 200
    assert response.json() == {"chats": [], "next_cursor": None}
    response = stack.owner_session.post(f"{stack.url}/api/p/{app_id}/v1/chats", json={"address": "instagram:1"})
    assert response.status_code == 503
    assert response.json()["error"] == "not_connected"
    assert stack.owner_session.get(f"{stack.url}/api/p/{app_id}/v1/events").status_code == 404
    assert stack.owner_session.get(f"{stack.url}/api/p/nobody/v1/chats").status_code == 404


def test_page_shows_the_provider(stack: OpenhostStack, page: Page) -> None:
    wait_for_live_provider(stack)
    stack.playwright_login(page)
    page.goto(stack.url)
    expect(page.locator("#providers")).to_contain_text("Instagram")
    expect(page.locator("#providers")).to_contain_text("not logged in")
    page.get_by_role("button", name="New chat").click()
    expect(page.get_by_text("None of your messaging apps is connected.")).to_be_visible()
