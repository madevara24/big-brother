#!/usr/bin/env bash
# Idempotent installer for vpswatch: builds the binary, drops it (plus a
# seed .env) into place, writes the systemd unit + timer, and reloads
# systemd so the timer picks up any changes. Safe to re-run.
#
# Needs root, to install a systemd unit and create the service user --
# run as:
#
#   sudo ./install.sh
#
# Override INSTALL_DIR / SYSTEMD_DIR / SERVICE_USER to install elsewhere.
# Set SKIP_USERADD=1 / SKIP_SYSTEMCTL=1 and unprivileged INSTALL_DIR /
# SYSTEMD_DIR paths to exercise this script's file-layout logic without
# root or a real systemd, e.g. in a scratch dir:
#
#   INSTALL_DIR=/tmp/vpswatch-scratch SYSTEMD_DIR=/tmp/vpswatch-scratch/systemd \
#     SKIP_USERADD=1 SKIP_SYSTEMCTL=1 ./install.sh
#
# Set DRY_RUN=1 on top of that to print the privileged steps (systemctl,
# useradd) without running them, while still exercising the actual file
# writes below.

set -euo pipefail

BINARY="vpswatch"
INSTALL_DIR="${INSTALL_DIR:-/opt/vpswatch}"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
SERVICE_USER="${SERVICE_USER:-vpswatch}"
DRY_RUN="${DRY_RUN:-0}"
SKIP_USERADD="${SKIP_USERADD:-0}"
SKIP_SYSTEMCTL="${SKIP_SYSTEMCTL:-0}"

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

run() {
	if [ "$DRY_RUN" = "1" ]; then
		echo "[dry-run] $*"
	else
		"$@"
	fi
}

echo "install: building $BINARY"
( cd "$repo_dir" && go build -o "$BINARY" ./cmd/vpswatch )

if [ "$SKIP_USERADD" != "1" ]; then
	if ! id "$SERVICE_USER" >/dev/null 2>&1; then
		echo "install: creating system user $SERVICE_USER"
		run useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
	else
		echo "install: user $SERVICE_USER already exists"
	fi
fi

echo "install: creating $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"

echo "install: installing binary into $INSTALL_DIR"
cp "$repo_dir/$BINARY" "$INSTALL_DIR/.$BINARY.new"
mv "$INSTALL_DIR/.$BINARY.new" "$INSTALL_DIR/$BINARY"

if [ -f "$INSTALL_DIR/.env" ]; then
	echo "install: $INSTALL_DIR/.env already exists, leaving it alone"
else
	echo "install: seeding $INSTALL_DIR/.env from .env.example -- fill in the real URLs before starting the timer"
	cp "$repo_dir/.env.example" "$INSTALL_DIR/.env"
	chmod 600 "$INSTALL_DIR/.env"
fi

if [ "$SKIP_USERADD" != "1" ]; then
	run chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"
fi

echo "install: writing systemd units to $SYSTEMD_DIR"
mkdir -p "$SYSTEMD_DIR"
sed -e "s#/opt/vpswatch#$INSTALL_DIR#g" \
	-e "s/^User=vpswatch/User=$SERVICE_USER/" \
	-e "s/^Group=vpswatch/Group=$SERVICE_USER/" \
	"$repo_dir/systemd/vpswatch.service" > "$SYSTEMD_DIR/vpswatch.service"
cp "$repo_dir/systemd/vpswatch.timer" "$SYSTEMD_DIR/vpswatch.timer"

if [ "$SKIP_SYSTEMCTL" != "1" ]; then
	echo "install: reloading systemd and enabling the timer"
	run systemctl daemon-reload
	run systemctl enable --now vpswatch.timer
else
	echo "install: SKIP_SYSTEMCTL=1, not touching systemd -- reload/enable it yourself:"
	echo "  systemctl daemon-reload && systemctl enable --now vpswatch.timer"
fi

echo "install: done"
