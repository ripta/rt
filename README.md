Ripta's collection of tools

`uni`
-----

```
go install github.com/ripta/rt/cmd/uni
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
