(() => {
  const PANEL_ID = "lightnode-text-input-panel";
  const BUTTON_ID = "lightnode-text-input-button";

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

  function mount() {
    if (document.getElementById(BUTTON_ID)) return true;
    if (!document.getElementById("overlayInput")) return false;

    const button = document.createElement("button");
    button.id = BUTTON_ID;
    button.type = "button";
    button.textContent = "文字输入";
    button.title = "先在本地完成中文输入，再整段粘贴到远端当前输入框";

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

    Object.assign(button.style, {
      position: "fixed", right: "76px", bottom: "22px", zIndex: "2147483647",
      height: "38px", padding: "0 14px", border: "1px solid rgba(255,255,255,.28)",
      borderRadius: "6px", color: "#fff", background: "rgba(24,28,36,.94)",
      font: "600 14px system-ui,sans-serif", cursor: "pointer",
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
    document.body.append(panel, button);
    return true;
  }

  if (!mount()) {
    const observer = new MutationObserver(() => {
      if (mount()) observer.disconnect();
    });
    observer.observe(document.documentElement, { childList: true, subtree: true });
  }
})();
