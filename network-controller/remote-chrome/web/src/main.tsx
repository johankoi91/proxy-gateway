import React, { useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Button, Input, Tooltip, Avatar, Dropdown, Modal, message as toast } from 'antd';
import { PlusOutlined, SearchOutlined, SettingOutlined, PaperClipOutlined, ArrowUpOutlined, StopOutlined, CopyOutlined, ReloadOutlined, UserOutlined, MenuFoldOutlined, MenuUnfoldOutlined } from '@ant-design/icons';
import 'antd/dist/reset.css';
import './styles.css';

type Message = { id: string; role: 'user' | 'assistant'; content: string; loading?: boolean; createdAt?: string };
type ModalState = { id: string; kind: string; title: string; content: string; buttons: { label: string }[]; blocking: boolean };
type Conversation = { id: string; title: string; messages: Message[]; updated: string; modal?: ModalState | null };
const API = import.meta.env.VITE_CLAUDE_API || '/api';
const makeId = () => globalThis.crypto?.randomUUID?.() || `msg_${Date.now()}_${Math.random().toString(36).slice(2)}`;
const formatMessageTime = (value?: string, fallback?: string) => {
  const date = new Date(value || fallback || Date.now());
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
};
async function downloadProtectedFile(url: string, label: string, token: string) {
  try {
    const response = await fetch(url, { headers: { Authorization: `Bearer ${token}` } });
    if (!response.ok) throw new Error(`下载失败（${response.status}）`);
    const blob = await response.blob();
    const objectUrl = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = objectUrl;
    anchor.download = label || 'download';
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(objectUrl), 1000);
  } catch (error) {
    toast.error(error instanceof Error ? error.message : '文件下载失败');
  }
}
function renderContent(content: string, token: string) {
  return content.split(/(\[[^\]]+\]\([^\)]+\))/g).map((part, index) => {
    const match = part.match(/^\[([^\]]+)\]\(([^\)]+)\)$/);
    if (!match) return <React.Fragment key={index}>{part}</React.Fragment>;
    const protectedFile = match[2].startsWith('/api/files/');
    return <a key={index} href={match[2]} target={protectedFile ? undefined : '_blank'} rel="noreferrer" className="download-link" onClick={protectedFile ? event => { event.preventDefault(); void downloadProtectedFile(match[2], match[1], token); } : undefined}>{match[1]}</a>;
  });
}

function App() {
  const [token, setToken] = useState(localStorage.getItem('claude_browser_token') || '');
  const [configured, setConfigured] = useState(Boolean(token));
  const [collapsed, setCollapsed] = useState(false);
  const [draft, setDraft] = useState('');
  const [sending, setSending] = useState(false);
  const [active, setActive] = useState('new');
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [modal, setModal] = useState<ModalState | null>(null);
  const current = conversations.find(c => c.id === active);
  const canSend = draft.trim().length > 0 && !sending && configured;
  const sorted = useMemo(() => [...conversations].sort((a, b) => b.updated.localeCompare(a.updated)), [conversations]);

  async function syncCurrent(previousId?: string) {
    const response = await fetch(`${API}/conversations/current`, { headers: { Authorization: `Bearer ${token}` } });
    const item = await response.json();
    if (!response.ok) throw new Error(item.error?.message || `读取对话失败 ${response.status}`);
    setConversations(items => {
      const local = items.find(c => c.id === item.id || c.id === previousId);
      const messages = local && item.messages?.length ? item.messages.map((remote: Message, index: number) => {
        const localMessage = local.messages[index];
        // Keep locally materialized download links and an in-flight assistant
        // result when the remote DOM snapshot has not caught up yet.
        if (localMessage && ((localMessage.content.includes('/api/files/') && !remote.content.includes('/api/files/')) || (localMessage.loading && !remote.content))) return localMessage;
        return remote;
      }) : local?.messages || item.messages || [];
      const merged = local ? { ...item, id: local.id, title: local.title || item.title, messages, updated: local.updated || item.updated } : item;
      return [merged, ...items.filter(c => c.id !== item.id && c.id !== previousId)];
    });
    setActive(item.id);
    setModal(item.modal || null);
    return item;
  }
  useEffect(() => {
    if (!configured) return;
    let cancelled = false;
    (async () => {
      const response = await fetch(`${API}/conversations`, { headers: { Authorization: `Bearer ${token}` } });
      const value = await response.json();
      if (!response.ok) throw new Error(value.error?.message || '读取历史失败');
      if (cancelled) return;
      if (Array.isArray(value.data)) setConversations(value.data.map((item: any) => ({ ...item, messages: [], updated: '' })));
      await syncCurrent();
    })().catch(error => toast.error(error.message));
    return () => { cancelled = true; };
  }, [configured, token]);

  useEffect(() => {
    if (!configured) return;
    const socket = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/ws`);
    socket.onopen = () => { socket.send(JSON.stringify({ type: 'auth', token })); if (active !== 'new') socket.send(JSON.stringify({ type: 'subscribe', conversationId: active })); };
    socket.onmessage = event => { try { const value = JSON.parse(event.data); if (value.type === 'ui.modal') { setModal(value.modal || null); return; } if (value.type === 'message.completed' || value.type === 'conversation.snapshot') { const item = value.conversation; if (item?.id) { setConversations(items => [item, ...items.filter(c => c.id !== item.id)]); setActive(currentId => currentId === 'new' ? item.id : currentId); setModal(item.modal || null); } } } catch { /* ignore malformed events */ } };
    return () => socket.close();
  }, [configured, token, active]);

  function saveToken() { const value = token.trim(); if (!value) return; localStorage.setItem('claude_browser_token', value); setConfigured(true); toast.success('已保存连接凭据'); }
  async function newChat() { try { const response = await fetch(`${API}/conversations`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } }); if (!response.ok) throw new Error(`接口错误 ${response.status}`); const item = await response.json(); const chat = { id: item.id, title: item.title || '新对话', messages: [], updated: new Date().toISOString() }; setConversations(c => [chat, ...c.filter(x => x.id !== chat.id)]); setActive(chat.id); setDraft(''); } catch (error) { toast.error(`新建对话失败：${error instanceof Error ? error.message : '未知错误'}`); } }
  async function actOnModal(label: string) { try { const response = await fetch(`${API}/ui/modal/action`, { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ label }) }); const value = await response.json().catch(() => ({})); if (!response.ok) throw new Error(value.error?.message || `弹框操作失败 ${response.status}`); setModal(null); } catch (error) { toast.error(error instanceof Error ? error.message : '弹框操作失败'); } }
  async function send() {
    console.log('[claude-web] send invoked', { configured, sending, draftLength: draft.trim().length, active, conversationCount: conversations.length });
    if (!configured || sending || !draft.trim()) return;
    let chat = current;
    if (!chat) { try { const response = await fetch(`${API}/conversations`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } }); const item = await response.json(); if (!response.ok) throw new Error(item.error?.message || `新建对话失败 ${response.status}`); chat = { id: item.id, title: item.title || '新对话', messages: [], updated: new Date().toISOString() }; setActive(chat.id); setConversations(c => [{ ...chat! }, ...c]); } catch (error) { toast.error(error instanceof Error ? error.message : '新建对话失败'); return; } }
    const chatId = chat.id;
    const text = draft.trim(); setDraft('');
    const sentAt = new Date().toISOString();
    const user: Message = { id: makeId(), role: 'user', content: text, createdAt: sentAt };
    const assistant: Message = { id: makeId(), role: 'assistant', content: '', loading: true, createdAt: sentAt };
    setConversations(c => c.map(item => item.id === chatId ? { ...item, title: item.messages.length ? item.title : text.slice(0, 28), messages: [...item.messages, user, assistant], updated: new Date().toISOString() } : item));
    setSending(true);
    try {
      const response = await fetch(`${API}/chat/completions`, { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ model: 'claude-browser', messages: [{ role: 'user', content: text }], stream: false }) });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(result.error?.message || `接口错误 ${response.status}`);
      if (result.error) throw new Error(result.error.message || '远程回复失败');
      const received = result.choices?.[0]?.message?.content || '';
      if (!received.trim()) throw new Error('接口返回为空');
      const completedAt = new Date().toISOString();
      setConversations(c => c.map(item => item.id === chatId ? { ...item, messages: item.messages.map(m => m.id === assistant.id ? { ...m, content: received, loading: false, createdAt: completedAt } : m), updated: completedAt } : item));
      try { await syncCurrent(chatId); } catch (error) { toast.warning(`回复已收到，但同步失败：${error instanceof Error ? error.message : '未知错误'}`); }
    } catch (error) {
      const message = error instanceof Error ? error.message : '读取回复失败';
      setConversations(c => c.map(item => item.id === chatId ? { ...item, messages: item.messages.map(m => m.id === assistant.id ? { ...m, content: `发送失败：${message}`, loading: false } : m) } : item));
      toast.error(message);
    } finally { setSending(false); }
  }
  return <div className="shell">
    <aside className={`sidebar ${collapsed ? 'collapsed' : ''}`}>
      <div className="side-top"><div className="logo">✦</div>{!collapsed && <span className="brand">Claude Browser</span>}<Button type="text" icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />} onClick={() => setCollapsed(!collapsed)} /></div>
      <Button className="new-chat" type="primary" icon={<PlusOutlined />} disabled={sending} onClick={newChat}>{!collapsed && '新建对话'}</Button>
      {!collapsed && <Input className="search" prefix={<SearchOutlined />} placeholder="搜索对话" />}
      {!collapsed && <div className="history">{sorted.map(item => <button disabled={sending} className={`history-item ${active === item.id ? 'active' : ''}`} key={item.id} onClick={async () => { try { const response = await fetch(`${API}/conversations/${item.id}/open`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } }); if (!response.ok) throw new Error('打开对话失败'); await syncCurrent(); } catch (error) { toast.error(error instanceof Error ? error.message : '读取对话失败'); } }}><span>{item.title}</span><small>{item.updated.slice(5, 10)}</small></button>)}</div>}
      <div className="side-bottom">{!collapsed && <div className="connection"><i /> 东京线路 · 已连接</div>}<Dropdown menu={{ items: [{ key: 'settings', label: '连接设置', icon: <SettingOutlined /> }] }}><Button type="text" icon={<SettingOutlined />} /></Dropdown><Avatar size={30} icon={<UserOutlined />} /></div>
    </aside>
    <main className="main">
      <header className="header"><div className="model"><span className="model-dot" /> Claude Browser <span className="chevron">⌄</span></div><div className="header-actions"><Tooltip title="当前使用固定 Claude Web 对话"><span className="secure">东京出口</span></Tooltip><Button type="text" icon={<SettingOutlined />} /></div></header>
      <Modal open={Boolean(modal)} title={modal?.title || 'Claude 提示'} footer={modal?.buttons.map(button => <Button key={button.label} type={/登录|验证|继续|确认|下载/i.test(button.label) ? 'primary' : 'default'} onClick={() => void actOnModal(button.label)}>{button.label}</Button>)} closable={!modal?.blocking} onCancel={() => setModal(null)} maskClosable={false}><div className="remote-modal-content">{modal?.content}</div>{modal?.kind === 'security_verification' && <div className="remote-modal-hint">这是 Claude 的安全验证，请在远程 Chrome 中完成，不会自动绕过。</div>}</Modal>
      {!configured ? <section className="setup"><div className="setup-icon">✦</div><h1>连接 Claude Browser</h1><p>使用北京 VPS 上的浏览器会话，让任意设备访问你的 Claude Web 对话。</p><Input.Password value={token} onChange={e => setToken(e.target.value)} placeholder="粘贴 API Token" onPressEnter={saveToken} /><Button type="primary" size="large" onClick={saveToken}>保存并开始</Button><small>Token 只保存在本机浏览器 Local Storage。</small></section> : current?.messages.length ? <div className="conversation"><div className="messages">{current.messages.map(m => <article className={`message ${m.role}`} key={m.id}><Avatar className="message-avatar" size={32} icon={m.role === 'assistant' ? '✦' : <UserOutlined />} /><div className="bubble"><div className="message-meta">{m.role === 'assistant' ? 'Claude' : '你'} · {formatMessageTime(m.createdAt, current.updated)}</div>{m.loading && !m.content ? <span className="typing"><i /><i /><i /></span> : <div className="markdown">{renderContent(m.content, token)}</div>}{m.role === 'assistant' && !m.loading && <div className="message-tools"><Button type="text" size="small" icon={<CopyOutlined />} onClick={() => navigator.clipboard.writeText(m.content)} /><Button type="text" size="small" icon={<ReloadOutlined />} /></div>}</div></article>)}</div></div> : <div className="welcome"><div className="welcome-mark">✦</div><h1>今天想聊点什么？</h1><p>你的 Claude Web 对话将通过东京出口发送。</p><div className="suggestions"><button onClick={() => setDraft('帮我梳理一下这个问题的关键点')}>梳理一个复杂问题 <span>→</span></button><button onClick={() => setDraft('帮我提出几个可行的方案')}>提出几个可行方案 <span>→</span></button><button onClick={() => setDraft('帮我把这段文字改得更清晰')}>改写一段文字 <span>→</span></button></div></div>}
      <div className="composer-wrap"><form className="composer" onSubmit={e => { e.preventDefault(); void send(); }}><Input.TextArea value={draft} onChange={e => setDraft(e.target.value)} onPressEnter={e => { if (!e.shiftKey) { e.preventDefault(); void send(); } }} placeholder="给 Claude 发消息…" autoSize={{ minRows: 1, maxRows: 8 }} disabled={!configured || sending} /><div className="composer-bar"><Button type="text" icon={<PaperClipOutlined />} disabled={!configured} /><span>Enter 发送 · Shift + Enter 换行</span>{sending ? <Button danger shape="circle" icon={<StopOutlined />} htmlType="button" /> : <Button type="primary" shape="circle" icon={<ArrowUpOutlined />} disabled={!configured || sending || !draft.trim()} htmlType="button" onMouseDown={e => { e.preventDefault(); void send(); }} onClick={e => { e.preventDefault(); void send(); }} />}</div></form><small className="disclaimer">Claude Browser 可能会犯错，请核对重要信息。</small></div>
    </main>
  </div>;
}
createRoot(document.getElementById('root')!).render(<App />);
