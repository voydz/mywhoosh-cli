# mywhoosh-cli

A small standalone CLI for MyWhoosh: log in, list completed activities, and download an activity's **original FIT bytes**. One binary, one account, no Garmin credentials, background jobs, device spoofing or FIT rewriting.

> **Compatibility:** MyWhoosh has no public API. This implementation follows source-reviewed private endpoints and is tested against local TLS fixtures. Live login and a real activity download have **not** been verified: no MyWhoosh credentials were supplied. Upstream API changes may require an update. This project is not affiliated with MyWhoosh.

## Install

```sh
brew install voydz/tap/mywhoosh-cli
mywhoosh version
```

Homebrew availability depends on the separately published release/tap. Release archives use names such as `mywhoosh-cli-0.1.0-macos-arm64.tar.gz`; Linux x86_64/aarch64 and macOS arm64/x86_64 are packaging targets. See [GitHub Releases](https://github.com/voydz/mywhoosh-cli/releases) for published assets and checksums; source/CI does not by itself establish a published release.

Or build from source with Go 1.24 or newer:

```sh
git clone https://github.com/voydz/mywhoosh-cli.git
cd mywhoosh-cli
go build -o mywhoosh ./cmd/mywhoosh
./mywhoosh --help
```

## Quick start

```sh
mywhoosh login --email rider@example.com             # hidden terminal password
mywhoosh status                                    # local presence, no request
mywhoosh status --verify --format json              # online validity, no relogin
mywhoosh activities                                 # one page, table
mywhoosh activities --page 2 --limit 20 --format json
mywhoosh activities download ACTIVITY_ID -o ride.fit
mywhoosh activities download ACTIVITY_ID --output ride.fit --overwrite --format json
mywhoosh logout                                    # local session removal only
```

`auth login|status|logout` and `activities list` are also supported. The root `--config-dir PATH` option goes **before** the command. Run `mywhoosh --help` for the command summary.

### Noninteractive authentication

Never put a password on the command line; `--password` is deliberately unsupported. Supply a password through explicitly selected stdin, a protected caller environment, or the hidden terminal prompt:

```sh
# Supply stdin from your own secret manager; do not put literal secrets in shell history.
your-secret-manager-command | mywhoosh login --email rider@example.com --password-stdin

# With MYWHOOSH_EMAIL and MYWHOOSH_PASSWORD already injected securely:
mywhoosh login
```

Email precedence: `--email`, then `MYWHOOSH_EMAIL`, then terminal prompt. Password precedence: `--password-stdin`, then `MYWHOOSH_PASSWORD`, then hidden terminal prompt. Missing noninteractive credentials fail immediately with exit 3. No `.env` file discovery or secret-file duplication is performed. Environment variables can still be visible to privileged processes; protect the injecting environment.

Activity commands reuse a cached session. On API HTTP 401 they reauthenticate **at most once per command**, and only if **both** credential environment variables are present, then save the new session and retry once. They never prompt. There is no invented refresh-token endpoint, general retry loop, or permanent unattended-access guarantee. A rejected signed file URL does not trigger account reauthentication. Without injected credentials, expired sessions require another `mywhoosh login`. HTTP 429 and 5xx are failures, not empty lists.

### Session storage

The default directory is `filepath.Join(os.UserConfigDir(), "mywhoosh")`: normally `$XDG_CONFIG_HOME/mywhoosh` or `~/.config/mywhoosh` on Linux, and `~/Library/Application Support/mywhoosh` on macOS. Override with `MYWHOOSH_CONFIG_DIR` or the root `--config-dir PATH` option.

Only version, bearer access token, opaque Whoosh ID and a stable random device ID are saved in `session.json`. No password or email is saved. The directory must be 0700 and session/lock files 0600; insecure permissions, symlinked session/directory targets and malformed sessions are rejected. Updates use an advisory lock and atomic replacement. The stable device ID is a request identifier required by the login protocol, **not** a recording-device identity or FIT identity change. Logout removes the local session; it does not claim server-side revocation. Treat backups of this directory as credentials.

## Activity and pagination contract

`activities [list]` defaults to page 1, requested page size 20, table output. Both page and limit must be positive. It fetches **exactly one API page** and preserves every returned record, even if the server ignores the requested page size. There is no `--all` or bulk download command.

`--format json` produces exactly one document on stdout. Diagnostics are safe, coded text on stderr. JSON activity fields are stable:

```json
{"activities":[{"id":"opaque-id","name":"Ride","started_at":"2026-10-02T18:30:00Z","downloadable":true}],"page":1,"next_page":null,"pagination_known":true}
```

This is a **schema example**, not live account data. IDs remain strings, including numeric IDs larger than JavaScript's exact integer range. Unknown or unzoned timestamps become `null`; timezone offsets are required for textual timestamps. Numeric Unix seconds/milliseconds are supported.

Known envelopes are a bare array, `data` array, and `data.results` array. Supported pagination indicators are `page`/`currentPage`/`current_page`, `totalPages`/`total_pages`, `hasNext`/`hasNextPage`, and `nextPage`. Contradictory metadata is an error, not permission to skip records. These are supported parser forms, not a claim that every current server returns them. Without metadata on a nonempty page, `pagination_known` is false and `next_page` is null: **that does not establish an end**. An empty page confirms an end.

Download walks pages to resolve the **exact activity ID** to its distinct `activityFileId`. It stops on a confirmed end, rejects repeated/overlapping pages and inconsistent metadata, and searches at most 100 pages. Exhaustion returns exit 6 rather than falsely declaring absence. Activities without a file reference return exit 4.

## Safe original FIT downloads

Choose an explicit path; filenames are never derived from an activity name or ID. The parent directory must already exist. Files are streamed into a private temporary file beside the destination and published only after a complete successful download. Publication cannot clobber an existing target unless `--overwrite` is explicit; overwrites are atomic. Failure cleans up the temporary file and leaves the previous destination untouched. Output files are 0600. FIT bytes are never printed to stdout.

Downloads require HTTPS, use a separate credential-free HTTP client, strip credentials/referrers on redirects, and limit redirect hops. API requests never follow redirects. Errors do not disclose passwords, bearer tokens, signed URLs, raw upstream bodies or source transport errors. Production endpoints are fixed; there is no public endpoint override or TLS bypass.

Structural screening checks a 12/14-byte FIT header, `.FIT` signature, declared data length plus file-CRC byte bounds, and the 64 MiB limit. It rejects obvious HTML/XML, truncated or oversized data. **It does not verify CRCs, FIT record semantics, activity validity, sensor completeness or Garmin metrics.** Original bytes are not repaired, re-encoded or modified.

Download JSON success:

```json
{"activity_id":"opaque-id","path":"/absolute/path/ride.fit","bytes":1234}
```

Again, a schema example. Status JSON contains `session_present` and `verification` (`not_checked`, `missing`, `valid`, `rejected`, `unknown`). Online status emits that document even when verification returns a nonzero exit status. A network failure is `unknown`, not rejected credentials.

### Optional downstream use

After separately inspecting the downloaded FIT, you may choose to use your existing Garmin CLI:

```sh
gc activities upload ride.fit --format json
```

That is a **separate write to Garmin**, not a command executed by this CLI. Garmin upload, deduplication, metric parity, Coach matching and scheduling have not been tested or implemented here.

## Exit status

| Exit | Meaning | Example error codes |
| --- | --- | --- |
| 0 | Success, including empty lists and local missing-session status | — |
| 1 | Transport, upstream, filesystem or malformed API response | `transport_error`, `rate_limited`, `upstream_error`, `invalid_fit`, `invalid_pagination`, `output_failed` |
| 2 | Usage or malformed/insecure configuration | `usage_error`, `invalid_config` |
| 3 | Credentials required or authentication rejected | `credentials_required`, `auth_required` |
| 4 | Confirmed activity/file absence | `not_found`, `file_not_found` |
| 5 | Existing destination without overwrite | `target_exists` |
| 6 | Lookup bound reached without a confirmed end | `search_limit_reached` |

## Development and verification

```sh
gofmt -w cmd internal
go vet ./...
go test ./...
go test -race ./...
go build -o dist/mywhoosh ./cmd/mywhoosh
```

CI runs the suite, race detector, vet, formatting checks, native builds and compiled executable smoke tests on Linux, macOS arm64 and macOS Intel. Tests use isolated temporary stores and local TLS servers, including request-body assertions, session reuse/recovery bounds, pagination, credential-safe redirects, atomic/no-clobber output and byte preservation. A fixture with a FIT-shaped header/length is used **only for structural screening**, never as evidence of semantic FIT validity. Test-only transport injection is not exposed by the shipping binary.

The local review host does not need a project toolchain: builds and execution can run on GitHub Actions. Passing fixtures does **not** prove live MyWhoosh compatibility. A live pilot still requires authorized secure credentials, login, one real activity download and cached-session reuse; no personal FIT fixture belongs in this public repository.

The source protocol/parser is adapted from MIT-licensed `iquesada/mywhoosh2garmin` at `e3575c37795a9849d443b827f91d6106e8aa9aae`; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). No Garmin integration or sync architecture was copied. Agent operation guidance is in [skills/mywhoosh/SKILL.md](skills/mywhoosh/SKILL.md).

MIT licensed; see [LICENSE](LICENSE).
