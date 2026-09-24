# Build/deploy helpers for vpswatch. See README.md for the underlying
# `go build` command and the config directory layout (.env, state.json,
# samples.log).
#
# DEPLOY_DIR is the live config directory on the VPS -- it holds .env,
# state.json, and samples.log, and is separate from this dev clone.
# Override it to deploy somewhere else, e.g.:
#
#   make deploy DEPLOY_DIR=/path/to/other/dir
#
# Unlike pmrunner (a long-lived daemon started by hand with nohup),
# vpswatch is a short-lived process invoked once per tick by
# systemd/vpswatch.timer -- there's no PID to find and kill before
# swapping the binary. `deploy` just installs the new binary and reloads
# the systemd unit so the next tick picks it up.
#
# Set DRY_RUN=1 to print what `deploy` would do without doing it.

BINARY := vpswatch
DEPLOY_DIR ?= /opt/vpswatch
DRY_RUN ?= 0

.PHONY: build deploy

build:
	go build -o $(BINARY) ./cmd/vpswatch

deploy: build
	@deploy_dir=$$(readlink -f "$(DEPLOY_DIR)"); \
	if [ ! -d "$$deploy_dir" ]; then \
		echo "deploy: $$deploy_dir does not exist" >&2; \
		exit 1; \
	fi; \
	echo "deploy: installing $(BINARY) into $$deploy_dir"; \
	if [ "$(DRY_RUN)" = "1" ]; then \
		echo "[dry-run] cp $(BINARY) $$deploy_dir/.$(BINARY).new && mv $$deploy_dir/.$(BINARY).new $$deploy_dir/$(BINARY)"; \
		echo "[dry-run] systemctl daemon-reload"; \
		echo "[dry-run] systemctl restart $(BINARY).timer"; \
	else \
		cp $(BINARY) "$$deploy_dir/.$(BINARY).new" && \
		mv "$$deploy_dir/.$(BINARY).new" "$$deploy_dir/$(BINARY)"; \
		systemctl daemon-reload; \
		systemctl restart $(BINARY).timer; \
	fi
