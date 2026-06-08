rt: Ripta's collection of tools

Expectations:

- tools read from STDIN, write to STDOUT, and hopefully print errors to STDERR;
- tools are meant to be combined with others, e.g., `hs` might be less useful
  to you, because it prints file hashes in binary output instead of hex (but
  `enc hex` converts it to hex strings).

You can install the all-in-one hyperbinary, which excludes any tools with CGO dependencies:

```
go install github.com/ripta/rt/hypercmd/rt@latest
```

or install all tools as individual binaries:

```
go install github.com/ripta/rt/cmd/...@latest
```

or pick-and-choose each tool to individually install:

* [cg](#cg) to run a command and annotate its output with timestamps
* [enc](#enc) to encode and decode STDIN
* [grpcto](#grpcto) to frame and unframe gRPC messages
* [hs](#hs) to hash STDIN
* [lipsum](#lipsum) to generate placeholder text
* [place](#place) for macOS Location Services (requires macOS and CGO)
* [streamdiff](#streamdiff) to help you pick out field changes off a stream of JSON
* [structfiles](#structfiles-sf) to examine and compare a pile of structured files
* [toto](#toto) to inspect some protobuf messages
* [uni](#uni) for unicode utils
* [yfmt](#yfmt) to reindent YAML while preserving comments

or, last but not least, install a lighter version of the hyperbinary, which excludes
tools with CGO, terminal, or filesystem requirements, but compiles to WASM:

```
GOOS=wasip1 GOARCH=wasm CGO_ENABLED=0 go build -o rt_lite.wasm -v ./hypercmd/rt_lite
```

Pull requests welcome, though you should probably check first before sinking any time.



`cg`
----

Run a command and annotate each line of its stdout and stderr with a stream
indicator (`O` for stdout, `E` for stderr, `I` for cg's own lifecycle
messages). At the end of the run, print a one-line summary with the exit
code, wall duration, and per-stream line counts.

Acts like the `annotate-output` script; `cg` is short for command guard.

```
go install github.com/ripta/rt/cmd/cg@latest
```

Basic usage:

```
❯ cg -- echo hello
O: hello
I: Finished exitcode=0 in 2ms (out=1 err=0)
```

Stdout and stderr are distinguished:

```
❯ cg -- sh -c 'echo out; echo err >&2'
O: out
E: err
I: Finished exitcode=0 in 3ms (out=1 err=1)
```

The child's exit code is propagated:

```
❯ cg -- sh -c 'exit 42'; echo $?
I: Finished exitcode=42 in 2ms (out=0 err=0)
42
```

If the child is killed by a signal, the summary reports the signal number
instead of an exit code:

```
❯ cg -- sh -c 'kill -TERM $$'
I: Finished signal=15 in 2ms (out=0 err=0)
```

SIGINT and SIGTERM are forwarded to the child process.

Verbose mode (`-v` / `--verbose`) restores the older preamble — version line,
prefix echo, `Started` line — and prefixes every output line with a
timestamp:

```
❯ cg -v -- echo hello
19:02:59 I: cg v0.1.0
19:02:59 I: prefix="15:04:05 "
19:02:59 I: Started echo hello
19:02:59 O: hello
19:02:59 I: Finished exitcode=0 in 2ms (out=1 err=0)
```

The verbose timestamp format follows the Go `time.Format` layout and is
customised with `--format`:

```
❯ cg -v --format '2006-01-02T15:04:05 ' -- echo hello
2026-02-22T19:05:00 I: cg v0.1.0
2026-02-22T19:05:00 I: prefix="2006-01-02T15:04:05 "
2026-02-22T19:05:00 I: Started echo hello
2026-02-22T19:05:00 O: hello
2026-02-22T19:05:00 I: Finished exitcode=0 in 2ms (out=1 err=0)
```

### Capturing output

`-c` / `--capture` writes the child's stdout and stderr to files under
`$TMPDIR/cg/<ID>/` and appends a short run ID to the summary line. The ID is
6 characters of Crockford base-32 (no `I`, `L`, `O`, or `U`), regenerated on
collision.

```
❯ cg -c -- sh -c 'echo out; echo err >&2'
I: Finished exitcode=0 in 3ms (out=1 err=1) id=Q3F9K2
```

Each run directory contains `stdout`, `stderr`, and a `meta.json` written
atomically at end-of-run. Resolution subcommands let downstream tooling
thread the ID through follow-up calls without scraping paths:

```
❯ cg out Q3F9K2
/tmp/cg/Q3F9K2/stdout

❯ cg paths Q3F9K2
/tmp/cg/Q3F9K2/stdout
/tmp/cg/Q3F9K2/stderr

❯ rg -i FOO $(cg out Q3F9K2)
```

`cg ls` lists recent runs, most-recent-first by mtime, one row per run:

```
❯ cg ls
Q3F9K2  exit=0   3ms     sh -c 'echo out; echo err >&2'
M7P4QX  exit=42  2ms     sh -c 'exit 42'
```

`cg ls -n N` overrides the default cap of 20.

Capture itself never deletes anything. `cg prune` is the explicit cleanup
hook:

```
❯ cg prune                  # keep the 50 most recent by mtime
❯ cg prune --keep 10        # keep the 10 most recent
❯ cg prune --older-than 7d  # evict runs older than seven days
❯ cg prune --dry-run        # print what would be removed, change nothing
```

`--keep` and `--older-than` are mutually exclusive. `--older-than` accepts
the Go `time.ParseDuration` grammar (`90m`, `1h30m`, `2h`) plus convenience
suffixes `Nd` (days) and `Nw` (weeks). Stray non-run entries and incomplete
runs (no `meta.json`) under `$TMPDIR/cg/` are skipped.

### Other flags

`--buffered` defers the child's output until the command finishes, grouping
by stream instead of streaming in real time.

`--log-parse json|logfmt` reformats structured child log lines; see
`cg --help` for the message-key, timestamp-key, timestamp-format, and field
selectors.

### MCP server

`cg mcp` starts a stdio MCP server that exposes the capture-run model as
native tools. Coding agents that speak MCP (Claude Code, the Anthropic SDK,
others) can call `cg` with structured JSON input and output rather than
constructing shell argv and parsing printed paths. The server is a thin
wrapper over the same on-disk capture model the shell subcommands use, so a
run started with `cg -c -- cmd` is visible to the MCP tools and a run
started by `cg_run` is visible to `cg ls`. MCP is additive; the shell
subcommands continue to work unchanged.

Register the server with Claude Code by adding a `cg` entry under
`mcpServers`:

```json
{
  "mcpServers": {
    "cg": {
      "command": "cg",
      "args": ["mcp"]
    }
  }
}
```

Any MCP host that speaks the stdio transport launches the server the same
way: spawn `cg mcp` and exchange MCP messages over its stdin and stdout.

The server registers ten tools:

| Tool | Purpose |
|------|---------|
| `cg_run` | Run a command with capture and return metadata plus head- or tail-window excerpts. |
| `cg_list` | List recent capture runs, most-recent-first by mtime. |
| `cg_meta` | Return the run state and `meta.json` fields for a run. |
| `cg_wait` | Block until a run finishes or a timeout elapses. |
| `cg_cancel` | Signal a run's process group, with optional escalation. |
| `cg_paths` | Return absolute paths for a run's `stdout`, `stderr`, `meta.json`. |
| `cg_stdout` | Fetch captured stdout for a run, with byte limits and head/tail windowing. |
| `cg_stderr` | Fetch captured stderr for a run, with byte limits and head/tail windowing. |
| `cg_grep` | Search a run's captured output and return matching lines. |
| `cg_prune` | Evict capture runs by count (`keep`) or age (`older_than`). |

Unknown IDs and malformed inputs surface as MCP tool errors. A child
command exiting non-zero is data, not an error: `cg_run` returns
successfully with `exit_code: N` and the caller decides how to react.

#### `cg_run`

Run a command with capture. Blocks until the child exits or
`wait_timeout_ms` elapses; on timeout, the child keeps running and the
capture continues on disk.

**Inputs**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `command` | `string[]` | required | argv; index 0 is the program. |
| `cwd` | `string` | server cwd | working directory. |
| `env` | `object` | server env | environment overrides, merged onto the server's env. |
| `wait` | `bool` | `true` | block until exit or timeout. |
| `wait_timeout_ms` | `int` | `60000` | how long to wait before returning `timed_out: true`. |
| `excerpt_bytes` | `int` | `4096` | per-stream excerpt cap; max `16384`. |
| `excerpt_from` | `string` | `auto` | excerpt window: `auto` picks head on success, tail on non-zero exit / signal / timeout; `head` or `tail` forces the window. |

**Outputs**

| Field | Type | Notes |
|-------|------|-------|
| `id` | `string` | Capture run ID. |
| `started` | `bool` | Set when `wait: false`. |
| `timed_out` | `bool` | Set when the wait timeout fired. |
| `exit_code` | `int?` | Child exit code; absent if timed out. |
| `signal` | `int?` | Signal that killed the child, if any. |
| `duration_ms` | `int?` | Wall-clock run duration; absent if timed out. |
| `stdout_lines` | `int?` | Total stdout lines; absent if timed out. |
| `stderr_lines` | `int?` | Total stderr lines; absent if timed out. |
| `stdout_excerpt` | `string` | `excerpt_bytes` from stdout; window per `excerpt_from`. |
| `stderr_excerpt` | `string` | `excerpt_bytes` from stderr; window per `excerpt_from`. |
| `excerpt_from` | `string` | Window that was used: `head` or `tail`. Omitted when no excerpts (e.g., `wait: false`). |
| `truncated` | `bool` | Either stream had more than `excerpt_bytes`. |

#### `cg_list`

List recent capture runs, most-recent-first by directory mtime. The default
surfaces only finished runs; pass `state` to include in-flight runs (started,
no `meta.json` yet) or to ask for them on their own.

**Inputs**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `limit` | `int` | `20` | maximum runs to return; max `1000`. |
| `state` | `string` | `finished` | filter: `all`, `finished`, or `running`. |

**Outputs**

| Field | Type | Notes |
|-------|------|-------|
| `runs` | `object[]` | One entry per matching run; see fields below. |

Every `runs[]` entry has `id` and `state` (`"finished"` or `"running"`).
Finished entries also carry `command`, `started_at`, `finished_at`,
`duration_ms`, `exit_code`, `signal?`, `stdout_lines`, `stderr_lines`.
In-flight entries are sparse: only `id`, `state`, and `started_at`
synthesized from the run directory's mtime.

#### `cg_meta`

Return a run's state and `meta.json` fields. An in-flight run (no
`meta.json` yet) returns `{id, state: "running"}` with no error; a finished
run returns `state: "finished"` plus all meta fields. An unknown ID is a
tool error.

**Inputs**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `id` | `string` | required | capture run ID. |

**Outputs**

| Field | Type | Notes |
|-------|------|-------|
| `id` | `string` | Run ID. |
| `state` | `string` | `"running"` or `"finished"`. |
| `command` | `string[]` | argv that was executed; finished runs only. |
| `started_at` | `string` | RFC 3339 timestamp; finished runs only. |
| `finished_at` | `string` | RFC 3339 timestamp; finished runs only. |
| `duration_ms` | `int` | Wall-clock duration; finished runs only. |
| `exit_code` | `int` | Child exit code; finished runs only. |
| `signal` | `int?` | Signal that killed the child, if any. |
| `stdout_lines` | `int` | Total stdout lines; finished runs only. |
| `stderr_lines` | `int` | Total stderr lines; finished runs only. |

#### `cg_wait`

Block until a run finishes or `timeout_ms` elapses. Uses the in-process
`Done` channel for runs this server started and falls back to filesystem
polling otherwise. An unknown ID is a tool error.

**Inputs**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `id` | `string` | required | capture run ID. |
| `timeout_ms` | `int` | `60000` | how long to block before returning `finished: false`. |

**Outputs**

| Field | Type | Notes |
|-------|------|-------|
| `id` | `string` | Run ID. |
| `finished` | `bool` | `true` if the run completed before the timeout. |
| meta fields | — | When `finished`, the same fields as `cg_meta`. |

#### `cg_cancel`

Send a signal to a run's process group. An already-finished run returns
`{signaled: false}` without error; an unknown ID is a tool error. With
`escalate_after_ms > 0`, the server sends the initial signal, waits up to
the deadline, and sends `escalate_signal` if the child is still running.

**Inputs**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `id` | `string` | required | capture run ID. |
| `signal` | `string`/`int` | `SIGTERM` | initial signal; `SIGTERM`, `SIGINT`, `SIGKILL`, or numeric. |
| `escalate_after_ms` | `int` | `0` | wait this long, then escalate; `0` disables escalation. |
| `escalate_signal` | `string`/`int` | `SIGKILL` | signal sent on escalation. |

**Outputs**

| Field | Type | Notes |
|-------|------|-------|
| `id` | `string` | Run ID. |
| `signaled` | `bool` | Whether the initial signal was sent. |
| `signal` | `int` | Numeric value of the initial signal. |
| `escalated` | `bool` | Whether `escalate_signal` was sent. |
| `escalate_signal` | `int?` | Numeric escalation signal; present only when escalated. |
| `finished` | `bool` | Whether the child had exited by the time the call returned. |

#### `cg_paths`

Return absolute paths for a run's `stdout`, `stderr`, and `meta.json`
files. Works for in-flight runs; the `meta` path is returned even when the
file does not yet exist, so callers can poll the same path.

**Inputs**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `id` | `string` | required | capture run ID. |

**Outputs**

| Field | Type | Notes |
|-------|------|-------|
| `stdout` | `string` | Absolute path to the stdout file. |
| `stderr` | `string` | Absolute path to the stderr file. |
| `meta` | `string` | Absolute path to `meta.json` (may not exist yet). |

#### `cg_stdout` and `cg_stderr`

Fetch captured stdout or stderr for a run. Defaults to the first 16 KiB;
`from: "tail"` reads the last `max_bytes` instead. Works for in-flight
runs. The default encoding validates bytes as UTF-8 and falls back to
base64 automatically on invalid input (binary streams or a tail read
that lands mid-codepoint); set `content_encoding: "base64"` to force
base64 for known binary streams.

**Inputs**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `id` | `string` | required | capture run ID. |
| `max_bytes` | `int` | `16384` | response cap; max `1048576` (1 MiB), clamped if higher. |
| `from` | `string` | `"head"` | `"head"` reads from `offset`; `"tail"` reads the last `max_bytes`. |
| `offset` | `int` | `0` | byte offset for head reads; ignored when `from: "tail"`. |
| `content_encoding` | `string` | `"utf8"` | `"utf8"` validates UTF-8 and falls back to base64 on invalid bytes; `"base64"` always base64-encodes. |

**Outputs**

| Field | Type | Notes |
|-------|------|-------|
| `content` | `string` | Bytes read from the stream, encoded per `content_encoding`. |
| `content_encoding` | `string` | `"utf8"` or `"base64"`; describes how to decode `content`. |
| `total_bytes` | `int` | Total size of the stream file. |
| `returned_bytes` | `int` | Length of `content` in bytes. |
| `truncated` | `bool` | More data exists beyond the returned window. |
| `clamped` | `bool` | `max_bytes` was reduced to the 1 MiB ceiling. |

#### `cg_grep`

Search a run's captured output line by line and return matching lines.
Supply exactly one of `text` (fixed substring) or `pattern` (RE2 regex).
Searches both streams by default. Works for in-flight runs. A line with
invalid UTF-8 is base64-encoded and tagged `content_encoding: "base64"`.

**Inputs**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `id` | `string` | required | capture run ID. |
| `text` | `string` | — | fixed-string substring; mutually exclusive with `pattern`. |
| `pattern` | `string` | — | RE2 regex; mutually exclusive with `text`. |
| `streams` | `string` | `all` | which streams to search: `all`, `stdout`, or `stderr`. |
| `case_insensitive` | `bool` | `false` | fold case when matching. |
| `invert_match` | `bool` | `false` | return lines that do NOT match. |
| `max_matches` | `int` | `1000` | cap on returned matches; max `10000`. |

**Outputs**

| Field | Type | Notes |
|-------|------|-------|
| `matches` | `object[]` | One entry per matching line; see fields below. |
| `match_count` | `int` | Number of returned matches. |
| `truncated` | `bool` | `max_matches` was hit before the streams were fully scanned. |

Each `matches[]` entry has `stream` (`"stdout"` or `"stderr"`),
`line_number` (1-based, per stream), `line`, and `content_encoding`
(omitted for UTF-8 lines, `"base64"` when the line is base64-encoded).

#### `cg_prune`

Evict capture runs from `$TMPDIR/cg/`. Either keep the `N` most recent
runs by mtime or remove runs older than a duration. `keep` and
`older_than` are mutually exclusive.

**Inputs**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `keep` | `int` | `50` | keep N most recent runs by mtime. |
| `older_than` | `string` | unset | evict runs older than the given duration, e.g. `7d`, `2h`, `90m`. |
| `dry_run` | `bool` | `false` | report what would be removed without removing. |

**Outputs**

| Field | Type | Notes |
|-------|------|-------|
| `removed` | `string[]` | Run IDs that were or would be removed. |
| `dry_run` | `bool` | Echoes the input flag. |


`enc`
----

```
go install github.com/ripta/rt/cmd/enc@latest
```

Encode and decode strings using various encodings:

* `a85` for ascii85;
* `b32` for base32 (RFC 4848 standard encoding, `ABCDEFGHIJKLMNOPQRSTUVWXYZ234567`);
* `b32c` for base32 with Crockford's alphabet (`0123456789ABCDEFGHJKMNPQRSTVWXYZ`);
* `b58` for base58;
* `b64` for base64 (RFC 4648 standard encoding, `ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/`);
* `hex` for lowercase hexadecimal;
* `url` for URL escape/unescape; and
* `varsel` for encoding raw bytes into Unicode variation selectors (VS-1 to VS-256).


`hs`
----

```
go install github.com/ripta/rt/cmd/hs@latest
```

Hash the input and print the resulting hash in binary bytes. Run with `-h` to
see the list of supported hash functions that are compiled into the binary,
which is approximately:

* `sha1` for SHA-1;
* `sha224` for SHA-224;
* `sha256` for SHA-256;
* `sha3` for SHA-3/512;
* `sha384` for SHA-384; and
* `sha512` for SHA-512.

To output hexadecimal, pipe the output to `enc hex`. My knowledge graph uses a
different representation for hashes, so it's useful to me to not have the hex
representation.

```
❯ head -n 2 hamlet.txt
To be, or not to be: that is the question:
Whether 'tis nobler in the mind to suffer

❯ cat hamlet.txt | hs sha256 | enc hex
e26671d53d74b6751373ad34768580af77847aa1513203d9a06c292617ab5c4b%

❯ cat hamlet.txt | hs sha256 | enc base64
4mZx1T10tnUTc600doWAr3eEeqFRMgPZoGwpJherXEs=%
```

(ICYDK, that `%` at the end is zsh's `PROMPT_EOL_MARK`.)


`grpcto`
--------

```
go install github.com/ripta/rt/cmd/grpcto@latest
```

Frame and unframe raw bytes in a gRPC envelope. For example, assuming a proto
message crafted using either `toto` (included in this repo) or `protoc
--encode` (the official protobuf compiler), you can frame the message using:

```
echo 'hello:"world"' \
    | protoc --encode foo.bar.v1.Thing ./thing.proto \
    | grpcto frame > message.raw
```

where the resulting `message.raw` can be sent directly to a running gRPC
service using `curl`:

```
curl -X POST --data-binary @message.raw -o response.raw -H 'content-type: application/grpc' --raw https://localhost:8443/foo.bar.v1.Thinger/Thing
```

and the `response.raw` can be unframed and decoded using `protoc`:

```
cat response.raw \
    | grpcto unframe \
    | protoc --decode_raw
```

`lipsum`
--------

Generate some placeholder text, beyond just `lorem ipsum`. It also does some
optional rate-limiting, printing one word at a time.

```
go install github.com/ripta/rt/cmd/lipsum@latest
```

`place`
------

Talk to macOS Location Services from the command line.

```
go install github.com/ripta/rt/cmd/place@latest
```

Query as plaintext:

```
❯ place
Latitude: 34.009414
Longitude: -118.162233
Accuracy: 45.751999
Last observed: 2022-02-02T21:24:40-08:00
```

or as JSON by giving `-j` or `--json`.

`streamdiff`
------------

Helps you pick out field changes off a stream of JSON.

```
go install github.com/ripta/rt/cmd/streamdiff@latest
```

It's technically usable  on any stream as long as the format is one JSON per
line.

It's convenient for viewing Kubernetes resource changes over time.

For example, you can start a watch (`-w`) on pods (`kubectl get pods`) and
pipe it to streamdiff. Most fields won't be printed, except when they change.
Consider this output:

```
❯ kubectl get pods -o json -w | streamdiff
T+23s Pod:pomerium-cache-6c9f84b747-cr2rx
  (1/2): spec.nodeName \ -> gke-vqjp-preemptible-065-38c45f41-wtnb
  (2/2): status.conditions \ -> [map[lastProbeTime:<nil> lastTransitionTime:2023-06-22T06:27:43Z status:True type:PodScheduled]]

T+24s Pod:pomerium-cache-6c9f84b747-cr2rx
  (1/6): status.conditions.0 \ -> map[lastProbeTime:<nil> lastTransitionTime:2023-06-22T06:27:43Z status:True type:Initialized]
  (2/6): status.conditions.1 \ -> map[lastProbeTime:<nil> lastTransitionTime:2023-06-22T06:27:43Z message:containers with unready status: [cache] reason:ContainersNotReady status:False type:Ready]
  (3/6): status.conditions.2 \ -> map[lastProbeTime:<nil> lastTransitionTime:2023-06-22T06:27:43Z message:containers with unready status: [cache] reason:ContainersNotReady status:False type:ContainersReady]
  (4/6): status.startTime \ -> 2023-06-22T06:27:43Z
  (5/6): status.containerStatuses \ -> [map[image:us.gcr.io/dc-02/gke-vqjp/pomerium-cache:v1.0.23.1390 imageID: lastState:map[] name:cache ready:false restartCount:0 started:false state:map[waiting:map[reason:ContainerCreating]]]]
  (6/6): status.hostIP \ -> 10.52.0.34

T+26s Pod:pomerium-cache-6c9f84b747-cr2rx
  (1/8): status.containerStatuses.0.ready false -> true
  (2/8): status.containerStatuses.0.started false -> true
  (3/8): status.containerStatuses.0.state.waiting map[reason:ContainerCreating] -> \
  (4/8): status.containerStatuses.0.state.running \ -> map[startedAt:2023-06-22T06:27:46Z]
  (5/8): status.containerStatuses.0.containerID \ -> containerd://293972feb5b498c80a585137299990c77f44ea46d6236432aba08e72108c35dc
  (6/8): status.phase Pending -> Running
  (7/8): status.podIP \ -> 10.53.1.92
  (8/8): status.podIPs \ -> [map[ip:10.53.1.92]]
```

While there is still some noise, it clearly shows when the pod was assigned to
a node, when the pod finished initializing, and when it changed phases from
Pending to Running.

In addition to a running log (as above), you can also run `streamdiff -i`,
which updates status on the same line instead of printing a new line for
every resource update. YMMV.

```
❯ kubectl get nodes -o json -w | streamdiff -i
\ Node:gke-vqjp-ondemand-370-504f82ce-r0d8	status.conditions.0.{type: FrequentContainerdRestart; status: True -> False} 
\ Node:gke-vqjp-preemptible-065-38c45f41-kvjd	status.conditions.0.lastHeartbeatTime: 2023-06-22T06:44:18Z -> 2023-06-22T06:49:19Z
| Node:gke-vqjp-preemptible-065-38c45f41-pklf	status.conditions.0.lastHeartbeatTime: 2023-06-22T06:44:15Z -> 2023-06-22T06:49:16Z
/ Node:gke-vqjp-preemptible-065-38c45f41-wtnb	status.conditions.0.lastHeartbeatTime: 2023-06-22T06:45:05Z -> 2023-06-22T06:50:11Z
```


`structfiles` (`sf`)
--------------------

Proof of concept tool to examine and compare a pile of structured files (e.g.,
Kubernetes manifests) strewn across multiple directories or files, with any
number of documents per file.

Supports YAML, JSON, TOML, HCLv2, GOB, CSV, MessagePack, and EDN as input and
output, with some caveats:

- HCLv2 output is experimental, due to the way that HCLv2 is schema-driven and
  the lack of a way to represent the schema in structfiles.
- CSV does not support nested maps. CSV treats each row as a separate document.
  The first row of a CSV file is assumed to be the header.
- YAML, JSON, and GOB support multiple documents in one stream.
- EDN decoding forces stringification of map keys, and does not yet support the
  entire EDN spec, e.g., `{:foo #{a 2}}` still trips up the converter.
- Logfmt does not support nested maps. Each log line is treated as a separate 
  document.

Resulting diff currently only in unified diff of YAML (see example).

```
go install github.com/ripta/rt/cmd/sf@latest
```

For a list of supported formats and format-specific options, run `sf formats`:

```
FORMAT    EXTENSIONS      INPUT   OPTIONS      OUTPUT   OPTIONS
csv       .csv            yes     sep:string   yes      sep:string
edn       .edn            yes     -            yes      indent:int prefix:string
gob       .gob            yes     -            yes      -
hcl2      .hcl            yes     -            yes      -
json      .json           yes     -            yes      indent:int no_indent:bool
logfmt    .logfmt         yes     -            yes      -
msgpack   .mpk .msgpack   yes     -            yes      -
toml      .toml           yes     -            yes      indent:int
yaml      .yml .yaml      yes     -            yes      indent:int
```

The simplest subcommand is `eval`, which reads one or more files and prints
the data back out, like a pretty-printer. The default format is JSON with
an indentation of 2 spaces.

```
❯ cat $dangit
{"foo":
"bar"}

❯ sf eval $dangit
{
  "foo": "bar"
}

❯ sf eval -f json $dangit
{
  "foo": "bar"
}

❯ sf eval -f json -o no_indent=true $dangit
{"foo":"bar"}
```

Of course, you can use it to convert between formats by specifying the desired
output format:

```
❯ cat $nabbit
{"foo":[1,2,"bar"]}

❯ sf eval -f toml $nabbit
foo = [1.0, 2.0, "bar"]

❯ rt sf eval -f hcl2 $nabbit
foo = [1, 2, "bar"]
```

Some formats may expect different shape data though:

```
❯ rt sf eval -f csv $nabbit
Error: interface conversion: interface {} is []interface {}, not string
```

As a special case, you can also read from STDIN by specifying `stdin://` (or `-`),
which assumes JSON or YAML. To optionally control the format parser, use `stdin://FORMAT`.

```
❯ mj foo=bar | sf eval -f yaml -
---
foo: bar

❯ mj foo=bar | sf eval -f yaml stdin://json
---
foo: bar

❯ generate-gob | sf eval -f json stdin://gob
{"vals":[1,2,3]}
```

For a more advanced example, compare two directories of Kubernetes manifests
containing all-in-one  manifests (`foo_aio`) and one-resource-per-file
(`foo_each`), using `-k`:

```
❯ sf diff -k ./samples/manifests/foo_aio ./samples/manifests/foo_each
--- ./samples/manifests/foo_aio
+++ ./samples/manifests/foo_each
@@ -25,7 +25,7 @@
             "name": "web",
             "ports": [
               {
-                "containerPort": 80
+                "containerPort": 8080
               }
             ]
           }
@@ -60,7 +60,7 @@
         }
       }
     },
-    "schedule": "*/1 * * * *"
+    "schedule": "* * * * *"
   }
 }
 {
@@ -73,8 +73,8 @@
   "spec": {
     "ports": [
       {
-        "port": 80,
+        "port": 8080,
-        "targetPort": 80
+        "targetPort": 8080
       }
     ],
     "selector": {
```

You can diff multiple files against one file by using the `::` delimiter. Arguments
before the delimiter are taken as one input, while arguments after are taken as
the second input to the diff:

```
❯ sf diff -k ./samples/manifests/foo_aio :: ./samples/manifests/foo_each/*.yaml
```

You can compare piles of structured files of differing formats and control the
output format being diffed with `-f`

```
❯ sf diff -f json ./samples/configs/yaml_each ./samples/configs/toml
--- ./samples/configs/yaml_each
+++ ./samples/configs/toml
@@ -32,7 +32,7 @@
       "role": "backend"
     }
   },
-  "title": "YAML Example One"
+  "title": "TOML Example One"
 }
 {
   "autoscaling_rules": [
@@ -68,5 +68,5 @@
       "role": "frontend"
     }
   },
-  "title": "YAML Example Two"
+  "title": "TOML Example Two"
 }
```

For tab-delimited output, use the CSV format and set the separator to
tab: `-f csv -o sep=$'\t'`


`toto`
------

Some dynamic protobuf inspection tools.

```
go install github.com/ripta/rt/cmd/toto@latest
```

You can build file descriptor set, and use protoc to inspect it:

```
toto compile samples
cat samples/.file_descriptor_set | protoc --decode_raw
```

Or generate an example protobuf message and dynamically convert it to json:

```
toto sample | toto recode -p samples/.file_descriptor_set -f json samples.data.v1.Envelope
```

The `toto compile` step is necessary, because you can't currently parse proto
files directly in go (or at least, I wasn't able to).

`uni`
-----

Unicode-related stuff.

```
# For a smaller installation, excluding the Unicode Han Database:
go install github.com/ripta/rt/cmd/uni@latest

# To include Unicode Han Database, which adds about 25MB to the binary:
go install -tags unihan github.com/ripta/rt/cmd/uni@latest
```

Size comparison:

```
❯ stat -f '%z %N' uni unihan
 6572386 uni
34410114 unihan
```

List characters:

```
❯ uni list java cecak
U+A981 	ꦁ	[EA A6 81   ]	<M,Mn>	JAVANESE SIGN CECAK
U+A9B3 	꦳	[EA A6 B3   ]	<M,Mn>	JAVANESE SIGN CECAK TELU
```

List characters with fewer details:

```
❯ uni list java cecak -o hexbytes,name
[EA A6 81   ]	JAVANESE SIGN CECAK
[EA A6 B3   ]	JAVANESE SIGN CECAK TELU
```

Show only the aggregate count (`-c`), skipping output (`-o none`):

```
❯ uni list java cecak -o none -c
Matched 2 runes
```

Show only characters in a specific character category, e.g.:

```
# All "Pd" (punctuation, dash)
❯ uni list -C Pd

# All "S" (symbols)
❯ uni list -C S

# All "N" (numbers) that aren't "No" (other)
❯ uni list -C N,!No

# All "Lu" (letters, uppercase) and "Ll" (letters, lowercase)
❯ uni list -C Lu,Ll

# All Cyrillic uppercase and lowercase letters (i.e., excluding modifiers and subscripts)
❯ uni list -C Lu,Ll cyrillic

# All iotified Cyrillic letters not containing 'small'
❯ uni list cyrillic iotified !small
```

Show only characters in a specific script, e.g.:

```
# All Sundanese characters, by codepoint name:
❯ uni list sundanese

# All Sundanese characters, by script name, which needs the --all flag:
❯ uni list -S Sundanese --all
```

Show only certain codepoints by character or codepoint:

```
# All lowercase ASCII characters:
❯ uni list -r a-z

# Uppercase A-G and lowercase a-g ASCII characters:
❯ uni list -r A-G,a-g

# Special characters from colon (codepoint 3A) to at sign (codepoint 40):
❯ uni list -r u+3a-40

# Emojis between 🤤 and 🤗 (order does not matter):
❯ uni list -r 🤤-🤗
❯ uni list -r 🤗-🤤

# Combine filters: emojis between 🤤 and 🤗 whose name includes "hand":
❯ uni list -r 🤤-🤗 hand
```

Don't forget to escape `!` in your shell if necessary.

List all character categories, their names, and counts:

```
❯ uni cats
KEY   NAME                    RUNE COUNT
C     Other                   139751
Cc    Control                 65
Cf    Format                  170
Co    Private Use             137468
[...]
```

List all scripts and counts:

```
❯ uni scripts
NAME                     RUNE COUNT
Adlam                    88
Ahom                     65
Anatolian_Hieroglyphs    583
[...]
```

Describe characters:

```
❯ echo 𝗀𝘨| uni describe
U+1D5C0 𝗀       [F0 9D 97 80]   <L,Ll>  MATHEMATICAL SANS-SERIF SMALL G
U+1D628 𝘨       [F0 9D 98 A8]   <L,Ll>  MATHEMATICAL SANS-SERIF ITALIC SMALL G
U+000A  "\n"    [0A         ]   <C,Cc>  <control>
```

Map characters for fun:

```
❯ echo Hello World | uni map smallcaps
Hᴇʟʟᴏ Wᴏʀʟᴅ

❯ echo Hello World | uni map italics
𝐻𝑒𝑙𝑙𝑜 𝑊𝑜𝑟𝑙𝑑
```

Canonically compose runes:

```
❯ echo 감 | uni nfc
감

❯ echo 감 | uni nfd
감
```

Sometimes it may be useful to decompose runes before describing:

```
❯ echo 쭈꾸쭈꾸 | uni d
U+CB48  쭈      [EC AD 88   ]   <L,Lo>  <Hangul Syllable>
U+AFB8  꾸      [EA BE B8   ]   <L,Lo>  <Hangul Syllable>
U+CB48  쭈      [EC AD 88   ]   <L,Lo>  <Hangul Syllable>
U+AFB8  꾸      [EA BE B8   ]   <L,Lo>  <Hangul Syllable>
U+000A  "\n"    [0A         ]   <C,Cc>  <control>

❯ echo 쭈꾸쭈꾸 | uni nfd | uni describe
U+110D  ᄍ      [E1 84 8D   ]   <L,Lo>  HANGUL CHOSEONG SSANGCIEUC
U+116E          [E1 85 AE   ]   <L,Lo>  HANGUL JUNGSEONG U
U+1101  ᄁ      [E1 84 81   ]   <L,Lo>  HANGUL CHOSEONG SSANGKIYEOK
U+116E          [E1 85 AE   ]   <L,Lo>  HANGUL JUNGSEONG U
U+110D  ᄍ      [E1 84 8D   ]   <L,Lo>  HANGUL CHOSEONG SSANGCIEUC
U+116E          [E1 85 AE   ]   <L,Lo>  HANGUL JUNGSEONG U
U+1101  ᄁ      [E1 84 81   ]   <L,Lo>  HANGUL CHOSEONG SSANGKIYEOK
U+116E          [E1 85 AE   ]   <L,Lo>  HANGUL JUNGSEONG U
U+000A  "\n"    [0A         ]   <C,Cc>  <control>
```

Sort input with different collation (`-l`):

```
❯ cat input.txt
Œthelwald
Zeus
Achilles

❯ cat input.txt | uni sort -l en-US
Achilles
Œthelwald
Zeus

❯ cat input.txt | uni sort -l da
Achilles
Zeus
Œthelwald

❯ cat input.txt | uni sort -l da -r
Œthelwald
Zeus
Achilles
```


`yfmt`
------

Reindent YAML while preserving comments.

```
go install github.com/ripta/rt/cmd/yfmt@latest
```

This tool treats comments as nodes and therefore will _not_ preserve comment
indentation. For example:

```
❯ cat in.yaml
# does this work?
foo:
   - 123   # I hope
           # maybe
   - 456

❯ yfmt < in.yaml
# does this work?
foo:
  - 123 # I hope
  # maybe
  - 456
```
