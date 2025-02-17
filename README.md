rt: Ripta's collection of tools

Expectations:

- tools read from STDIN, write to STDOUT, and hopefully print errors to STDERR;
- tools are meant to be combined with others, e.g., `hs` might be less useful
  to you, because it prints file hashes in binary output instead of hex (but
  `enc hex` converts it to hex strings).

You can install the all-in-one hyperbinary:

```
go install github.com/ripta/rt/hypercmd/rt@latest
```

or install all tools as individual binaries:

```
go install github.com/ripta/rt/cmd/...@latest
```

or pick-and-choose each tool to individually install:

* [enc](#enc) to encode and decode STDIN
* [grpcto](#grpcto) to frame and unframe gRPC messages
* [hs](#hs) to hash STDIN
* [place](#place) for macOS Location Services
* [streamdiff](#streamdiff) to help you pick out field changes off a stream of JSON
* [structfiles](#structfiles) to examine and compare a pile of structured files
* [toto](#toto) to inspect some protobuf messages
* [uni](#uni) for unicode utils
* [yfmt](#yfmt) to reindent YAML while preserving comments

Pull requests welcome, though you should probably check first before sinking any time.



`enc`
----

```
go install github.com/ripta/rt/cmd/enc@latest
```

Encode and decode strings using various encodings:

* `a85` for ascii85;
* `b32` for base32;
* `b58` for base58;
* `b64` for base64;
* `hex` for hexadecimal; and
* `url` for URL escape/unescape.


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

Supports YAML and JSON as input.

Resulting diff currently only in unified diff of YAML (see example).

```
go install github.com/ripta/rt/cmd/sf@latest
```

For example, compare two directories of Kubernetes manifests containing all-in-one
manifests (`foo_aio`) and one-resource-per-file (`foo_each`):

```
❯ structfiles diff ./samples/manifests/foo_aio ./samples/manifests/foo_each
--- before
+++ after
@@ -17,7 +17,7 @@
         - image: nginx:latest
           name: web
           ports:
-            - containerPort: 80
+            - containerPort: 8080
 ---
 apiVersion: batch/v1beta1
 kind: CronJob
@@ -36,7 +36,7 @@
               - date; echo "Hello, World!"
             image: ubuntu:latest
             name: web
-  schedule: '*/1 * * * *'
+  schedule: '* * * * *'
 ---
 apiVersion: v1
 kind: Service
@@ -45,7 +45,7 @@
   namespace: foo
 spec:
   ports:
-    - port: 80
+    - port: 8080
-      targetPort: 80
+      targetPort: 8080
   selector:
     app: bar
```

You can diff multiple files against one file by using the `::` delimiter. Arguments
before the delimiter are taken as one input, while arguments after are taken as
the second input to the diff:

```
❯ structfiles diff ./samples/manifests/foo_aio :: ./samples/manifests/foo_each/*.yaml
```


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
go install github.com/ripta/rt/cmd/uni@latest
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
U+1D5C0	𝗀	MATHEMATICAL SANS-SERIF SMALL G
U+1D628	𝘨	MATHEMATICAL SANS-SERIF ITALIC SMALL G
U+000A	"\n"	<control>
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
U+CB48	쭈	<Hangul Syllable>
U+AFB8	꾸	<Hangul Syllable>
U+CB48	쭈	<Hangul Syllable>
U+AFB8	꾸	<Hangul Syllable>
U+000A	"\n"	<control>

❯ echo 쭈꾸쭈꾸 | uni nfd | uni describe
U+110D	ᄍ	HANGUL CHOSEONG SSANGCIEUC
U+116E	ᅮ	HANGUL JUNGSEONG U
U+1101	ᄁ	HANGUL CHOSEONG SSANGKIYEOK
U+116E	ᅮ	HANGUL JUNGSEONG U
U+110D	ᄍ	HANGUL CHOSEONG SSANGCIEUC
U+116E	ᅮ	HANGUL JUNGSEONG U
U+1101	ᄁ	HANGUL CHOSEONG SSANGKIYEOK
U+116E	ᅮ	HANGUL JUNGSEONG U
U+000A	"\n"	<control>
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
