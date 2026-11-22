#!/bin/sh
# Applies tc netem rules when NETEM_* env vars are set, then execs the app.
# Needs: cap_add NET_ADMIN and running as root (see the `netem` compose
# service and k8s securityContext notes). Without the capability it logs a
# warning and continues — the app still serves, just without packet-level
# delay/loss.
set -e

IFACE="${NETEM_IFACE:-eth0}"

if [ -n "$NETEM_DELAY" ] || [ -n "$NETEM_LOSS" ]; then
    RULE="qdisc add dev $IFACE root netem"
    [ -n "$NETEM_DELAY" ] && RULE="$RULE delay $NETEM_DELAY ${NETEM_JITTER:-0ms}"
    [ -n "$NETEM_LOSS" ]  && RULE="$RULE loss $NETEM_LOSS"
    [ -n "$NETEM_CORRUPT" ] && RULE="$RULE corrupt $NETEM_CORRUPT"
    if tc $RULE 2>/dev/null; then
        echo "[entrypoint] netem applied on $IFACE: delay=${NETEM_DELAY:-none} loss=${NETEM_LOSS:-none}"
    else
        echo "[entrypoint] WARNING: tc failed (need root + NET_ADMIN); continuing without netem" >&2
    fi
fi

exec chaos-target
