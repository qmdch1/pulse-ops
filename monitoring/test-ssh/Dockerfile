FROM alpine:3.23
RUN apk add --no-cache openssh bash procps && adduser -D -s /bin/bash pulse && echo 'pulse:pulse_isolated_test_only' | chpasswd && mkdir -p /home/pulse/.ssh /run/sshd && chown -R pulse:pulse /home/pulse
COPY start.sh /start.sh
RUN chmod 755 /start.sh
EXPOSE 22
ENTRYPOINT ["/start.sh"]
