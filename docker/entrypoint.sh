#!/bin/sh
set -eu
mkdir -p /data
chown operator:nogroup /data
exec runuser -u operator -- /usr/local/bin/operator
