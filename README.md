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

* [calc](#calc) for arbitrary-precision arithmetic
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



`calc`
------

Arbitrary-precision calculator. Expressions can be passed as arguments, piped
via stdin, or entered in an interactive REPL.

```
❯ calc '355/113'
3.14159
```

In REPL mode, results are stored as `$N` variables for reuse:

```
❯ calc
calc: type an expression to calculate, ".help" for help, or ^D to exit
calc:000> 2**128
340282366920938463463374607431768211456
calc:001> $0 + 1
340282366920938463463374607431768211457
```

Use `-d` to control decimal places (default 30), and `-v` for verbose output
showing the internal construction:

```
❯ calc -d 50 '√2'
1.41421356237309504880168872420969807856967187537695

❯ calc -v '22/7'
calc:000/ Construction: Multiply(Inverse(Named("1", Int(1))), Multiply(Int(22), Inverse(Int(7))))
3.142857142857142857142857142857
```

Sessions can be saved and loaded with `.save` and `.load`.

Functions are called with C-style syntax, e.g. `sin(0)`, `log(8, 2)`, or
`atan2(1, 1)`. The library, also listed in the REPL with `.show functions`:

| Group | Functions |
|-------|-----------|
| Basic | `abs(x)` |
| Trigonometric | `sin(x)`, `cos(x)`, `tan(x)`, `asin(x)`, `acos(x)`, `atan(x)`, `atan2(y, x)` |
| Exponential and logarithmic | `exp(x)`, `ln(x)`, `log10(x)`, `log2(x)`, `log(x, base)` |
| Roots | `sqrt(x)`, `cbrt(x)` |
| Rounding | `floor(x)`, `ceil(x)`, `round(x)` |
| Comparison | `min(x, ...)`, `max(x, ...)` |
| Hyperbolic | `sinh(x)`, `cosh(x)`, `tanh(x)` |
| Combinatorial | `factorial(n)`, `gamma(x)` |

Trig functions take and return radians; work in degrees by converting
explicitly, e.g. `sin(45 * PI / 180)`.


`cg`
----

Run a command with `cg run` and annotate each output line with a stream
indicator: `O` for stdout, `E` for stderr, `I` for cg's own lifecycle messages.
At the end of the run, a one-line summary reports the exit code, wall duration,
and per-stream line counts. Bare `cg` is dispatch-only: it owns the `run`,
resolution, and `mcp` subcommands and no longer execs programs itself.

```
go install github.com/ripta/rt/cmd/cg@latest
```

```
❯ cg run -- echo hello
O: hello
I: Finished exitcode=0 in 2ms (out=1 err=0)

❯ cg run -- sh -c 'echo out; echo err >&2'
O: out
E: err
I: Finished exitcode=0 in 3ms (out=1 err=1)
```

The child's exit code propagates to the shell. If the child is killed by a
signal, the summary reports the signal number instead. SIGINT and SIGTERM are
forwarded to the child.

`-c` / `--capture` writes the child's stdout and stderr to `$TMPDIR/cg/<ID>/`
and appends a short run ID to the summary line. Resolution subcommands thread
the ID through follow-up calls:

```
❯ cg run -c -- sh -c 'echo out; echo err >&2'
I: Finished exitcode=0 in 3ms (out=1 err=1) id=Q3F9K2

❯ cg out Q3F9K2
/tmp/cg/Q3F9K2/stdout

❯ cg paths Q3F9K2
/tmp/cg/Q3F9K2/stdout
/tmp/cg/Q3F9K2/stderr

❯ rg -i FOO $(cg out Q3F9K2)
```

`cg ls` lists recent runs, most-recent-first; `cg ls -n N` overrides the
default cap of 20, and `-n 0` (or any negative value) lists everything.
`--state`, `--exit-code`, and `--since`/`--before` narrow the listing further;
all three are optional and compose with each other and with `--pool` (see
below for the full grammar). Capture never deletes anything; `cg prune` is the
explicit cleanup hook:

```
❯ cg ls
Q3F9K2  exit=0   3ms     sh -c 'echo out; echo err >&2'
M7P4QX  exit=42  2ms     sh -c 'exit 42'

❯ cg prune                  # keep the 50 most recent by mtime
❯ cg prune --keep 10
❯ cg prune --older-than 7d
❯ cg prune --dry-run
```

`cg run` takes the execution flags. `-v` / `--verbose` prefixes every line with a
timestamp and adds a started/finished preamble; `--format` controls the layout
using Go's `time.Format` syntax. `--buffered` defers child output until the
command finishes, grouping by stream. `--log-parse json|logfmt` reformats
structured log lines inline. cg flags must precede the command; everything after
the first positional or `--` is passed through to the child untouched.

`cg note` records free-form memos that outlive a single run. A note carries an
ID, a required message, an optional set of `key=value` tags, and a creation
timestamp. Notes persist under `$TMPDIR/cg/notes/` and are shared with the
`cg_note_*` MCP tools. A note written from the shell is visible to an agent, and
a note written by an agent is visible here.

`cg note add` takes the message as an argument or on stdin. Repeat `-k` to
attach tags. `cg note ls` lists notes newest-first; `-n` overrides the default
cap of 20, and `-k key` or `-k key=value` filters by tag. `cg note grep`
searches message bodies with `--text` (a fixed string) or `--pattern` (an RE2
regex). `cg note rm` deletes notes by ID.

```
❯ cg note add 'baseline suite green at HEAD' -k run=Q3F9K2 -k branch=cg5
D2BHTQ

❯ cg note add 'migration still running; hold re-runs'
68NBND

❯ cg note ls
68NBND  2026-07-13T02:18:15Z
  migration still running; hold re-runs

D2BHTQ  2026-07-13T02:18:15Z  branch=cg5 run=Q3F9K2
  baseline suite green at HEAD

❯ cg note ls -k run=Q3F9K2
D2BHTQ  2026-07-13T02:18:15Z  branch=cg5 run=Q3F9K2
  baseline suite green at HEAD

❯ cg note grep --text baseline
D2BHTQ  2026-07-13T02:18:15Z  branch=cg5 run=Q3F9K2
  baseline suite green at HEAD

❯ cg note rm D2BHTQ
D2BHTQ
```

`cg mcp` starts a stdio MCP server that exposes the capture-run model as native
tools, using the same on-disk storage the shell subcommands use — a run started
with `cg run -c` is visible to `cg_list`, and a run started by `cg_run` is
visible to `cg ls`. Register with Claude Code:

```
claude mcp add cg cg mcp
```

Or by hand in the MCP host config:

```json
{
  "mcpServers": {
    "cg": { "command": "cg", "args": ["mcp"] }
  }
}
```

The server registers fifteen tools:

| Tool | Purpose |
|------|---------|
| `cg_run` | Run a command with capture; returns metadata and head/tail excerpts. |
| `cg_run_many` | Run a flat pool of commands with a parallelism knob and a fail policy; returns a pool ID and a per-run summary. |
| `cg_list` | List recent runs, most-recent-first. |
| `cg_meta` | Return run state and metadata. |
| `cg_wait` | Block until a run finishes or a timeout elapses. |
| `cg_cancel` | Signal a run's process group, with optional escalation. |
| `cg_paths` | Return absolute paths for a run's stdout, stderr, and meta.json. |
| `cg_stdout` | Fetch captured stdout with byte limits and head/tail windowing. |
| `cg_stderr` | Fetch captured stderr with byte limits and head/tail windowing. |
| `cg_grep` | Search captured output and return matching lines. |
| `cg_prune` | Evict runs by count or age. |
| `cg_note_add` | Record a free-form note with an optional set of `key=value` tags. |
| `cg_note_list` | List notes newest-first, with an optional key filter and limit. |
| `cg_note_delete` | Delete a note by ID. |
| `cg_note_grep` | Search note bodies and return whole matching notes. |

The notes store is shared the same way capture runs are. A note written with
`cg note add` is visible to `cg_note_list`, and a note written by `cg_note_add`
is visible to `cg note ls`.

A non-zero child exit code is data, not an MCP error: `cg_run` returns
successfully with `exit_code: N` and the caller decides how to react.

Runs survive `cg mcp` restarts. Each `cg_run` hands the child to a small
detached supervisor process whose lifetime matches the run's. Restarting the
server does not kill or lose in-flight runs; a fresh server picks them up from
the run directory. One caveat: `cg_wait` keeps an in-process fast path only for
runs the current server started. After a restart, waits on pre-existing runs
fall back to filesystem polling. Same result, slightly coarser latency.

If a supervisor dies before recording the run's exit — a SIGKILL, say — the
run never gets its `meta.json`. Such a run lists as `abandoned` in `cg ls` and
in `cg_list`, which also accepts `state: abandoned` as a filter. `cg prune`
treats abandoned runs as evictable alongside finished ones. A run whose
supervisor still holds the run lock is live and is never pruned.

A run directory with no lock file, no pid file, and no `start.json` at all
carries no liveness signal whatsoever, which happens when a supervisor dies
before it can even acquire the lock. Such a run lists as `unknown` rather than
`running` in `cg ls` and in `cg_list`, which also accepts `state: unknown` as a
filter. When `start.json` is missing, both `cg ls` and `cg_list` fall back to
the run directory's mtime for an approximate elapsed time: `cg ls` marks it
with a `~` prefix, and `cg_list` sets `started_at_approx: true` alongside the
mtime-derived `started_at`.

`cg ls --state` and `cg_list`'s `state` input both take the same enum:
`all|finished|running|failed|abandoned|unknown`. `cg ls` defaults to `all`,
listing every state, and `cg_list` does too. **This is a change from
`cg_list`'s earlier default of `finished`** — a caller that didn't pass
`state` explicitly now sees running, failed, abandoned, and unknown rows it
didn't before; pass `state: "finished"` to keep the old behavior.

`--exit-code` on `cg ls` and `exit_code` on `cg_list` filter finished runs by
exit code: a bare `N` means equals, or prefix with `!=`, `>=`, `>`, `<`, or
`<=` for the other five comparisons. Runs with no exit code (running,
abandoned, unknown, start-failed) never match. A collapsed pool summary row
has no single exit code to compare — it always passes through untouched,
regardless of the filter; expand with `--pool any` (or a pool ID) to filter
individual member rows instead.

`--since`/`--before` on `cg ls` and `since`/`before` on `cg_list` filter by
start time. `since` is inclusive ("at or after"); `before` is exclusive
("strictly before"). TIME accepts a relative duration meaning ago (`4h`,
`7d`, the same grammar `cg prune --older-than` uses), a full RFC3339
timestamp, or a bare `YYYY-MM-DD` date interpreted as local-timezone
midnight. Unlike `--exit-code`, these bounds do apply to pool rows, using the
pool's own precise `started_at` — the exemption is specific to exit codes,
which pools genuinely don't have one of.

`cg_run_many` runs a flat pool of commands. Each argv in `commands` runs
`repeat` times, through at most `parallelism` workers. Parallelism defaults to
1, which executes runs in listed order with repeats consecutive. `on_error`
decides what a failure does to the rest of the pool: `continue` (default) runs
everything, `stop` schedules nothing new, and `kill` additionally cancels
in-flight runs. `cwd` and `env` are shared across the pool. `wait` and
`wait_timeout_ms` work as in `cg_run`; a timeout returns the partial summary
while the pool keeps running.

`cg_run_many` is not a workflow engine. There are no dependencies between runs,
no conditionals, and no per-run fallback; the calling agent is the control-flow
engine. There is also no `cg run-many` shell counterpart, since the shell
already has `xargs -P` and `make -j`.

The result is a summary: counts, the commands array echoed once, and one flat
record per run referencing its command by index. Failed runs carry tail
excerpts of both streams, sized by `excerpt_bytes` with `0` disabling them,
under a 16 KB pool-wide budget; failures past the budget carry
`excerpt_omitted: true` instead. Successful runs carry no excerpts. Skipped and
pending runs are visible in the summary; a pending run has no run ID yet.

A pool is one more ID in the run namespace: a directory under the capture root
holding `pool.json` and no stream files. Members are ordinary sibling run
directories, so `cg_meta`, `cg_stdout`, `cg_stderr`, and `cg_grep` work on any
member run ID from the summary. Scheduling lives in a small detached pool
supervisor, following the same pattern as single runs. A server restart
therefore loses nothing: in-flight runs finish, pending jobs still get
scheduled, and a fresh server's `cg_wait` on the pool ID aggregates via
polling. A SIGKILLed pool supervisor leaves an abandoned pool, listed and
evictable like an abandoned run.

The other tools understand pools. `cg_wait` on a pool ID blocks until the pool
finishes and returns the same summary as the sync call. `cg_list` and `cg ls`
collapse members behind one row per pool with state and counts; the `pool`
filter expands them: a pool ID lists that pool's members, `none` lists only
standalone runs, and `any` lists everything uncollapsed. `cg_meta` on a pool ID
returns the pool state and the manifest. `cg_prune` evicts a pool and its
members as one unit, and never a member from under a live pool. `cg_cancel`
accepts pool IDs: the default SIGTERM stops scheduling and lets in-flight runs
finish, while SIGINT additionally cancels them.

Every distinct command in a pool passes the approval gate below before anything
spawns. A denial fails the whole call with nothing started, and `repeat` does
not multiply prompts.

`cg_run` checks each command against an approval matcher before running it. The
default mode prompts for unmatched commands when the client supports elicitation,
and otherwise fails closed; `cg mcp --blindly-allow` skips the gate entirely.
Rules live in `~/.config/cg/approve.yaml` (global) and `.cg.yaml` /
`.cg/approve.yaml` / `.claude/cg.yaml` (project), merged at startup:

```yaml
version: 1
mode: enforce        # enforce (default), allow-all, or deny-all
deny:
  - regex: '^/tmp/'
    message: do not run executables from temporary directories
allow:
  - prefix: [go, test]
  - prefix: [./scripts/build.sh]
  - regex: '^/opt/foo/bin/[^ ]+(\s|$)'
```

In enforce mode the matcher checks deny rules, then allow rules, then restrict
rules, then prompts; deny always wins. Each rule matches by `exact` argv, `prefix`
tokens, `glob`, or `regex`. `argv[0]` is resolved to an absolute path before
matching. For `exact` and `prefix` rules the first token's shape decides how it
matches: a bare program name (`go`) matches the invoked basename however the
command was spelled, an absolute path pins the exact executable, and a relative
path (`./scripts/build.sh`) resolves against the project root. `glob` and `regex`
rules match the canonical absolute join by default and accept `as_basename: true`
to match the basename join instead; the shape inference covers `exact` and
`prefix`, so `as_basename` is rejected there. Shells and inline-code interpreters
are denied by default and cannot be re-allowed.

A `restrict` section adds a fourth tier below `allow`: a command that matches a
`restrict` rule but no `allow` rule is refused rather than prompted, so a policy
can enumerate the safe subset of a tool and fail the rest closed within that
scope. The tiers run `deny` > `allow` > `restrict` > `prompt`, so an `allow`
carves a command out of a `restrict` scope while an explicit `deny` still beats
everything. `restrict` rules take the same four kinds as `allow` and `deny` and
accept an optional `message`.

```yaml
allow:
  - prefix: [git, show]
  - prefix: [git, log]
restrict:
  - prefix: [git]
    message: only read-only git is permitted here
```

Here `git show` and `git log` run, every other `git` subcommand is refused with
the message, and a command outside the `git` scope still prompts.


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
