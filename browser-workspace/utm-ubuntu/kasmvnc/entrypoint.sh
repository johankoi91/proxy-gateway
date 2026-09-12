#!/bin/sh
set -eu

mkdir -p "$HOME/.vnc" "$HOME/.config/openbox" "$HOME/.config/fcitx5" "$HOME/Downloads"
install -m 0644 /opt/kasm-browser/kasmvnc.yaml "$HOME/.vnc/kasmvnc.yaml"
install -m 0755 /opt/kasm-browser/xstartup "$HOME/.vnc/xstartup"
install -m 0755 /opt/kasm-browser/openbox-autostart "$HOME/.config/openbox/autostart"
install -m 0644 /opt/kasm-browser/fcitx5-profile "$HOME/.config/fcitx5/profile"
install -m 0644 /opt/kasm-browser/fcitx5-config "$HOME/.config/fcitx5/config"
# The image supplies its own xstartup; skip KasmVNC's interactive DE chooser.
touch "$HOME/.vnc/.de-was-selected"
# A recreated container has no live Chromium process, so any singleton files
# persisted in the profile volume are stale.
rm -f "$HOME"/.config/chromium/SingletonCookie \
      "$HOME"/.config/chromium/SingletonLock \
      "$HOME"/.config/chromium/SingletonSocket
rm -f /tmp/.X1-lock /tmp/.X11-unix/X1

if [ ! -s "$HOME/.kasmpasswd" ]; then
  export KASM_USER KASM_PASSWORD
  /usr/bin/expect <<'EOF'
set timeout 15
spawn vncpasswd -u $env(KASM_USER) -w
expect -re {Password:}
send -- "$env(KASM_PASSWORD)\r"
expect -re {Verify:}
send -- "$env(KASM_PASSWORD)\r"
expect eof
catch wait result
exit [lindex $result 3]
EOF
fi

exec vncserver :1 \
  -fg \
  -geometry 1680x1050 \
  -depth 24 \
  -xstartup "$HOME/.vnc/xstartup"
