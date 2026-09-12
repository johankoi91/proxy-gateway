FROM ghcr.io/selkies-project/selkies/desktop@sha256:c958a33d22074456fd99507d71cb0aa9a155eb5d71d26da1040ae4fb126043e0

USER 0
RUN sed -i 's/^# *\(zh_CN.UTF-8 UTF-8\)/\1/' /etc/locale.gen \
    && locale-gen

COPY chrome-autostart.desktop /etc/xdg/autostart/
RUN chmod 0644 /etc/xdg/autostart/chrome-autostart.desktop
USER 1000
