# ECS Cloud Assistant Source Deploy

This runbook is for A21 Air emergency/source hotfix deployment to the Aliyun ECS gateway. It is not the final production package path.

Use the fixed `ecs-package`, `ecs-package-validate` and `ecs-preflight-dry-run` flow for release-package validation. Use this script only when a small gateway source hotfix must be deployed through Cloud Assistant and then recorded as evidence.

## Dry Run

Run from the repository root:

```bash
server/deploy/aliyun/cloud-assistant-source-deploy.sh \
  --source-ref HEAD \
  --dry-run
```

The dry run creates a `git archive` snapshot, splits it into Cloud Assistant-safe chunks, renders the remote Bash script and prints only safe metadata:

- source ref and short commit
- source archive SHA256
- archive byte count
- chunk size and part count
- remote temp directory
- whether a proxy is configured

It does not call Aliyun APIs and does not read provider credentials.

## Deploy

When local Aliyun API egress is unreliable, open a SOCKS tunnel through 5080lab first:

```bash
ssh -i "$A21_5080_KEY" \
  -N -D 127.0.0.1:18080 \
  -o BatchMode=yes \
  -o StrictHostKeyChecking=accept-new \
  -o ExitOnForwardFailure=yes \
  -p 22 21@192.168.1.6
```

Then deploy:

```bash
server/deploy/aliyun/cloud-assistant-source-deploy.sh \
  --source-ref HEAD \
  --proxy socks5://127.0.0.1:18080 \
  --public-base-url http://47.103.57.217
```

The script:

- requires a clean worktree unless `--allow-dirty` is passed
- archives only the requested Git ref
- uses 20 KB base64 chunks by default, below Cloud Assistant `SendFile` practical limits
- sanitizes Aliyun CLI stderr before printing signed request parameters
- runs the remote command under Bash with `set -euo pipefail`
- verifies the uploaded part count and SHA256 before extraction
- runs a source-archive-safe test set
- builds `cmd/stackchan-gateway`
- verifies `libopus.so.0`
- sources `/etc/a21-air/gateway.env`
- runs `voice-profile-check`
- installs the binary to `/opt/a21-air/stackchan-gateway/stackchan-gateway`
- restarts `stackchan-gateway`
- checks ECS-local `/healthz` and `/readyz`
- verifies the installed `physical-led-retest`, `physical-reconnect-retest`, `physical-acceptance-metrics` and `physical-acceptance-report` commands are present without producing false hardware acceptance reports
- optionally checks public `/healthz` and `/readyz`

## Evidence To Record

Record only safe deployment facts in `docs/control/TASK_BOARD.md`:

- source ref and source SHA256
- Cloud Assistant `SendFile` invoke IDs and count
- `RunCommand` invoke ID
- tests run on ECS
- `voice-profile-check` profile summary
- binary mtime, size and owner
- ECS-local and public health/readiness result
- any failed deploy attempts and whether they happened before install/restart

Never record Aliyun request signatures, raw OTA tokens, provider keys, private env contents, transcripts, prompts, generated text, raw audio or physical LED RGB payload values.
