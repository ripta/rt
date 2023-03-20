Ripta's collection of tools

Tools:

* [enc](#enc) to encode and decode strings
* [grpcto](#grpcto) to frame and unframe gRPC messages
* [place](#place) for macOS Location Services
* [toto](#toto) to inspect some protobuf messages
* [uni](#uni) for unicode utils

`enc`
----

```
go install github.com/ripta/rt/cmd/enc@latest
```

Encode and decode strings using various encodings:

* `a85` for ascii85;
* `b32` for base32;
* `b58` for base58;
* `b64` for base64; and
* `hex` for hexadecimal.

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

```
go install github.com/ripta/rt/cmd/uni@latest
```

List characters:

```
❯ uni list java cecak
U+A981 	ꦁ	JAVANESE SIGN CECAK
U+A9B3 	꦳	JAVANESE SIGN CECAK TELU
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
