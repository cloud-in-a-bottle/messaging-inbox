# messaging-inbox

All your chats in one place, for [Cloud in a Bottle](https://github.com/cloud-in-a-bottle/cloud-in-a-bottle). The
inbox reads and sends through every installed app that provides the
[messaging service](https://github.com/cloud-in-a-bottle/messaging-service-spec): bottled-whatsapp,
bottled-fb-messenger, bottled-instagram, and later Signal and RCS. It stores nothing itself. Each messaging app stays
the source of truth for its own chats and does its own logging in.

- **One list** of every network's chats, by latest activity, each tagged with its network. Chats are never merged
  across networks, even with the same person.
- **Reading and sending** in any chat: replies, reactions, edits, unsends, attachments, read receipts and typing. Each
  is offered only where that chat's network allows it, from the capabilities the service reports.
- **Search** across every network at once.
- **New chats** on any connected network, by name or (where the network allows) phone number.
- **Live**: changes arrive over each provider's event stream.

## How it works

- `internal/hub` finds the providers through the router (`GET /api/services/v2/providers`) and follows each one's
  event stream. It caches their accounts and chat lists, refetching a chat when its provider says it changed, so the
  merged list is served without a round trip to every app.
- `internal/server` serves the UI and a small API:
  - `/api/state` returns the providers, `/api/chats` the merged list, and `/api/search` fans out to every provider.
  - `/api/events` is the hub's change stream.
  - `/api/p/<app id>/v1/...` passes everything else (messages, files, sends, reactions, ...) through to that
    provider, as this app. The browser never holds the app's token.
- `internal/static` is the UI. It builds on the messaging apps' own UI: `ui.js` (elements, avatars, attachments,
  reactions, popovers) and `app.css` come from bottled-bridge-core's `web/static` package, served under
  `/static/core/`.

The app needs the service's `read` and `write` grants (declared in `openhost.toml`, approved at install).

## Development

```bash
just setup     # go deps, pre-commit hooks, playwright browser
just test-go   # unit tests
just test      # plus the container tests: the inbox beside a real bottled-instagram, behind the real router
```

The container tests deploy bottled-instagram from GitHub, so they need access to that repo.

## License

AGPL-3.0, as the UI builds on bottled-bridge-core's.
