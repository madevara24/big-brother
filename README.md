# big-brother

`vpswatch` is a standalone Go binary that watches a Linux VPS's own
resources -- CPU load, memory, swap, disk, I/O wait -- heartbeats to
[healthchecks.io](https://healthchecks.io), and alerts to a Discord
webhook. Go stdlib only, one static binary, no database: it keeps a
rolling hour of samples on disk for its own hourly digest and nothing
more (sysstat already keeps real long-term history on most VPSes).

## What it does

Each tick (once a minute, via `systemd/vpswatch.timer`) vpswatch runs,
does its work, and exits -- it is not a long-lived daemon. Four signals
come out of that:

1. **Heartbeat** -- a ping to your healthchecks.io check URL, every tick,
   unconditionally, even if a metric is over threshold. If the pings
   stop, healthchecks.io raises the alarm itself: that's the
   whole-box-is-dead signal, and it must never be confused with
   "something's degraded." Set the check's grace period in
   healthchecks.io to a few times `VPSWATCH_TICK`.
2. **Metric alerts** -- a Discord message only after a metric has been
   bad for `VPSWATCH_DEBOUNCE` consecutive ticks (5 minutes, at the
   default 1-minute tick), then one recovery message the tick it clears.
   Never one message per check.
3. **Hourly digest** -- a Discord table of min/avg/max/p95 per metric
   over the last digest period, once per `VPSWATCH_DIGEST_INTERVAL_MINUTES`.
4. **Setup message** -- one Discord message on the very first run:
   "big-brother is running" plus a table of current metric readings.
   Sent once, never repeated.
5. **Warn tier** (off by default, `VPSWATCH_WARN_ENABLED=false`) -- a
   lower-severity heads-up below "alert" that fires once per incident
   and does not repeat.

## Metrics (v1)

| Metric | Source | Notes |
|---|---|---|
| CPU load (1m) | `/proc/loadavg` | field 0 |
| Memory available | `/proc/meminfo` `MemAvailable` | not "free" -- available |
| Swap used | `/proc/meminfo` `SwapTotal - SwapFree` | |
| Disk used % | `statfs(2)` on each real mount from `/proc/mounts` | `/boot` gets its own threshold |
| I/O wait % | `/proc/stat` `cpu` line, delta over the tick | needs a previous reading, kept in the state file |

Pseudo filesystems (`proc`, `sysfs`, `tmpfs`, `overlay`, `cgroup`, ...)
are skipped for disk usage; only real block-backed filesystems are
reported, deduped by backing device so bind mounts don't double-count.

## Storage

No time-series database. Two files live next to the binary's config
directory:

- **`samples.log`** -- one JSON line per tick (timestamp + every metric
  reading), pruned to the last `VPSWATCH_SAMPLE_RETENTION_MINUTES`. This
  is what the hourly digest reads to compute min/avg/max/p95.
- **`state.json`** -- current alert/dedupe state per metric (consecutive
  bad-check count, whether currently alerting, whether the warn message
  for the current incident has fired), the last digest time, whether the
  one-time setup message has been sent, and the previous `/proc/stat` CPU
  snapshot (needed to compute the I/O wait delta across separate,
  short-lived tick processes).

Because each tick is its own short-lived process invoked by the systemd
timer, all of this genuinely has to live on disk -- there's no
in-memory state carried between ticks.

## Build

```
go build ./cmd/vpswatch
```

or

```
make build
```

Both produce a `vpswatch` binary in the repo root.

## Configuration

Copy `.env.example` to `.env` (in whatever directory you point
`--config-dir` at) and fill in the two secret URLs:

- `VPSWATCH_HEALTHCHECKS_PING_URL` -- your healthchecks.io check's ping URL.
- `VPSWATCH_DISCORD_WEBHOOK_URL` -- a Discord webhook URL.

Every other key in `.env.example` is a tunable with a sane default; see
the comments in that file for what each one does, including the warn-tier
thresholds and the digest/retention intervals (the latter two aren't part
of the original healthchecks/Discord-facing spec, but exist so the hourly
digest can be tested without waiting an hour -- see "How to test" below).

Never commit a real `.env` -- it's gitignored, and `install.sh` never
overwrites an existing one.

vpswatch looks for `.env`, reads/writes `state.json`, and
reads/appends/prunes `samples.log` all in the directory given by
`--config-dir` (default: `$VPSWATCH_CONFIG_DIR`, or the current
directory if that's unset too).

## Install

```
sudo ./install.sh
```

This is idempotent: it builds the binary, creates a `vpswatch` system
user (skipped if it already exists), installs the binary and a seed
`.env` into `/opt/vpswatch` (an existing `.env` is never touched),
renders `systemd/vpswatch.{service,timer}` into `/etc/systemd/system`,
and runs `systemctl daemon-reload` + `enable --now vpswatch.timer`.

Override `INSTALL_DIR`, `SYSTEMD_DIR`, or `SERVICE_USER` to install
somewhere else. See the comments at the top of `install.sh` for how to
exercise its file-layout logic (`SKIP_USERADD=1 SKIP_SYSTEMCTL=1`) in a
scratch directory without root or a real systemd -- useful for reviewing
what it would do before running it for real.

**After installing**, edit `/opt/vpswatch/.env` with your real
healthchecks.io and Discord URLs before the timer's first tick runs
(or the next tick will exit non-zero and log to the journal, since both
URLs are required config).

## Deploy (after the first install)

```
make deploy DEPLOY_DIR=/opt/vpswatch
```

Unlike a long-lived daemon, there's no PID to find and kill first --
`deploy` just builds, swaps the binary in place, and reloads/restarts the
systemd timer so the next tick uses the new binary. `DRY_RUN=1` prints
what it would do without doing it.

## How to test

All of the following can be done from a dev checkout, pointed at a
throwaway `.env` in a scratch directory -- no real install needed.

**Build:**

```
go build ./cmd/vpswatch
```

**Setup message fires once:** point `.env` at a test webhook (e.g.
[webhook.site](https://webhook.site), or a real Discord webhook in a
scratch channel) and a healthchecks.io ping URL, then:

```
./vpswatch --config-dir /path/to/scratch-dir
```

The first run should send "big-brother is running" plus a table of
current readings, and ping healthchecks. Run it again -- the setup
message must not repeat, but the heartbeat still fires every time.

**No alert on a healthy box:** with default thresholds, a normal tick
shouldn't send anything besides the heartbeat (and the setup message, on
the first run).

**Debounce + recovery:** force a metric bad, e.g.
`VPSWATCH_CPU_LOAD_ALERT=0.001` in the scratch `.env` (load average is
essentially never that low), then run the tick 5 times in a row:

```
for i in $(seq 5); do ./vpswatch --config-dir /path/to/scratch-dir; done
```

The alert should fire on the 5th tick, not before. Then set the
threshold back to something realistic (e.g. `6.0`) and run one more
tick -- a single recovery message should fire.

**Hourly digest:** set `VPSWATCH_DIGEST_INTERVAL_MINUTES=1` in the
scratch `.env`, run a few ticks a minute or so apart, and confirm a
min/avg/max/p95 table shows up on Discord once the interval elapses,
without needing to wait a real hour.

**Warn tier:** set `VPSWATCH_WARN_ENABLED=true` and a warn threshold
below your current reading (e.g. `VPSWATCH_CPU_LOAD_WARN=0.001`). The
warn message should fire once and not repeat on the next tick, even
though the metric is still in the warn zone.

**Install:** in a container or throwaway VM (not this dev checkout's
host, since it touches systemd and creates a system user for real):

```
sudo ./install.sh
# then edit /opt/vpswatch/.env with real URLs, and check:
systemctl list-timers vpswatch.timer
journalctl -u vpswatch.service -f
```

Confirm the timer fires every minute and each run's log lines show up in
the journal.
