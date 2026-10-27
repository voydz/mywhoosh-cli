---
name: mywhoosh
description: Use when reading MyWhoosh profile, training statistics or activities.
version: 1.0.0
author: mywhoosh-cli contributors
license: MIT
metadata:
  hermes:
    tags: [mywhoosh, cycling, fit, cli]
    related_skills: []
---

# MyWhoosh CLI

## Overview

Use `mywhoosh` for read-only profile and training statistics, FIT summaries, GPX points, activity listing and original FIT/GPX downloads. It uses an undocumented private API whose behavior can change. This repository skill is portable guidance, not automatically installed runtime configuration.

## When to use

- Inspect completed MyWhoosh activities as JSON.
- Read the rider profile, source-reported statistics, FIT session/lap summaries or recorded GPX points.
- Download one explicitly selected activity's original FIT or GPX file.
- Diagnose missing/expired local sessions without exposing account details.

Do not use it for Garmin writes, Coach workout conversion, device spoofing, bulk synchronization or scheduling. These are outside this CLI.

## Workflow

1. Check `mywhoosh version` and `mywhoosh status --format json`. Local session presence is not proof of validity. If online validity matters, use `mywhoosh status --verify --format json`; `unknown` means an upstream/network problem, not rejected credentials.
2. If needed, ask the user to authenticate securely with `mywhoosh login --email EMAIL` at their terminal. Never request passwords in chat or pass them as argv. For unattended use, credentials must already be injected through protected `MYWHOOSH_EMAIL`/`MYWHOOSH_PASSWORD` environment variables, or explicitly use `--password-stdin` from an approved secret manager. Do not create `.env` copies.
3. List one bounded page: `mywhoosh activities --page 1 --limit 20 --format json`. Preserve exact IDs as strings. If `pagination_known` is false, a null `next_page` does not confirm the end. Request further pages explicitly; do not implement an unbounded crawl.
4. Read selected coaching data with `mywhoosh profile --format json`, `mywhoosh stats --format json`, or `mywhoosh activities get ID --format json`. The last command detects FIT/GPX and returns recorded FIT sessions/laps or GPX points without writing a file. Missing values are null, not zero. Units appear in field names; elapsed time includes pauses and timer time excludes them. FIT metrics are recorded source values, not estimates. Configured MyWhoosh FTP is not inferred from best power. The three `*_reported_unknown_unit` totals retain raw source values; do not assign them units or assume totals equal the listed activities.
5. If a file is requested, download the exact chosen ID: `mywhoosh activities download ID --output /approved/path/ride.fit --format json`. The parent directory must exist. Use `--overwrite` only with explicit replacement approval. Verify the receipt path and byte count; do not infer FIT semantics or Garmin metrics from structural screening.
6. Report only verified results. A successful local fixture/build is not a real account download; a downloaded FIT is not a Garmin upload. Any downstream `gc activities upload FILE --format json` requires separate write authorization and read-back verification.

For GPX-capable versions, `activities get ID --format json` detects the actual source format. GPX returns `source: "gpx"`, empty `sessions`/`laps` and `tracks` with ordered segments/points. FIT retains its original schema and summaries. GPX contains selected recorded timestamps/coordinates/elevation/power/HR/cadence, not derived distance, averages, pause time or fabricated laps. Missing values stay null; zeros remain present. Capture full point JSON privately before reducing it: it may be large and contain coordinates.

Downloads still default to FIT. If MyWhoosh returns GPX, use the explicit `mywhoosh activities download ID --file-format gpx --output /approved/path/ride.gpx --format json`. A GPX suffix is required. Original bytes including BOM are preserved; mismatches return `unsupported_format` without publishing a file. GPX is fully validated within 64 MiB, 100,000 points, 10,000 combined tracks/segments and depth 64. Unknown extensions remain in the original, not necessarily in JSON. No GPX-to-FIT conversion or automatic downstream sync adaptation is implied.

Completion: the requested page or original file is obtained, safe output is verified, and live limitations remain explicit.

## Authentication and automation

`auth login|status|logout` aliases are supported. Root `--config-dir PATH` precedes the command; `MYWHOOSH_CONFIG_DIR` is its environment fallback. Only one account is supported. The config directory and session file must be private (0700/0600). Logout removes the local token, not a server-side session. Do not inspect or include `session.json` in reports.

Profile, stats and activity commands reuse the session and reauthenticate at most once on API HTTP 401, only when both credential environment variables are available. They never prompt or invent a refresh endpoint. Status verification never relogs in. Exit 3 needs authentication; exit 1 may be a network/upstream/structural problem; exit 4 confirms absence; exit 5 protects an existing file; exit 6 means lookup reached its 100-page bound, not absence.

## Common pitfalls

- IDs and file references differ; use the activity ID from JSON and let the CLI resolve its file reference.
- `--limit` is a server hint; do not truncate or discard the extra records a server may return.
- Never use activity names as paths. Choose a caller-approved output path and preserve original bytes.
- FIT download screening checks header/length bounds only; FIT `get` separately checks CRCs. GPX download/get validate the supported UTF-8 GPX 1.1 dialect. Format errors do not establish login failure or source corruption.
- Never log passwords, bearer tokens, signed URLs, raw upstream bodies or personal FIT contents.
- Never add a public endpoint/TLS override to test production behavior; the repository's tests inject local TLS clients internally.

## Verification checklist

- [ ] Exact activity ID preserved; bounded request scope.
- [ ] JSON parsed as one document, exit status checked separately.
- [ ] Output path exists and receipt is consistent; overwrite was authorized if used.
- [ ] No secrets or signed URLs in reports.
- [ ] Live MyWhoosh, Garmin upload and metric claims match actual observed evidence.
