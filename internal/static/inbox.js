import {
  attachmentNode, avatar, closePopover, el, formatDay, formatDuration, formatTime, linkify, menu,
  missingAttachmentNode, openPopover, reactionPicker, reactionsNode, tapToShowTools, template, toast, toolButton,
} from "/static/core/ui.js";

const app = document.getElementById("app");
const PAGE_SIZE = 50;
const DEFAULT_REACTIONS = ["❤️", "😆", "😮", "😢", "😠", "👍"];
// Each network's color, for its badge and your own messages in its chats.
const NETWORK_COLORS = {
  whatsapp: "#1fa855", messenger: "#0a7cff", instagram: "#d6249f", signal: "#3a76f0", rcs: "#1a73e8",
};

const state = {
  // By app ID: { app_id, app_name, account, error, live, ui_url }.
  providers: new Map(),
  chats: [],
  // The open chat: { appId, chatId }.
  active: null,
  detail: null,
  caps: {},
  messages: [],
  hasMoreBefore: false,
  beforeCursor: null,
  // Asked the network for older messages; load them when the chat next changes.
  awaitingOlder: false,
  replyTo: null,
  editing: null,
  pendingFiles: [],
  highlight: null,
  // "<app ID>/<chat ID>" -> timer, for chats where someone is typing.
  typing: new Map(),
};

// ---------------------------------------------------------------- helpers

async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body instanceof FormData) {
    opts.body = body;
  } else if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  if (!res.ok) {
    let detail = `${res.status} ${res.statusText}`;
    try {
      const data = await res.json();
      detail = data.detail || data.error || detail;
    } catch {}
    throw new Error(detail);
  }
  if (res.status === 204) return null;
  const text = await res.text();
  return text ? JSON.parse(text) : null;
}

// A call to one provider's messaging service, through the inbox's server.
function papi(appId, method, rest, body) {
  return api(method, `/api/p/${encodeURIComponent(appId)}/${rest}`, body);
}

// Files (attachments, avatars) are given as URLs relative to the provider's service.
function fileURL(appId, rel) {
  return rel ? `/api/p/${encodeURIComponent(appId)}/${rel}` : null;
}

function provider(appId) {
  return state.providers.get(appId);
}

function network(appId) {
  const p = provider(appId);
  return p && p.account ? p.account.network : "";
}

function networkName(appId) {
  const p = provider(appId);
  return (p && p.account && p.account.network_name) || (p ? p.app_name : "?");
}

function networkBadge(appId) {
  return el("span", { class: "net", style: `--net:${NETWORK_COLORS[network(appId)] || "var(--fg-muted)"}` },
    networkName(appId));
}

function chatKey(appId, chatId) {
  return `${appId}/${chatId}`;
}

function isActive(appId, chatId) {
  return state.active && state.active.appId === appId && state.active.chatId === chatId;
}

function chatHash(appId, chatId, messageId) {
  const base = `#/c/${encodeURIComponent(appId)}/${encodeURIComponent(chatId)}`;
  return messageId ? `${base}/m/${encodeURIComponent(messageId)}` : base;
}

function isMuted(c) {
  return c.muted_until === "forever" || (c.muted_until && new Date(c.muted_until) > new Date());
}

function personName(p) {
  return p.is_self ? "You" : (p.name || "Someone");
}

// ---------------------------------------------------------------- providers and chats

async function refreshState() {
  const s = await api("GET", "/api/state");
  state.providers = new Map(s.providers.map((p) => [p.app_id, p]));
  renderProviders();
  renderChatList();
}

const STATE_TEXT = {
  connected: "", connecting: "connecting…", logged_out: "not logged in", error: "error",
};

function renderProviders() {
  const box = app.querySelector("#providers");
  const providers = [...state.providers.values()];
  if (!providers.length) {
    box.replaceChildren(el("p", { class: "hint" },
      "No messaging apps yet. Install one (eg bottled-whatsapp or bottled-fb-messenger) and its chats show up here. ",
      el("button", { type: "button", class: "link", onclick: () => api("POST", "/api/rediscover").then(refreshState)
        .catch((e) => toast(e.message)) }, "Look again")));
    return;
  }
  box.replaceChildren(...providers.map((p) => {
    const st = p.account ? p.account.state : "error";
    const problem = p.error || (st !== "connected" ? STATE_TEXT[st] : "");
    return el("a", {
      class: `provider${problem ? " bad" : ""}`, href: p.ui_url, target: "_blank", rel: "noopener",
      title: p.error || (p.account && p.account.state_detail) || `Open ${p.app_name}`,
    }, el("span", { class: "dot", style: `background:${NETWORK_COLORS[network(p.app_id)] || "var(--fg-muted)"}` }),
    networkName(p.app_id), problem ? el("small", {}, ` · ${problem}`) : null);
  }));
}

let chatsTimer;
function refreshChatsSoon() {
  clearTimeout(chatsTimer);
  chatsTimer = setTimeout(() => refreshChats().catch((e) => console.error(e)), 150);
}

async function refreshChats() {
  state.chats = await api("GET", "/api/chats");
  renderChatList();
}

function renderChatList() {
  const list = app.querySelector("#conversations");
  list.replaceChildren(...state.chats.map((c) => {
    const typing = state.typing.has(chatKey(c.app_id, c.id));
    return el("li", {
      class: `${isActive(c.app_id, c.id) ? "active" : ""}${isMuted(c) ? " muted" : ""}`,
      onclick: () => { location.hash = chatHash(c.app_id, c.id); },
    },
    avatar(c.title, `${c.app_id}:${c.id}`, fileURL(c.app_id, c.avatar_url)),
    el("span", { class: "name" }, c.pinned ? "📌 " : "", c.title, " ", networkBadge(c.app_id)),
    el("span", { class: "time" }, c.last_message_at ? formatTime(c.last_message_at) : ""),
    el("span", { class: "preview" }, typing ? "typing…" : (c.last_message_preview || "")),
    c.unread_count ? el("span", { class: "unread" }, c.unread_count > 99 ? "99+" : c.unread_count)
      : (c.marked_unread ? el("span", { class: "unread" }, "•") : el("span")));
  }));
  if (!state.chats.length && state.providers.size) {
    list.append(el("li", { class: "hint" }, "No chats yet. They appear here as your messaging apps sync."));
  }
}

// ---------------------------------------------------------------- search

async function runSearch(q) {
  const results = app.querySelector("#search-results");
  const convs = app.querySelector("#conversations");
  if (!q) {
    results.hidden = true;
    convs.hidden = false;
    return;
  }
  const res = await api("GET", `/api/search?q=${encodeURIComponent(q)}`);
  convs.hidden = true;
  results.hidden = false;
  results.replaceChildren(...res.hits.map((h) => el("li",
    { onclick: () => { location.hash = chatHash(h.app_id, h.message.chat_id, h.message.id); } },
    avatar(h.chat_title, `${h.app_id}:${h.message.chat_id}`),
    el("span", { class: "name" }, h.chat_title, " ", networkBadge(h.app_id)),
    el("span", { class: "time" }, formatTime(h.message.sent_at)),
    el("span", { class: "preview" }, `${personName(h.message.sender)}: `, ...highlightSnippet(h.snippet)),
  )));
  if (!res.hits.length) results.append(el("li", { class: "hint" }, "No matches."));
  for (const [appId, why] of Object.entries(res.failed)) {
    results.append(el("li", { class: "hint" }, `${networkName(appId)} couldn't search: ${why}`));
  }
}

function highlightSnippet(snippet) {
  return snippet.split(/(\[[^\]]*\])/).map((part) =>
    part.startsWith("[") && part.endsWith("]") ? el("mark", {}, part.slice(1, -1)) : part);
}

// ---------------------------------------------------------------- a chat

function routeFromHash() {
  const m = /^#\/c\/([^/]+)\/([^/]+)(?:\/m\/([^/]+))?$/.exec(location.hash);
  if (!m) return closeChat();
  openChat(decodeURIComponent(m[1]), decodeURIComponent(m[2]), m[3] ? decodeURIComponent(m[3]) : null)
    .catch((e) => toast(e.message));
}

function closeChat() {
  state.active = null;
  app.querySelector(".shell").classList.remove("in-thread");
  app.querySelector("#thread").replaceChildren(el("div", { class: "empty" }, "Select a conversation"));
  renderChatList();
}

async function openChat(appId, chatId, highlightId) {
  const switching = !isActive(appId, chatId);
  state.active = { appId, chatId };
  state.highlight = highlightId;
  app.querySelector(".shell").classList.add("in-thread");
  const thread = app.querySelector("#thread");
  if (switching) {
    state.replyTo = null;
    state.editing = null;
    state.pendingFiles = [];
    state.awaitingOlder = false;
    state.messages = [];
    thread.replaceChildren(template("tpl-thread"));
    thread.style.setProperty("--bubble-out", NETWORK_COLORS[network(appId)] || "var(--accent)");
    wireThread(thread);
  }
  const query = highlightId ? `around=${encodeURIComponent(highlightId)}&` : "";
  const [detail, page] = await Promise.all([
    papi(appId, "GET", `v1/chats/${encodeURIComponent(chatId)}`),
    papi(appId, "GET", `v1/chats/${encodeURIComponent(chatId)}/messages?${query}limit=${PAGE_SIZE}`),
  ]);
  if (!isActive(appId, chatId)) return;
  setDetail(detail);
  state.messages = page.messages;
  state.hasMoreBefore = page.has_more_before;
  state.beforeCursor = page.before_cursor;
  renderMessages({ scroll: highlightId ? "highlight" : "bottom" });
  renderChatList();
  markRead();
}

function setDetail(detail) {
  const p = provider(state.active.appId);
  state.detail = detail;
  state.caps = detail.capabilities || (p && p.account ? p.account.capabilities : {}) || {};
  renderThreadHeader();
}

function renderThreadHeader() {
  const d = state.detail;
  const { appId } = state.active;
  const thread = app.querySelector("#thread");
  thread.querySelector('[data-field="avatar"]').replaceChildren(
    avatar(d.title, `${appId}:${d.id}`, fileURL(appId, d.avatar_url)));
  thread.querySelector('[data-field="name"]').textContent = d.title;
  const parts = [networkName(appId)];
  if (d.kind === "group") parts.push(`${d.participants.length} members`);
  if (d.disappearing_timer_s) parts.push(`⏱ ${formatDuration(d.disappearing_timer_s * 1000)}`);
  if (isMuted(d)) parts.push("muted");
  if (d.read_only) parts.push("imported history only");
  thread.querySelector('[data-field="subtitle"]').textContent = parts.join(" · ");
  thread.querySelector("#composer").hidden = d.read_only;
  thread.querySelector(".attach").hidden = !state.caps.attachments;
}

function wireThread(thread) {
  thread.querySelector('[data-action="back"]').addEventListener("click", () => { location.hash = ""; });
  thread.querySelector('[data-action="load-more"]').addEventListener("click", () => loadMore().catch((e) => toast(e.message)));
  thread.querySelector('[data-action="load-older"]').addEventListener("click", loadOlder);
  thread.querySelector('[data-action="cancel-reply"]').addEventListener("click", () => { setReply(null); setEditing(null); });
  const input = thread.querySelector("#composer-input");
  const fileInput = thread.querySelector("#file-input");
  let typingSent = 0;
  let typingStopTimer;
  const sendTyping = (typing) => {
    const { appId, chatId } = state.active;
    papi(appId, "POST", `v1/chats/${encodeURIComponent(chatId)}/typing`, { typing }).catch(() => {});
  };
  input.addEventListener("input", () => {
    input.style.height = "auto";
    input.style.height = `${input.scrollHeight}px`;
    if (state.editing || !state.caps.typing) return;
    const now = Date.now();
    if (input.value && now - typingSent > 8000) {
      typingSent = now;
      sendTyping(true);
    }
    clearTimeout(typingStopTimer);
    typingStopTimer = setTimeout(() => {
      if (typingSent) {
        typingSent = 0;
        sendTyping(false);
      }
    }, 4000);
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing && window.matchMedia("(min-width: 761px)").matches) {
      e.preventDefault();
      thread.querySelector("#composer").requestSubmit();
    } else if (e.key === "Escape" && state.editing) {
      setEditing(null);
    }
  });
  input.addEventListener("paste", (e) => {
    const files = [...e.clipboardData.files];
    if (files.length && state.caps.attachments) {
      e.preventDefault();
      addPendingFiles(files);
    }
  });
  fileInput.addEventListener("change", () => {
    addPendingFiles([...fileInput.files]);
    fileInput.value = "";
  });
  thread.querySelector("#composer").addEventListener("submit", async (e) => {
    e.preventDefault();
    const { appId, chatId } = state.active;
    const text = input.value.trim();
    const btn = thread.querySelector(".send");
    if (state.editing) {
      if (!text) return;
      btn.disabled = true;
      try {
        await papi(appId, "PATCH", `v1/messages/${encodeURIComponent(state.editing.id)}`, { text });
        setEditing(null);
      } catch (err) {
        toast(`Edit failed: ${err.message}`);
      } finally { btn.disabled = false; }
      return;
    }
    if (!text && !state.pendingFiles.length) return;
    const form = new FormData();
    form.append("text", text);
    // Makes a retried request (eg after a dropped connection) safe: the provider sends it once.
    form.append("client_id", crypto.randomUUID());
    if (state.replyTo) form.append("reply_to", state.replyTo.id);
    for (const f of state.pendingFiles) form.append("files[]", f, f.name);
    btn.disabled = true;
    try {
      await papi(appId, "POST", `v1/chats/${encodeURIComponent(chatId)}/messages`, form);
      input.value = "";
      input.style.height = "auto";
      typingSent = 0;
      setReply(null);
      state.pendingFiles = [];
      renderPendingFiles();
    } catch (err) {
      toast(`Send failed: ${err.message}`);
    } finally {
      btn.disabled = false;
      input.focus();
    }
  });
}

function addPendingFiles(files) {
  state.pendingFiles.push(...files);
  renderPendingFiles();
}

function renderPendingFiles() {
  const box = app.querySelector("#pending-files");
  if (!box) return;
  box.hidden = !state.pendingFiles.length;
  box.replaceChildren(...state.pendingFiles.map((f, i) => el("span", {},
    f.name, " ", el("button", { type: "button", class: "icon", onclick: () => {
      state.pendingFiles.splice(i, 1);
      renderPendingFiles();
    } }, "✕"))));
}

function showComposerBar(label, text) {
  const bar = app.querySelector("#reply-bar");
  if (!bar) return;
  bar.hidden = !label;
  if (!label) return;
  bar.querySelector('[data-field="reply-label"]').textContent = label;
  bar.querySelector('[data-field="reply-text"]').textContent = text;
  app.querySelector("#composer-input").focus();
}

function setReply(message) {
  state.replyTo = message;
  if (message) state.editing = null;
  showComposerBar(message && `Replying to ${personName(message.sender)}`, message ? (message.text || "(attachment)") : "");
}

function setEditing(message) {
  state.editing = message;
  const input = app.querySelector("#composer-input");
  if (message) {
    state.replyTo = null;
    input.value = message.text;
    input.dispatchEvent(new Event("input"));
  } else if (input) {
    input.value = "";
    input.style.height = "auto";
  }
  showComposerBar(message && "Editing message", message ? message.text : "");
}

async function loadMore() {
  if (!state.beforeCursor) return reloadActive();
  const { appId, chatId } = state.active;
  const page = await papi(appId, "GET",
    `v1/chats/${encodeURIComponent(chatId)}/messages?before=${encodeURIComponent(state.beforeCursor)}&limit=${PAGE_SIZE}`);
  if (!isActive(appId, chatId)) return;
  state.messages = page.messages.concat(state.messages);
  state.hasMoreBefore = page.has_more_before;
  if (page.before_cursor) state.beforeCursor = page.before_cursor;
  renderMessages({ scroll: "keep" });
}

// Once everything the provider has is shown, it may be able to fetch older messages from the network.
function renderHistoryButton() {
  const button = app.querySelector('[data-action="load-older"]');
  if (!button) return;
  button.hidden = !state.caps.load_older || state.hasMoreBefore || (state.detail && state.detail.read_only);
  button.disabled = state.awaitingOlder;
  const name = state.active ? networkName(state.active.appId) : "";
  button.textContent = state.awaitingOlder ? `Loading earlier messages from ${name}…` : `Load earlier messages from ${name}`;
}

async function loadOlder() {
  const { appId, chatId } = state.active;
  state.awaitingOlder = true;
  renderHistoryButton();
  try {
    await papi(appId, "POST", `v1/chats/${encodeURIComponent(chatId)}/history`);
  } catch (err) {
    state.awaitingOlder = false;
    renderHistoryButton();
    toast(err.message);
  }
}

let reloadTimer;
function reloadActiveSoon() {
  clearTimeout(reloadTimer);
  reloadTimer = setTimeout(() => reloadActive().catch((e) => console.error(e)), 100);
}

// Refetches the open chat's newest messages, as many as are loaded (up to a limit), keeping any older ones.
async function reloadActive() {
  if (!state.active) return;
  const { appId, chatId } = state.active;
  if (state.awaitingOlder) {
    state.awaitingOlder = false;
    await loadMore();
  }
  const limit = Math.min(Math.max(state.messages.length, PAGE_SIZE), 500);
  const [detail, newest] = await Promise.all([
    papi(appId, "GET", `v1/chats/${encodeURIComponent(chatId)}`),
    papi(appId, "GET", `v1/chats/${encodeURIComponent(chatId)}/messages?limit=${limit}`),
  ]);
  if (!isActive(appId, chatId)) return;
  setDetail(detail);
  const ids = new Set(newest.messages.map((m) => m.id));
  const first = newest.messages[0];
  const older = first ? state.messages.filter((m) => !ids.has(m.id) && new Date(m.sent_at) <= new Date(first.sent_at)) : [];
  state.messages = older.concat(newest.messages);
  if (!older.length) {
    state.hasMoreBefore = newest.has_more_before;
    state.beforeCursor = newest.before_cursor;
  }
  const box = app.querySelector("#messages");
  const atBottom = box && box.scrollHeight - box.scrollTop - box.clientHeight < 80;
  renderMessages({ scroll: atBottom ? "bottom" : "keep" });
  if (document.visibilityState === "visible") markRead();
}

let markReadTimer;
function markRead() {
  clearTimeout(markReadTimer);
  if (!state.active) return;
  const { appId, chatId } = state.active;
  const c = state.chats.find((x) => x.app_id === appId && x.id === chatId);
  if (!c || (!c.unread_count && !c.marked_unread)) return;
  markReadTimer = setTimeout(() => {
    if (isActive(appId, chatId)) papi(appId, "POST", `v1/chats/${encodeURIComponent(chatId)}/read`, {}).catch(() => {});
  }, 500);
}

// ---------------------------------------------------------------- messages

function renderMessages({ scroll }) {
  const box = app.querySelector("#messages");
  const list = app.querySelector("#message-list");
  if (!box || !list) return;
  const prevHeight = box.scrollHeight;
  const prevTop = box.scrollTop;
  app.querySelector('[data-action="load-more"]').hidden = !state.hasMoreBefore;
  renderHistoryButton();
  const isGroup = state.detail && state.detail.kind === "group";
  const items = [];
  let lastDay = null;
  let lastSender = null;
  for (const m of state.messages) {
    const day = new Date(m.sent_at).toDateString();
    if (day !== lastDay) {
      items.push(el("li", { class: "day" }, formatDay(m.sent_at)));
      lastDay = day;
      lastSender = null;
    }
    if (m.kind === "event") {
      items.push(el("li", { class: "update" }, m.event ? m.event.text : "", " · ", formatTime(m.sent_at)));
      lastSender = null;
      continue;
    }
    const showSender = isGroup && !m.outgoing && lastSender !== m.sender.id;
    items.push(renderMessage(m, showSender));
    lastSender = m.sender.id;
  }
  closePopover();
  list.replaceChildren(...items);
  if (scroll === "bottom") box.scrollTop = box.scrollHeight;
  else if (scroll === "keep") box.scrollTop = prevTop + (box.scrollHeight - prevHeight);
  else if (scroll === "highlight") {
    const target = list.querySelector(".msg.highlight");
    if (target) target.scrollIntoView({ block: "center" });
  }
}

const STATUS_TEXT = { pending: "Sending…", sent: "✓", delivered: "✓✓", read: "✓✓ Seen" };

function renderMessage(m, showSender) {
  const { appId } = state.active;
  const node = el("li", { class: `msg${m.outgoing ? " out" : ""}${m.id === state.highlight ? " highlight" : ""}` });
  if (showSender) node.append(el("span", { class: "author" }, personName(m.sender)));
  const onlySticker = !m.unsent && !m.text && m.attachments.length === 1 && m.attachments[0].kind === "sticker";
  const bubble = el("div", { class: `bubble${onlySticker ? " sticker" : ""}` });
  if (m.unsent) {
    bubble.classList.add("deleted");
    bubble.append(m.outgoing ? "You unsent a message." : "This message was unsent.");
  } else {
    if (m.reply_to) {
      const target = m.reply_to.message_id;
      bubble.append(el("div", {
        class: "quote",
        onclick: target ? () => { location.hash = chatHash(appId, state.active.chatId, target); } : null,
      }, el("strong", {}, m.reply_to.sender_name || ""), m.reply_to.text || "(attachment)"));
    }
    for (const a of m.attachments) {
      bubble.append(a.status === "available" ? attachmentNode({
        url: fileURL(appId, a.url), kind: a.kind, contentType: a.content_type, filename: a.filename, size: a.size,
        width: a.width, height: a.height, caption: a.caption,
      }) : missingAttachmentNode(a.status === "expired", a.filename));
    }
    if (m.text) bubble.append(el("div", { class: "text" }, ...linkify(m.text)));
    if (!m.text && !m.attachments.length) bubble.append(el("span", { class: "missing" }, "(empty message)"));
  }
  const row = el("div", { class: "msg-row" }, bubble);
  if (!m.unsent && !(state.detail && state.detail.read_only)) {
    const tools = renderTools(m, node);
    if (tools) row.append(tools);
  }
  node.append(row);
  if (m.reactions.length) {
    node.append(reactionsNode(m.reactions.map((r) => ({ emoji: r.emoji, name: personName(r.sender), mine: r.mine }))));
  }
  const meta = [formatTime(m.sent_at)];
  if (m.edited_at) meta.push("edited");
  if (m.expires_at) meta.push(`⏱ ${formatDuration(Math.max(0, new Date(m.expires_at) - Date.now()))}`);
  const metaNode = el("div", { class: "meta" }, meta.join(" · "));
  if (m.status === "failed") {
    const id = encodeURIComponent(m.id);
    metaNode.append(
      el("span", { class: "failed", title: m.error || "" }, `· Not sent${m.error ? `: ${m.error}` : ""}`),
      el("button", { type: "button", class: "link",
        onclick: () => papi(appId, "POST", `v1/messages/${id}/retry`).catch((e) => toast(e.message)) }, "Retry"),
      el("button", { type: "button", class: "link",
        onclick: () => papi(appId, "DELETE", `v1/messages/${id}`).catch((e) => toast(e.message)) }, "Discard"));
  } else if (m.status) {
    metaNode.append(`· ${STATUS_TEXT[m.status] || m.status}`);
  }
  node.append(metaNode);
  tapToShowTools(node);
  return node;
}

function withinWindow(m, maxAgeS) {
  return maxAgeS === null || maxAgeS === undefined || Date.now() - new Date(m.sent_at) < maxAgeS * 1000;
}

// The icons beside a bubble: react and reply where the chat allows them, and a menu of the rest.
function renderTools(m, node) {
  const caps = state.caps;
  const tools = el("div", { class: "tools" });
  if (caps.reactions) tools.append(toolButton("react", "React", (e) => openPopover(e.currentTarget, node, picker(m))));
  if (caps.reply) tools.append(toolButton("reply", "Reply", () => setReply(m)));
  const items = menuItems(m);
  if (items.length) tools.append(toolButton("more", "More", (e) => openPopover(e.currentTarget, node, menu(items))));
  return tools.childElementCount ? tools : null;
}

function picker(m) {
  const { appId } = state.active;
  const mine = m.reactions.find((r) => r.mine);
  const emojis = state.caps.reactions.emoji || DEFAULT_REACTIONS;
  const id = encodeURIComponent(m.id);
  return reactionPicker(emojis, mine ? mine.emoji : null, (emoji) => {
    const done = emoji === null
      ? papi(appId, "DELETE", `v1/messages/${id}/reactions/${encodeURIComponent(mine.emoji)}`)
      : papi(appId, "POST", `v1/messages/${id}/reactions`, { emoji });
    done.catch((e) => toast(e.message));
  });
}

function menuItems(m) {
  const caps = state.caps;
  const { appId } = state.active;
  const items = [];
  const sent = m.outgoing && m.status !== "pending" && m.status !== "failed";
  if (sent && caps.edit && m.text && !m.attachments.length && withinWindow(m, caps.edit.max_age_s)) {
    items.push(["Edit", () => setEditing(m)]);
  }
  if (sent && caps.unsend && withinWindow(m, caps.unsend.max_age_s)) {
    items.push(["Unsend", async () => {
      if (!confirm("Unsend this message for everyone?")) return;
      await papi(appId, "DELETE", `v1/messages/${encodeURIComponent(m.id)}`).catch((e) => toast(e.message));
    }]);
  }
  if (m.text) items.push(["Copy text", () => navigator.clipboard.writeText(m.text).catch((e) => toast(e.message))]);
  return items;
}

function renderTyping() {
  const box = app.querySelector("#typing");
  if (!box || !state.active) return;
  const typing = state.typing.has(chatKey(state.active.appId, state.active.chatId));
  box.hidden = !typing;
  box.textContent = typing ? "typing…" : "";
}

// ---------------------------------------------------------------- new chat

function openNewChat() {
  const frag = template("tpl-new-chat");
  const backdrop = frag.querySelector(".modal-backdrop");
  document.body.append(frag);
  const close = () => backdrop.remove();
  backdrop.querySelector('[data-action="close"]').addEventListener("click", close);
  backdrop.addEventListener("click", (e) => { if (e.target === backdrop) close(); });
  const usable = [...state.providers.values()].filter((p) => p.account && p.account.state === "connected"
    && (p.account.capabilities.start_chat || []).length);
  const picker = backdrop.querySelector("#network-picker");
  const people = backdrop.querySelector("#people");
  const phoneForm = backdrop.querySelector("#phone-form");
  let current = null;
  if (!usable.length) {
    picker.replaceChildren(el("p", { class: "hint" }, "None of your messaging apps is connected."));
    backdrop.querySelector("#people-search").hidden = true;
    return;
  }
  const start = async (body) => {
    try {
      const chat = await papi(current.app_id, "POST", "v1/chats", body);
      close();
      location.hash = chatHash(current.app_id, chat.id);
    } catch (err) { toast(err.message); }
  };
  const search = async (q) => {
    people.replaceChildren(el("li", { class: "hint" }, "Searching…"));
    try {
      const res = await papi(current.app_id, "GET", `v1/people?q=${encodeURIComponent(q)}`);
      people.replaceChildren(...res.people.filter((p) => p.address).map((p) => el("li", {
        onclick: () => start(p.id ? { person_id: p.id } : { address: p.address }),
      }, avatar(p.name, `${current.app_id}:${p.address}`, fileURL(current.app_id, p.avatar_url)),
      el("span", {}, p.name || p.address, p.phone ? el("small", { class: "hint" }, ` ${p.phone}`) : null))));
      if (!people.childElementCount) people.append(el("li", { class: "hint" }, "Nobody found."));
    } catch (err) {
      people.replaceChildren(el("li", { class: "hint" }, err.message));
    }
  };
  const choose = (p) => {
    current = p;
    for (const b of picker.children) b.classList.toggle("active", b.dataset.app === p.app_id);
    phoneForm.hidden = !p.account.capabilities.start_chat.includes("phone");
    people.replaceChildren();
    search(backdrop.querySelector("#people-query").value.trim());
  };
  picker.replaceChildren(...usable.map((p) => el("button", {
    type: "button", "data-app": p.app_id, onclick: () => choose(p),
  }, networkName(p.app_id))));
  backdrop.querySelector("#people-search").addEventListener("submit", (e) => {
    e.preventDefault();
    search(backdrop.querySelector("#people-query").value.trim());
  });
  phoneForm.addEventListener("submit", (e) => {
    e.preventDefault();
    const phone = backdrop.querySelector("#phone-input").value.trim();
    if (phone) start({ phone });
  });
  choose(usable[0]);
  backdrop.querySelector("#people-query").focus();
}

// ---------------------------------------------------------------- live events

function connectEvents() {
  const source = new EventSource("/api/events");
  source.addEventListener("hello", () => {
    refreshState().catch((e) => console.error(e));
    refreshChatsSoon();
    if (state.active) reloadActiveSoon();
  });
  source.addEventListener("providers", () => refreshState().catch((e) => console.error(e)));
  source.addEventListener("chat", (e) => {
    const { app_id: appId, chat_id: chatId } = JSON.parse(e.data);
    refreshChatsSoon();
    if (state.active && state.active.appId === appId && (!chatId || chatId === state.active.chatId)) reloadActiveSoon();
  });
  source.addEventListener("typing", (e) => {
    const t = JSON.parse(e.data);
    const key = chatKey(t.app_id, t.chat_id);
    clearTimeout(state.typing.get(key));
    if (t.typing) {
      state.typing.set(key, setTimeout(() => {
        state.typing.delete(key);
        renderTyping();
        renderChatList();
      }, 8000));
    } else {
      state.typing.delete(key);
    }
    renderTyping();
    renderChatList();
  });
  // The server ends the stream if this page falls behind; reconnect, and "hello" refetches everything.
  source.addEventListener("error", () => {
    if (source.readyState === EventSource.CLOSED) setTimeout(connectEvents, 2000);
  });
}

document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible" && state.active) markRead();
});

// ---------------------------------------------------------------- boot

app.querySelector('[data-action="new-chat"]').addEventListener("click", openNewChat);
let searchTimer;
app.querySelector("#search").addEventListener("input", (e) => {
  clearTimeout(searchTimer);
  const q = e.target.value.trim();
  searchTimer = setTimeout(() => runSearch(q).catch((err) => toast(err.message)), 250);
});
window.addEventListener("hashchange", routeFromHash);
await refreshState();
await refreshChats();
routeFromHash();
connectEvents();
