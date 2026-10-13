const http = require("node:http");
const https = require("node:https");
const crypto = require("node:crypto");
const fs = require("node:fs");
const { chromium } = require("playwright-core");
const { WebSocketServer } = require('ws');
const path = require('node:path');

const host = process.env.API_HOST || "10.2.0.12";
const port = Number(process.env.API_PORT || 9186);
const token = process.env.CLAUDE_BROWSER_API_TOKEN;
const cdpUrl = process.env.CDP_URL || "http://127.0.0.1:9222";
const tlsCert = process.env.API_TLS_CERT;
const tlsKey = process.env.API_TLS_KEY;
const model = process.env.API_MODEL || "claude-browser";
const mock = process.env.API_MOCK === "1";
const maxPromptChars = Number(process.env.API_MAX_PROMPT_CHARS || 8000);
let browser;
let page;
let queue = Promise.resolve();
const bridgePages = new WeakSet();
let activeNetworkStream = null;
const conversationSnapshots = new Map();
const websocketClients = new Set();
const modalSnapshots = new Map();
const browserSessions = new Map();
const files = new Map();
const downloadedArtifactKeys = new Set();
const downloadedArtifactLinks = new Map();
const downloadDir = '/home/ubuntu/Downloads/claude-browser';
function broadcastConversation(conversationId, event) {
  for (const client of websocketClients) if (client.authenticated && client.conversationId === conversationId && client.readyState === 1) client.send(JSON.stringify(event));
}
function broadcastModal(conversationId, modal) {
  for (const client of websocketClients) if (client.authenticated && (!conversationId || client.conversationId === conversationId) && client.readyState === 1) client.send(JSON.stringify({ type: 'ui.modal', conversationId, modal }));
}
async function detectModal(p) {
  return p.evaluate(() => {
    const candidates = [...document.querySelectorAll('[role="dialog"], [aria-modal="true"]')].filter(node => {
      const style = getComputedStyle(node);
      const rect = node.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && style.visibility !== 'hidden' && style.display !== 'none';
    });
    const node = candidates.at(-1);
    if (!node) return null;
    const buttons = [...node.querySelectorAll('button')].map(button => ({ label: (button.innerText || button.getAttribute('aria-label') || '').trim().replace(/\s+/g, ' ') })).filter(item => item.label).slice(0, 8);
    const heading = node.querySelector('h1,h2,h3,[role="heading"]');
    const title = (heading?.textContent || '').trim().replace(/\s+/g, ' ');
    const content = (node.textContent || '').trim().replace(/\s+/g, ' ');
    return { title: title || 'Claude 提示', content, buttons };
  }).catch(() => null);
}
async function publishModal(p, conversationId) {
  const found = await detectModal(p);
  const key = conversationId || 'global';
  const previous = modalSnapshots.get(key);
  if (!found) {
    if (previous) { modalSnapshots.delete(key); broadcastModal(conversationId, null); }
    return null;
  }
  const modal = { ...found, id: `modal-${crypto.createHash('sha1').update(JSON.stringify(found)).digest('hex').slice(0, 12)}`, kind: /验证码|验证|captcha|challenge/i.test(`${found.title} ${found.content}`) ? 'security_verification' : /登录|sign in|login/i.test(`${found.title} ${found.content}`) ? 'login_required' : 'claude_web_notice', blocking: true };
  if (!previous || previous.id !== modal.id) { modalSnapshots.set(key, modal); broadcastModal(conversationId, modal); }
  return modal;
}
async function installModalObserver(p) {
  if (p.__lightnodeModalObserver) return;
  await p.exposeFunction('__lightnodeModalChanged', async () => {
    const id = new URL(p.url()).pathname.match(/^\/chat\/([^/]+)/)?.[1] || 'global';
    await publishModal(p, id);
  });
  await p.evaluate(() => {
    if (window.__lightnodeModalObserverInstalled) return;
    window.__lightnodeModalObserverInstalled = true;
    let timer;
    const observer = new MutationObserver(() => { clearTimeout(timer); timer = setTimeout(() => window.__lightnodeModalChanged(), 120); });
    observer.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['aria-hidden', 'aria-modal', 'style', 'class'] });
  });
  p.__lightnodeModalObserver = true;
  const id = new URL(p.url()).pathname.match(/^\/chat\/([^/]+)/)?.[1] || 'global';
  await publishModal(p, id);
}
function safeFileName(value, fallback) {
  const name = String(value || '').split(/[\\/]/).pop() || fallback;
  return name.replace(/[^a-zA-Z0-9._-]/g, '_').slice(-120) || fallback;
}
async function materializeLinks(text) {
  const matches = [...text.matchAll(/https?:\/\/[^\s)]+/g)].map(match => match[0].replace(/[.,;]+$/, ''));
  const unique = [...new Set(matches)].slice(0, 3);
  if (!unique.length) return text;
  await fs.promises.mkdir(downloadDir, { recursive: true });
  const links = [];
  for (const url of unique) {
    try {
      // Use the remote Chrome context so signed Claude downloads and session
      // protected links retain the same cookies/authentication as the page.
      const p = await getPage();
      const response = await p.context().request.get(url, { timeout: 60000, maxRedirects: 5, failOnStatusCode: false });
      if (!response.ok()) continue;
      const size = Number(response.headers()['content-length'] || 0);
      if (size > 100 * 1024 * 1024) continue;
      const data = await response.body();
      if (data.length > 100 * 1024 * 1024) continue;
      const id = crypto.randomUUID();
      const disposition = response.headers()['content-disposition'] || '';
      const dispositionName = disposition.match(/filename\*?=(?:UTF-8''|\"?)([^;\"]+)/i)?.[1];
      const name = dispositionName || path.basename(new URL(url).pathname) || `download-${id}`;
      const safe = safeFileName(decodeURIComponent(name), `download-${id}`);
      const target = path.join(downloadDir, `${id}-${safe}`);
      await fs.promises.writeFile(target, data);
      files.set(id, { path: target, name: safe });
      links.push({ id, name: safe });
    } catch { /* keep original answer when a link is not downloadable */ }
  }
  return links.length ? `${text}\n\n下载文件：\n${links.map(item => `- [${item.name}](/api/files/${item.id})`).join('\n')}` : text;
}

function authorized(req) {
  const actual = Buffer.from(req.headers.authorization || "");
  const wanted = Buffer.from(`Bearer ${token || ""}`);
  return Boolean(token) && actual.length === wanted.length && crypto.timingSafeEqual(actual, wanted);
}
function json(res, status, value) {
  res.writeHead(status, { "content-type": "application/json; charset=utf-8", "cache-control": "no-store", "access-control-allow-origin": process.env.API_CORS_ORIGIN || "http://localhost:5190", "access-control-allow-headers": "Authorization, Content-Type", "access-control-allow-methods": "GET, POST, OPTIONS" });
  res.end(JSON.stringify(value));
}
function readBody(req) {
  return new Promise((resolve, reject) => {
    let data = "";
    req.on("data", chunk => { data += chunk; if (data.length > 512 * 1024) reject(new Error("request too large")); });
    req.on("end", () => { try { resolve(JSON.parse(data || "{}")); } catch { reject(new Error("invalid JSON")); } });
    req.on("error", reject);
  });
}
function textFromContent(content) {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content.filter(item => item && item.type === "text").map(item => item.text || "").join("\n");
}
function promptFromMessages(messages) {
  if (!Array.isArray(messages)) return "";
  const latest = [...messages].reverse().find(item => item && item.role === "user");
  let prompt = textFromContent(latest?.content).trim();
  if (prompt.length > maxPromptChars) prompt = prompt.slice(-maxPromptChars);
  return prompt;
}
async function getPage() {
  if (!browser) browser = await chromium.connectOverCDP(cdpUrl);
  const context = browser.contexts()[0];
  if (!context) throw new Error("browser context unavailable");
  const pages = context.pages();
  page = pages.find(item => item.url().includes("claude.ai")) || pages[0];
  if (!page) page = await context.newPage();
  if (!page.url().includes("claude.ai")) await page.goto("https://claude.ai/", { waitUntil: "domcontentloaded" });
  await installNetworkBridge(page);
  await installModalObserver(page);
  return page;
}
async function installNetworkBridge(p) {
  if (bridgePages.has(p)) return;
  await p.exposeFunction("__lightnodeClaudeSseEvent", event => {
    const state = activeNetworkStream;
    if (!state || !event || !String(event.url || '').includes('/completion')) return;
    if (event.phase === 'start') state.started = true;
    if (event.phase === 'chunk' && event.chunk) processClaudeSseChunk(state, event.chunk);
    if (event.phase === 'end') { state.ended = true; state.resolveEnd?.(); }
  });
  await p.evaluate(() => {
    if (window.__lightnodeClaudeFetchPatched) return;
    window.__lightnodeClaudeFetchPatched = true;
    const originalFetch = window.fetch.bind(window);
    window.fetch = async (...args) => {
      const response = await originalFetch(...args);
      const requestUrl = typeof args[0] === 'string' ? args[0] : args[0]?.url || '';
      if (!requestUrl.includes('/completion') || !response.body) return response;
      void (async () => {
        try {
          await window.__lightnodeClaudeSseEvent({ phase: 'start', url: requestUrl });
          const reader = response.clone().body.getReader();
          const decoder = new TextDecoder();
          while (true) {
            const { value, done } = await reader.read();
            if (done) break;
            await window.__lightnodeClaudeSseEvent({ phase: 'chunk', url: requestUrl, chunk: decoder.decode(value, { stream: true }) });
          }
          await window.__lightnodeClaudeSseEvent({ phase: 'end', url: requestUrl });
        } catch (error) { await window.__lightnodeClaudeSseEvent({ phase: 'end', url: requestUrl, error: String(error) }); }
      })();
      return response;
    };
  });
  bridgePages.add(p);
}
function processClaudeSseChunk(state, chunk) {
  state.buffer += chunk;
  const lines = state.buffer.split(/\r?\n/);
  state.buffer = lines.pop() || '';
  for (const line of lines) {
    if (line.startsWith('data:')) state.dataLines.push(line.slice(5).trim());
    if (line.trim() === '' && state.dataLines.length) {
      const payload = state.dataLines.join('\n');
      state.dataLines = [];
      if (payload === '[DONE]') continue;
      try { const value = JSON.parse(payload); const text = claudeDeltaText(value); if (text) state.onDelta(text); } catch { /* incomplete or non-JSON event */ }
    }
  }
}
function claudeDeltaText(value) {
  if (!value || typeof value !== 'object') return '';
  if (typeof value.delta?.text === 'string') return value.delta.text;
  if (typeof value.completion === 'string') return value.completion;
  if (typeof value.content_block?.text === 'string') return value.content_block.text;
  if (Array.isArray(value.content)) return value.content.filter(item => item?.type === 'text').map(item => item.text || '').join('');
  return '';
}
async function composer(p) {
  await p.waitForLoadState('domcontentloaded').catch(() => {});
  for (const item of [p.locator('[contenteditable="true"]').last(), p.locator('textarea').last(), p.locator('[role="textbox"]').last()]) {
    if (await item.count() && await item.isVisible().catch(() => false)) return item;
  }
  await p.waitForTimeout(2000);
  for (const item of [p.locator('[contenteditable="true"]').last(), p.locator('textarea').last(), p.locator('[role="textbox"]').last()]) {
    if (await item.count() && await item.isVisible().catch(() => false)) return item;
  }
  throw new Error("Claude composer not found");
}
async function webConversations() {
  const p = await getPage();
  return p.evaluate(() => {
    const seen = new Set();
    return [...document.querySelectorAll('a[href*="/chat/"]')].map(a => {
      const href = a.getAttribute('href') || '';
      const id = href.split('/chat/')[1]?.split(/[?#]/)[0];
      return { id, title: (a.textContent || '').trim().replace(/\s+/g, ' ') || 'Claude 对话', url: href };
    }).filter(item => item.id && !seen.has(item.id) && seen.add(item.id)).slice(0, 100);
  });
}
async function currentConversation() {
  const p = await getPage();
  const snapshot = await p.evaluate(() => {
    const id = location.pathname.match(/^\/chat\/([^/]+)/)?.[1] || 'new';
    const messages = [...document.querySelectorAll('[data-testid="user-message"], [data-testid="assistant-message"]')].map((node, index) => {
      const role = node.getAttribute('data-testid') === 'user-message' ? 'user' : 'assistant';
      const copy = node.cloneNode(true);
      copy.querySelectorAll('.sr-only, [data-testid="message-actions"]').forEach(el => el.remove());
      const body = role === 'assistant' ? copy.querySelector('.font-claude-response') || copy : copy;
      const content = (body.textContent || '').trim();
      const links = [...body.querySelectorAll('a[href]')].map(anchor => ({ label: (anchor.textContent || '').trim().replace(/\s+/g, ' '), href: anchor.href || anchor.getAttribute('href') || '' })).filter(item => item.href && item.label && /(下载|文档|\.md\b|\.pdf\b|\.docx?\b|\.xlsx?\b|\.csv\b|\.zip\b)/i.test(`${item.label} ${item.href}`)).slice(0, 5);
      return { id: `${id}_${index}`, role, content, links };
    }).filter(message => message.content);
    return { id, title: document.title.replace(/\s*-\s*Claude$/, ''), messages, updated: new Date().toISOString() };
  });
  snapshot.modal = await publishModal(p, snapshot.id);
  for (const message of snapshot.messages) if (message.role === 'assistant') {
    message.content = appendDomLinks(message.content, message.links || []);
    delete message.links;
    message.content = await materializeLinks(message.content);
  }
  const artifactLinks = await materializeArtifactDownloads(p, snapshot.id);
  if (artifactLinks.length) {
    const lastAssistant = [...snapshot.messages].reverse().find(message => message.role === 'assistant');
    if (lastAssistant) lastAssistant.content = appendDomLinks(lastAssistant.content, artifactLinks.map(item => ({ label: item.name, href: `/api/files/${item.id}` })));
  }
  conversationSnapshots.set(snapshot.id, snapshot);
  return snapshot;
}
async function openNewConversation() {
  const p = await getPage();
  await p.goto('https://claude.ai/new', { waitUntil: 'domcontentloaded' });
  await p.waitForTimeout(800);
  return { id: p.url().split('/chat/')[1]?.split(/[?#]/)[0] || `new_${Date.now()}`, title: '新对话', url: p.url(), messages: [] };
}
function networkAssistantText(data) {
  const messages = Array.isArray(data?.chat_messages) ? data.chat_messages : [];
  const assistant = [...messages].reverse().find(item => item && (item.sender === 'assistant' || item.sender === 'assistant_message'));
  if (!assistant) return '';
  if (typeof assistant.text === 'string') return assistant.text.trim();
  if (Array.isArray(assistant.content)) return assistant.content.filter(item => item?.type === 'text').map(item => item.text || '').join('').trim();
  return '';
}
function isTransientAssistant(text) { return /^(筛选中|运行中|Claude 正在思考|Thinking|Generating|Stop generating)\s*$/i.test(text.trim()); }
async function domDownloadLinks(p) {
  return p.evaluate(() => {
    const node = [...document.querySelectorAll('[data-testid="assistant-message"]')].at(-1);
    if (!node) return [];
    return [...node.querySelectorAll('a[href]')].map(anchor => ({
      label: (anchor.textContent || '').trim().replace(/\s+/g, ' '),
      href: anchor.href || anchor.getAttribute('href') || ''
    })).filter(item => item.href && item.label && /(下载|文档|\.md\b|\.pdf\b|\.docx?\b|\.xlsx?\b|\.csv\b|\.zip\b)/i.test(`${item.label} ${item.href}`)).slice(0, 5);
  }).catch(() => []);
}
function appendDomLinks(text, links) {
  if (!links.length) return text;
  const existing = new Set([...text.matchAll(/\[[^\]]+\]\((https?:\/\/[^)]+)\)/g)].map(match => match[1]));
  const extra = links.filter(item => !existing.has(item.href)).map(item => `[${item.label}](${item.href})`);
  return extra.length ? `${text}\n\n${extra.join('\n')}` : text;
}
async function materializeArtifactDownloads(p, conversationId = '') {
  await fs.promises.mkdir(downloadDir, { recursive: true });
  const buttons = p.locator('[data-testid="assistant-message"]').last().locator('button[aria-label*="下载"], button[aria-label*="Download"]');
  const count = await buttons.count().catch(() => 0);
  const links = [];
  for (let index = 0; index < Math.min(count, 5); index++) {
    const button = buttons.nth(index);
    const label = await button.getAttribute('aria-label').catch(() => '') || `download-${index + 1}`;
    const key = `${conversationId}:${label}`;
    if (downloadedArtifactKeys.has(key)) {
      const existing = downloadedArtifactLinks.get(key);
      if (existing) links.push(existing);
      continue;
    }
    try {
      const downloadPromise = p.waitForEvent('download', { timeout: 30000 });
      await button.click();
      const download = await downloadPromise;
      const id = crypto.randomUUID();
      const safe = safeFileName(download.suggestedFilename(), `download-${id}`);
      const target = path.join(downloadDir, `${id}-${safe}`);
      // Connected-over-CDP Chrome may remove Playwright's temporary artifact
      // before saveAs runs. Read the download stream directly instead.
      const stream = await download.createReadStream();
      if (!stream) throw new Error('download stream unavailable');
      const chunks = [];
      for await (const chunk of stream) chunks.push(Buffer.from(chunk));
      let data = Buffer.concat(chunks);
      // Claude artifacts are often blob: URLs. Chromium exposes the download
      // event, but CDP's download stream can be empty; fetch the blob inside
      // the page that owns it in that case.
      if (!data.length && download.url().startsWith('blob:')) {
        const encoded = await p.evaluate(async url => {
          const response = await fetch(url);
          const bytes = new Uint8Array(await response.arrayBuffer());
          if (bytes.length > 100 * 1024 * 1024) throw new Error('download exceeds 100MB');
          let binary = '';
          for (let offset = 0; offset < bytes.length; offset += 0x8000) binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
          return btoa(binary);
        }, download.url());
        data = Buffer.from(encoded, 'base64');
      }
      if (!data.length) throw new Error('download is empty');
      if (data.length > 100 * 1024 * 1024) throw new Error('download exceeds 100MB');
      await fs.promises.writeFile(target, data);
      files.set(id, { path: target, name: safe });
      downloadedArtifactKeys.add(key);
      const link = { id, name: safe };
      downloadedArtifactLinks.set(key, link);
      links.push(link);
    } catch (error) {
      console.warn(`artifact download failed label=${label} error=${error.name || 'error'} message=${String(error.message || error).slice(0, 240)}`);
    }
  }
  return links;
}
async function sendToClaude(text, onDelta = () => {}) {
  if (mock) return `VS Code 接口联调成功（Mock 模式）。已收到 ${text.length} 个字符；当前没有访问远程 Claude。`;
  const p = await getPage();
  const input = await composer(p);
  const beforeCount = await p.locator('[data-testid="assistant-message"]').count();
  const beforeText = await p.locator('[data-testid="assistant-message"]').last().locator('.font-claude-response').innerText().catch(() => '');
  const conversationId = new URL(p.url()).pathname.split('/chat/')[1]?.split(/[?#]/)[0] || '';
  const networkConversations = [];
  const streamState = { started: false, ended: false, buffer: '', dataLines: [], onDelta, endPromise: null, resolveEnd: null };
  streamState.endPromise = new Promise(resolve => { streamState.resolveEnd = resolve; });
  activeNetworkStream = streamState;
  const onConversationResponse = async response => {
    const request = response.request();
    if (request.method() !== 'GET' || !conversationId || !response.url().includes(`/chat_conversations/${conversationId}`) || response.url().includes('/composer_notices')) return;
    try { networkConversations.push(await response.json()); } catch { /* response may still be streaming */ }
  };
  p.on('response', onConversationResponse);
  const network = new Map();
  const started = new Map();
  const apiRequest = request => request.resourceType() === "fetch" || request.resourceType() === "xhr";
  const onRequest = request => { if (apiRequest(request)) started.set(request, Date.now()); };
  const onResponse = response => { const request = response.request(); if (apiRequest(request)) network.set(request, { status: response.status(), started: started.get(request) || Date.now(), finished: 0 }); };
  const onRequestDone = request => { if (apiRequest(request)) { const item = network.get(request) || { status: 0, started: started.get(request) || Date.now() }; item.finished = Date.now(); network.set(request, item); } };
  p.on("request", onRequest);
  p.on("response", onResponse);
  p.on("requestfinished", onRequestDone);
  p.on("requestfailed", onRequestDone);
  const completionResponse = p.waitForResponse(response => response.request().method() === 'POST' && response.url().includes(`/chat_conversations/${conversationId}/completion`), { timeout: 120000 }).catch(() => null);
  await input.click();
  await input.fill(text).catch(() => p.keyboard.insertText(text));
  await p.keyboard.press("Enter");
  try {
    await completionResponse;
    let fromNetwork = '';
    for (let attempt = 0; attempt < 20; attempt++) {
      await p.waitForTimeout(250);
      const candidate = networkAssistantText(networkConversations.at(-1));
      if (candidate && candidate !== beforeText.trim() && !isTransientAssistant(candidate)) { fromNetwork = candidate; break; }
    }
    if (fromNetwork) {
      const artifactLinks = await materializeArtifactDownloads(p, conversationId);
      const domLinks = await domDownloadLinks(p);
      return appendDomLinks(appendDomLinks(fromNetwork.slice(-100000), domLinks), artifactLinks.map(item => ({ label: item.name, href: `/api/files/${item.id}` })));
    }
    const domResult = await p.evaluate(async ({ initialCount, initialText, timeoutAt }) => {
    const generating = /Stop generating|正在生成|Generating/i;
    return new Promise((resolve, reject) => {
      let last = "";
      let stable = 0;
      let timer;
      let interval;
      const root = document.querySelector("main") || document.body;
      const observer = new MutationObserver(inspect);
      const finish = (error, value) => {
        observer.disconnect();
        clearTimeout(timer);
        clearInterval(interval);
        error ? reject(new Error(error)) : resolve(value);
      };
      function inspect() {
        if (Date.now() > timeoutAt) return finish("generation timeout");
        const nodes = [...document.querySelectorAll('[data-testid="assistant-message"]')];
        if (nodes.length <= initialCount && !((nodes.at(-1)?.querySelector('.font-claude-response')?.innerText || '').trim() !== initialText)) return;
        const textNode = nodes[nodes.length - 1].querySelector(".font-claude-response");
        const value = (textNode?.innerText || "").trim();
        const status = document.querySelector('[data-testid="response-status-announcer"]')?.innerText || "";
        if (!value || generating.test(status)) return;
        stable = value === last ? stable + 1 : 0;
        last = value;
        if (stable >= 3) {
          const links = [...nodes[nodes.length - 1].querySelectorAll('a[href]')].map(anchor => ({ label: (anchor.textContent || '').trim().replace(/\s+/g, ' '), href: anchor.href || anchor.getAttribute('href') || '' })).filter(item => item.href && item.label && /(下载|文档|\.md\b|\.pdf\b|\.docx?\b|\.xlsx?\b|\.csv\b|\.zip\b)/i.test(`${item.label} ${item.href}`)).slice(0, 5);
          finish(null, { text: value.slice(-100000), links });
        }
      }
      observer.observe(root, { subtree: true, childList: true, characterData: true });
      timer = setTimeout(() => finish("generation timeout"), Math.max(1000, timeoutAt - Date.now()));
      interval = setInterval(inspect, 500);
      inspect();
    });
    }, { initialCount: beforeCount, initialText: beforeText.trim(), timeoutAt: Date.now() + 120000 });
    const artifactLinks = await materializeArtifactDownloads(p, conversationId);
    return appendDomLinks(appendDomLinks(domResult.text, domResult.links || []), artifactLinks.map(item => ({ label: item.name, href: `/api/files/${item.id}` })));
  } finally {
    activeNetworkStream = null;
    p.off("request", onRequest);
    p.off("response", onResponse);
    p.off("requestfinished", onRequestDone);
    p.off("requestfailed", onRequestDone);
    p.off('response', onConversationResponse);
    console.log(`claude network api_requests=${network.size} completed=${[...network.values()].filter(item => item.finished).length}`);
  }
}
function enqueue(text, onDelta) {
  const task = queue.then(() => sendToClaude(text, onDelta));
  queue = task.catch(() => {});
  return task;
}
function completion(id, text) {
  return { id, object: "chat.completion", created: Math.floor(Date.now() / 1000), model,
    choices: [{ index: 0, message: { role: "assistant", content: text }, finish_reason: "stop" }],
    usage: { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 } };
}
function beginStream(res) {
  res.writeHead(200, { "content-type": "text/event-stream; charset=utf-8", "cache-control": "no-cache, no-transform", connection: "keep-alive", "x-accel-buffering": "no", "access-control-allow-origin": process.env.API_CORS_ORIGIN || "http://localhost:5190" });
  res.socket?.setNoDelay(true);
  res.flushHeaders?.();
}
async function stream(res, result) {
  const { id, created } = result;
  const content = result.choices[0].message.content;
  for (let offset = 0; offset < content.length; offset += 800) {
    res.write(`data: ${JSON.stringify({ id, object: "chat.completion.chunk", created, model, choices: [{ index: 0, delta: { content: content.slice(offset, offset + 800) }, finish_reason: null }] })}\r\n\r\n`);
    await new Promise(resolve => setTimeout(resolve, 10));
  }
  res.write(`data: ${JSON.stringify({ id, object: "chat.completion.chunk", created, model, choices: [{ index: 0, delta: {}, finish_reason: "stop" }] })}\r\n\r\n`);
  res.write("data: [DONE]\r\n\r\n");
  await new Promise(resolve => setTimeout(resolve, 2000));
  if (!res.destroyed) res.end();
}
function streamStart(res, id) {
  beginStream(res);
  res.write(`data: ${JSON.stringify({ id, object: "chat.completion.chunk", created: Math.floor(Date.now() / 1000), model, choices: [{ index: 0, delta: { role: "assistant", content: "" }, finish_reason: null }] })}\n\n`);
}
async function handler(req, res) {
  const requestId = String(req.headers['x-request-id'] || crypto.randomUUID());
  res.setHeader('x-request-id', requestId);
  if (req.method === "OPTIONS") { res.writeHead(204, { "access-control-allow-origin": process.env.API_CORS_ORIGIN || "http://localhost:5190", "access-control-allow-headers": "Authorization, Content-Type", "access-control-allow-methods": "GET, POST, OPTIONS" }); return res.end(); }
  if (req.url === "/healthz") return json(res, 200, { status: "ok", provider: "claude-browser", protocol: "openai-chat-completions", mock });
  if (!authorized(req)) return json(res, 401, { error: { message: "unauthorized", type: "authentication_error" } });
  if (req.method === "GET" && req.url === "/v1/models") return json(res, 200, { object: "list", data: [{ id: model, object: "model", created: 0, owned_by: "claude-browser" }] });
  if (req.method === "GET" && req.url === "/v1/conversations") return json(res, 200, { data: await webConversations() });
  if (req.method === "POST" && req.url === "/v1/sessions") {
    try {
      const input = await readBody(req).catch(() => ({}));
      const p = await getPage();
      const sessionPage = await p.context().newPage();
      const id = crypto.randomUUID();
      browserSessions.set(id, { id, page: sessionPage, createdAt: new Date().toISOString() });
      await sessionPage.goto('https://claude.ai/new', { waitUntil: 'domcontentloaded' }).catch(error => { if (!String(error?.message || error).includes('ERR_ABORTED')) throw error; });
      await sessionPage.bringToFront().catch(() => {});
      await installNetworkBridge(sessionPage);
      await installModalObserver(sessionPage);
      return json(res, 201, { id, name: String(input.name || `session-${id.slice(0, 8)}`), status: 'login_required', remoteDesktopUrl: `https://154.8.225.152:8445/?session=${id}` });
    } catch (error) {
      return json(res, 502, { error: { message: `创建浏览器会话失败: ${error.message}` } });
    }
  }
  const sessionMatch = req.url.match(/^\/v1\/sessions\/([a-f0-9-]+)$/);
  if (req.method === "GET" && sessionMatch) {
    const session = browserSessions.get(sessionMatch[1]);
    if (!session) return json(res, 404, { error: { message: 'session_not_found' } });
    return json(res, 200, { id: session.id, createdAt: session.createdAt, url: session.page.url(), closed: session.page.isClosed() });
  }
  if (req.method === "GET" && req.url === "/v1/conversations/current") {
    try { return json(res, 200, await currentConversation()); }
    catch (error) { return json(res, 502, { error: { message: error.message } }); }
  }
  if (req.method === "POST" && req.url === "/v1/conversations") return json(res, 201, await openNewConversation());
  if (req.method === "POST" && req.url === "/v1/ui/modal/action") {
    try {
      const input = await readBody(req);
      const label = String(input.label || '').trim();
      if (!label || label.length > 120) return json(res, 400, { error: { message: 'modal button label is required' } });
      const p = await getPage();
      const dialog = p.locator('[role="dialog"], [aria-modal="true"]').filter({ visible: true }).last();
      const button = dialog.getByRole('button', { name: label, exact: true }).first();
      await button.click({ timeout: 10000 });
      await p.waitForTimeout(250);
      await publishModal(p, new URL(p.url()).pathname.match(/^\/chat\/([^/]+)/)?.[1] || 'global');
      return json(res, 200, { status: 'clicked' });
    } catch (error) {
      return json(res, 502, { error: { message: `弹框操作失败: ${error.message}` } });
    }
  }
  const fileMatch = req.url.match(/^\/v1\/files\/([a-f0-9-]+)$/);
  if (req.method === "GET" && fileMatch) {
    let file = files.get(fileMatch[1]);
    // The download volume survives an API restart; recover the in-memory
    // index from the deterministic `<id>-<filename>` path when needed.
    if (!file) {
      const prefix = `${fileMatch[1]}-`;
      const names = await fs.promises.readdir(downloadDir).catch(() => []);
      const recovered = names.find(name => name.startsWith(prefix));
      if (recovered) file = { path: path.join(downloadDir, recovered), name: recovered.slice(prefix.length) };
    }
    if (!file) return json(res, 404, { error: { message: 'file_not_found' } });
    res.writeHead(200, { 'content-type': 'application/octet-stream', 'content-disposition': `attachment; filename="${file.name}"`, 'cache-control': 'private, max-age=3600' });
    return fs.createReadStream(file.path).pipe(res);
  }
  const openMatch = req.url.match(/^\/v1\/conversations\/([^/]+)\/open$/);
  if (req.method === "POST" && openMatch) {
    try {
      const p = await getPage();
      await p.goto(`https://claude.ai/chat/${openMatch[1]}`, { waitUntil: "domcontentloaded" }).catch(error => {
        // Claude can abort a navigation after the document has already
        // reached the requested route (redirects and SPA handoff). Treat only
        // that browser-level abort as a successful open; surface other errors.
        if (!String(error?.message || error).includes("ERR_ABORTED")) throw error;
      });
      return json(res, 200, { id: openMatch[1], status: "open" });
    } catch (error) {
      return json(res, 502, { error: { message: `打开对话失败: ${error.message}` } });
    }
  }
  if (req.method === "POST" && req.url === "/v1/chat/completions") {
    const startedAt = Date.now();
    try {
      const input = await readBody(req);
      const prompt = promptFromMessages(input.messages);
      if (!prompt) return json(res, 400, { error: { message: "messages is required", type: "invalid_request_error" } });
      const id = `chatcmpl_${crypto.randomUUID()}`;
      if (input.stream) {
        console.log(`chat start request_id=${requestId} id=${id} stream=true`);
        streamStart(res, id);
        let closed = false;
        let streamedChars = 0;
        const keepAlive = setInterval(() => { if (!closed && !res.destroyed) res.write(": keep-alive\n\n"); }, 5000);
        res.on("close", () => { closed = true; clearInterval(keepAlive); });
        try {
          let streamedText = '';
          const text = await enqueue(prompt, delta => {
            streamedText += delta;
            streamedChars += delta.length;
            if (!closed && !res.destroyed) res.write(`data: ${JSON.stringify({ id, object: 'chat.completion.chunk', created: Math.floor(Date.now() / 1000), model, choices: [{ index: 0, delta: { content: delta }, finish_reason: null }] })}\r\n\r\n`);
          });
          // Materialize links after the upstream answer is complete. The
          // answer text remains streamed; only the generated download block
          // is appended as one final delta so clients get a usable file URL.
          const enrichedText = await materializeLinks(text);
          const suffix = enrichedText.startsWith(text) ? enrichedText.slice(text.length) : '';
          if (!closed && !res.destroyed) {
            if (streamedChars === 0) await stream(res, completion(id, enrichedText));
            else {
              if (suffix) res.write(`data: ${JSON.stringify({ id, object: 'chat.completion.chunk', created: Math.floor(Date.now() / 1000), model, choices: [{ index: 0, delta: { content: suffix }, finish_reason: null }] })}\r\n\r\n`);
              res.write(`data: ${JSON.stringify({ id, object: 'chat.completion.chunk', created: Math.floor(Date.now() / 1000), model, choices: [{ index: 0, delta: {}, finish_reason: 'stop' }] })}\r\n\r\n`); res.write('data: [DONE]\r\n\r\n'); await new Promise(resolve => setTimeout(resolve, 500)); if (!res.destroyed) res.end();
            }
          }
        } finally {
          clearInterval(keepAlive);
          console.log(`chat end request_id=${requestId} id=${id} closed=${closed} duration_ms=${Date.now() - startedAt}`);
        }
        return;
      }
      console.log(`chat start request_id=${requestId} id=${id} stream=false`);
      const rawText = await enqueue(prompt);
      const text = await materializeLinks(rawText);
      const snapshot = await currentConversation();
      broadcastConversation(snapshot.id, { type: 'message.completed', conversation: snapshot });
      console.log(`chat end request_id=${requestId} id=${id} duration_ms=${Date.now() - startedAt}`);
      return json(res, 200, completion(id, text));
    } catch (error) {
      console.error(`chat failed duration_ms=${Date.now() - startedAt} error=${error.name}`);
      if (res.destroyed) return;
      if (res.headersSent) {
        res.write(`data: ${JSON.stringify({ error: { message: error.message, type: "upstream_error" } })}\n\n`);
        return res.end("data: [DONE]\n\n");
      }
      return json(res, 502, { error: { message: error.message, type: "upstream_error" } });
    }
  }
  return json(res, 404, { error: { message: "not_found", type: "invalid_request_error" } });
}
const server = tlsCert && tlsKey ? https.createServer({ cert: fs.readFileSync(tlsCert), key: fs.readFileSync(tlsKey) }, handler) : http.createServer(handler);
const wss = new WebSocketServer({ noServer: true });
server.on('upgrade', (req, socket, head) => {
  if (req.url !== '/v1/ws') return socket.destroy();
  wss.handleUpgrade(req, socket, head, ws => wss.emit('connection', ws));
});
wss.on('connection', ws => {
  websocketClients.add(ws);
  ws.on('message', async payload => {
    try {
      const event = JSON.parse(String(payload));
      if (event.type === 'auth') { ws.authenticated = event.token === token; if (!ws.authenticated) return ws.close(1008, 'unauthorized'); ws.send(JSON.stringify({ type: 'authenticated' })); }
      if (event.type === 'subscribe' && ws.authenticated) {
        ws.conversationId = event.conversationId;
        const snapshot = conversationSnapshots.get(event.conversationId);
        if (snapshot) ws.send(JSON.stringify({ type: 'conversation.snapshot', conversation: snapshot }));
      }
    } catch { ws.send(JSON.stringify({ type: 'error', message: 'invalid event' })); }
  });
  ws.on('close', () => websocketClients.delete(ws));
});
server.listen(port, host, () => console.log(`Claude Browser API listening on ${host}:${port} (${model})`));
