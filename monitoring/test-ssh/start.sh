#!/bin/sh
set -eu
umask 077
if [ "$HOST_ID" = bastion ] && [ ! -f /keys/client_key ]; then
  ssh-keygen -q -t ed25519 -N '' -f /keys/client_key
  chown 10001:10001 /keys/client_key
fi
while [ ! -f /keys/client_key.pub ]; do sleep 1; done
if [ ! -f "/keys/$HOST_ID.host" ]; then ssh-keygen -q -t ed25519 -N '' -f "/keys/$HOST_ID.host"; fi
ssh-keygen -lf "/keys/$HOST_ID.host.pub" | awk '{print $2}' > "/keys/$HOST_ID.fingerprint"
chmod 644 "/keys/$HOST_ID.fingerprint"
cp /keys/client_key.pub /home/pulse/.ssh/authorized_keys
chown -R pulse:pulse /home/pulse/.ssh
exec /usr/sbin/sshd -D -e -h "/keys/$HOST_ID.host" -o PasswordAuthentication=yes -o PermitRootLogin=no -o AllowUsers=pulse -o AllowTcpForwarding=yes -o PrintMotd=no
