const fs = require('node:fs');
const { chromium } = require('playwright-core');
const redactHeaders = headers => Object.fromEntries(Object.entries(headers || {}).map(([key, value]) => /authorization|cookie|token|secret|api-key/i.test(key) ? [key, '[REDACTED]'] : [key, value]));
const safeUrl = value => { const url = new URL(value); return `${url.origin}${url.pathname}`; };
(async () => {
  const browser = await chromium.connectOverCDP(process.env.CDP_URL || 'http://127.0.0.1:9222');
  const page = browser.contexts()[0].pages().find(item => item.url().includes('claude.ai'));
  if (!page) throw new Error('Claude page not found');
  const events = [];
  const onRequest = request => { if (request.url().includes('claude.ai')) events.push({ time: new Date().toISOString(), phase: 'request', method: request.method(), url: safeUrl(request.url()), type: request.resourceType(), headers: redactHeaders(request.headers()) }); };
  const onResponse = response => { if (response.url().includes('claude.ai')) events.push({ time: new Date().toISOString(), phase: 'response', method: response.request().method(), url: safeUrl(response.url()), type: response.request().resourceType(), status: response.status(), headers: redactHeaders(response.headers()) }); };
  const onFinished = request => { if (request.url().includes('claude.ai')) events.push({ time: new Date().toISOString(), phase: 'finished', url: safeUrl(request.url()) }); };
  page.on('request', onRequest); page.on('response', onResponse); page.on('requestfinished', onFinished); page.on('requestfailed', onFinished);
  await page.reload({ waitUntil: 'domcontentloaded' }); await page.waitForTimeout(5000);
  page.off('request', onRequest); page.off('response', onResponse); page.off('requestfinished', onFinished); page.off('requestfailed', onFinished);
  fs.writeFileSync('/tmp/claude-network-trace.json', JSON.stringify({ capturedAt: new Date().toISOString(), page: page.url(), events }, null, 2));
  console.log(JSON.stringify({ page: page.url(), events: events.length, paths: [...new Set(events.map(event => event.url))] }));
  await browser.close();
})().catch(error => { console.error(error); process.exit(1); });
