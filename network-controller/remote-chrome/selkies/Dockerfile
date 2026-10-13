FROM ghcr.io/selkies-project/selkies/desktop@sha256:c958a33d22074456fd99507d71cb0aa9a155eb5d71d26da1040ae4fb126043e0

USER 0
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      fcitx5 \
      fcitx5-chinese-addons \
      fcitx5-frontend-gtk3 \
    && find /var/lib/apt/lists -mindepth 1 -delete \
    && sed -i 's/^# *\(zh_CN.UTF-8 UTF-8\)/\1/' /etc/locale.gen \
    && locale-gen \
    && chown root:root /opt/google/chrome/chrome-sandbox \
    && chmod 4755 /opt/google/chrome/chrome-sandbox \
    && sed -i 's/ --no-sandbox --password-store=basic/ --password-store=basic/' \
        /opt/google/chrome/google-chrome \
    && ! grep -q -- '--no-sandbox' /opt/google/chrome/google-chrome

COPY chrome-autostart.desktop /etc/xdg/autostart/
COPY fcitx5-autostart.desktop /etc/xdg/autostart/
COPY lightnode-fcitx5-start /usr/local/bin/
COPY native-input-assist.js /usr/local/lib/python3.14/dist-packages/selkies/selkies_web/
COPY patch-selkies-composition.sh /usr/local/bin/
RUN chmod 0644 /etc/xdg/autostart/chrome-autostart.desktop /etc/xdg/autostart/fcitx5-autostart.desktop \
    && chmod 0644 /usr/local/lib/python3.14/dist-packages/selkies/selkies_web/native-input-assist.js \
    && chmod 0755 /usr/local/bin/lightnode-fcitx5-start /usr/local/bin/patch-selkies-composition.sh \
    && /usr/local/bin/patch-selkies-composition.sh \
    && rm /usr/local/bin/patch-selkies-composition.sh \
    && sed -i 's#</body>#<script src="./native-input-assist.js"></script></body>#' \
      /usr/local/lib/python3.14/dist-packages/selkies/selkies_web/index.html \
    && grep -q 'native-input-assist.js' \
      /usr/local/lib/python3.14/dist-packages/selkies/selkies_web/index.html
USER 1000
