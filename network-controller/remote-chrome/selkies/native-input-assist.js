(() => {
  const PANEL_ID = "lightnode-text-input-panel";
  const BUTTON_ID = "lightnode-text-input-button";
  const COPY_ID = "lightnode-copy-local-button";
  let latestRemoteText = "";
  let latestRemoteTruncated = false;

  function sendRemoteKey(type, key, code, keyCode, ctrlKey) {
    const event = new KeyboardEvent(type, {
      key,
      code,
      keyCode,
      which: keyCode,
      ctrlKey,
      bubbles: true,
      cancelable: true,
    });
    Object.defineProperty(event, "__selkiesClipReplay", { value: true });
    window.dispatchEvent(event);
  }

  function pasteRemote() {
    sendRemoteKey("keydown", "Control", "ControlLeft", 17, true);
    sendRemoteKey("keydown", "v", "KeyV", 86, true);
    sendRemoteKey("keyup", "v", "KeyV", 86, true);
    sendRemoteKey("keyup", "Control", "ControlLeft", 17, false);
  }

  async function waitForClipboardReady() {
    const deadline = Date.now() + 10_000;
    while (Date.now() < deadline) {
      if (window.clipboard_enabled === true &&
          typeof window.lightnodeSendClipboardText === "function") return;
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
    throw new Error("clipboard channel is not ready");
  }

  async function writeLocalClipboard(text) {
    try {
      if (window.isSecureContext && navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
        return;
      }
    } catch (error) {
      console.warn("Clipboard API write failed; trying the document fallback", error);
    }
    const fallback = document.createElement("textarea");
    fallback.value = text;
    Object.assign(fallback.style, { position: "fixed", left: "-9999px", top: "-9999px" });
    document.body.appendChild(fallback);
    fallback.focus();
    fallback.select();
    const copied = document.execCommand("copy");
    fallback.remove();
    if (!copied) throw new Error("browser denied clipboard write");
  }

  function mount() {
    if (document.getElementById(BUTTON_ID)) return true;
    if (!document.getElementById("overlayInput")) return false;

    const button = document.createElement("button");
    button.id = BUTTON_ID;
    button.type = "button";
    button.textContent = "文字输入";
    button.title = "先在本地完成中文输入，再整段粘贴到远端当前输入框";

    const copyLocal = document.createElement("button");
    copyLocal.id = COPY_ID;
    copyLocal.type = "button";
    copyLocal.hidden = true;
    copyLocal.textContent = "复制到本机";
    copyLocal.title = "把最近一次远端剪贴板文本写入本机剪贴板";

    const copyStatus = document.createElement("span");
    copyStatus.id = "lightnode-copy-local-button-status";
    copyStatus.hidden = true;

    const panel = document.createElement("section");
    panel.id = PANEL_ID;
    panel.hidden = true;

    const textarea = document.createElement("textarea");
    textarea.className = "allow-native-input";
    textarea.rows = 8;
    textarea.placeholder = "在这里输入或粘贴完整内容。编辑期间不会操作远端。";
    textarea.setAttribute("aria-label", "待粘贴到远端的文字");

    const status = document.createElement("span");
    const paste = document.createElement("button");
    paste.type = "button";
    paste.textContent = "粘贴到远端";
    const close = document.createElement("button");
    close.type = "button";
    close.textContent = "关闭";
    const actions = document.createElement("div");
    actions.append(status, close, paste);
    panel.append(textarea, actions);

    const setBusy = (busy, message = "") => {
      paste.disabled = busy;
      close.disabled = busy;
      textarea.disabled = busy;
      status.textContent = message;
    };
    const hide = () => {
      panel.hidden = true;
      button.textContent = "文字输入";
    };
    const show = () => {
      panel.hidden = false;
      button.textContent = "关闭输入";
      textarea.focus();
    };

    button.addEventListener("click", () => panel.hidden ? show() : hide());
    close.addEventListener("click", hide);
    textarea.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && !event.isComposing) {
        event.preventDefault();
        event.stopPropagation();
        hide();
      }
    });
    paste.addEventListener("click", async () => {
      const text = textarea.value;
      if (!text) {
        status.textContent = "请输入内容";
        textarea.focus();
        return;
      }
      setBusy(true, `正在同步 ${new TextEncoder().encode(text).length} 字节...`);
      try {
        await waitForClipboardReady();
        await window.lightnodeSendClipboardText(text);
        hide();
        document.getElementById("overlayInput")?.focus();
        await new Promise((resolve) => setTimeout(resolve, 80));
        pasteRemote();
        textarea.value = "";
        setBusy(false, "已粘贴");
      } catch (error) {
        setBusy(false, "同步失败，请重试");
        console.error("LightNode text paste failed", error);
      }
    });
    copyLocal.addEventListener("click", async () => {
      if (!latestRemoteText) return;
      copyLocal.disabled = true;
      try {
        await writeLocalClipboard(latestRemoteText);
        copyStatus.hidden = false;
        copyStatus.textContent = latestRemoteTruncated ? "已复制（远端内容已截断）" : "已复制到本机剪贴板";
      } catch (error) {
        copyStatus.hidden = false;
        copyStatus.textContent = "复制失败，请重新点击或手动选择文本";
        console.error("LightNode local clipboard write failed", error);
      } finally {
        copyLocal.disabled = false;
      }
    });

    Object.assign(button.style, {
      position: "fixed", right: "76px", bottom: "22px", zIndex: "2147483647",
      height: "38px", padding: "0 14px", border: "1px solid rgba(255,255,255,.28)",
      borderRadius: "6px", color: "#fff", background: "rgba(24,28,36,.94)",
      font: "600 14px system-ui,sans-serif", cursor: "pointer",
    });
    Object.assign(copyLocal.style, {
      position: "fixed", right: "76px", bottom: "68px", zIndex: "2147483647",
      height: "36px", padding: "0 12px", border: "1px solid #16a34a",
      borderRadius: "6px", color: "#fff", background: "rgba(22,101,52,.96)",
      font: "600 14px system-ui,sans-serif", cursor: "pointer",
    });
    Object.assign(copyStatus.style, {
      position: "fixed", right: "76px", bottom: "108px", zIndex: "2147483647",
      padding: "6px 10px", borderRadius: "5px", color: "#fff",
      background: "rgba(24,28,36,.94)", font: "13px system-ui,sans-serif",
    });
    Object.assign(panel.style, {
      position: "fixed", left: "50%", bottom: "70px", transform: "translateX(-50%)",
      zIndex: "2147483647", width: "min(760px, calc(100vw - 220px))", padding: "12px",
      border: "1px solid rgba(255,255,255,.28)", borderRadius: "8px",
      background: "rgba(24,28,36,.98)", boxShadow: "0 12px 36px rgba(0,0,0,.42)",
    });
    Object.assign(textarea.style, {
      display: "block", width: "100%", minHeight: "160px", boxSizing: "border-box",
      resize: "vertical", padding: "12px", border: "1px solid #64748b", borderRadius: "6px",
      outline: "none", color: "#111827", background: "#fff", caretColor: "#111827",
      font: "16px/1.55 system-ui,sans-serif",
    });
    Object.assign(actions.style, {
      display: "flex", alignItems: "center", justifyContent: "flex-end",
      gap: "8px", marginTop: "10px",
    });
    Object.assign(status.style, {
      marginRight: "auto", color: "#cbd5e1", font: "13px system-ui,sans-serif",
    });
    for (const action of [close, paste]) {
      Object.assign(action.style, {
        height: "36px", padding: "0 14px", border: "1px solid #64748b",
        borderRadius: "6px", cursor: "pointer",
      });
    }
    Object.assign(paste.style, { borderColor: "#2563eb", color: "#fff", background: "#2563eb" });
    document.body.append(panel, button, copyLocal, copyStatus);
    return true;
  }

  window.addEventListener("message", (event) => {
    if (event.origin !== window.location.origin) return;
    const data = event.data;
    if (!data || data.type !== "clipboardContentUpdate" || typeof data.text !== "string") return;
    latestRemoteText = data.text;
    latestRemoteTruncated = data.truncated === true;
    const copyButton = document.getElementById(COPY_ID);
    const copyMessage = document.getElementById("lightnode-copy-local-button-status");
    if (copyButton) {
      copyButton.hidden = latestRemoteText.length === 0;
      copyButton.title = latestRemoteTruncated
        ? "远端文本超过客户端显示上限，请使用文件传输或分段复制"
        : "把最近一次远端剪贴板文本写入本机剪贴板";
    }
    if (copyMessage) copyMessage.hidden = true;
  });

  if (!mount()) {
    const observer = new MutationObserver(() => {
      if (mount()) observer.disconnect();
    });
    observer.observe(document.documentElement, { childList: true, subtree: true });
  }
})();
