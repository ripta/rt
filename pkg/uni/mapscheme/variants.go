package mapscheme

import "github.com/ripta/rt/pkg/uni/runerange"

func init() {
	BoldedUpperRange := runerange.FromRuneRange('𝐀', '𝐙')
	BoldedLowerRange := runerange.FromRuneRange('𝐚', '𝐳')
	registry["bolded"] = MustGenerateFromRuneRanges(
		ASCIIUpperLowerRange,
		runerange.CombineRuneRanges(BoldedUpperRange, BoldedLowerRange),
	)

	BoldFrakturUpperRange := runerange.FromRuneRange('𝕬', '𝖅')
	BoldFrakturLowerRange := runerange.FromRuneRange('𝖆', '𝖟')
	registry["bold-fraktur"] = MustGenerateFromRuneRanges(
		ASCIIUpperLowerRange,
		runerange.CombineRuneRanges(BoldFrakturUpperRange, BoldFrakturLowerRange),
	)

	registry["clapback"] = MustGenerateFromString(
		" ",
		"👏",
	)

	registry["double-struck"] = MustGenerateFromString(
		// C, H, N, P, Q, R, Z are not in order in the Unicode block
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"𝕒𝕓𝕔𝕕𝕖𝕗𝕘𝕙𝕚𝕛𝕜𝕝𝕞𝕟𝕠𝕡𝕢𝕣𝕤𝕥𝕦𝕧𝕨𝕩𝕪𝕫𝔸𝔹ℂ𝔻𝔼𝔽𝔾ℍ𝕀𝕁𝕂𝕃𝕄ℕ𝕆ℙℚℝ𝕊𝕋𝕌𝕍𝕎𝕏𝕐ℤ",
	)

	registry["fraktur"] = MustGenerateFromString(
		// H, I, R, Z, C are not in order in the Unicode block
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"𝔞𝔟𝔠𝔡𝔢𝔣𝔤𝔥𝔦𝔧𝔨𝔩𝔪𝔫𝔬𝔭𝔮𝔯𝔰𝔱𝔲𝔳𝔴𝔵𝔶𝔷𝔄𝔅ℭ𝔇𝔈𝔉𝔊ℌℑ𝔍𝔎𝔏𝔐𝔑𝔒𝔓𝔔ℜ𝔖𝔗𝔘𝔙𝔚𝔛𝔜ℨ",
	)

	ItalicsUpperRange := runerange.FromRuneRange('𝐴', '𝑍')
	ItalicsLowerRange := runerange.CombineRuneRanges(
		runerange.FromRuneRange('𝑎', '𝑔'),
		// U+1D455 is already fulfilled by U+210E (Planck constant symbol)
		runerange.FromRune('ℎ'),
		runerange.FromRuneRange('𝑖', '𝑧'),
	)
	registry["italics"] = MustGenerateFromRuneRanges(
		ASCIIUpperLowerRange,
		runerange.CombineRuneRanges(ItalicsUpperRange, ItalicsLowerRange),
	)

	MonospacedUpperRange := runerange.FromRuneRange('𝙰', '𝚉')
	MonospacedLowerRange := runerange.FromRuneRange('𝚊', '𝚣')
	registry["monospaced"] = MustGenerateFromRuneRanges(
		ASCIIUpperLowerRange,
		runerange.CombineRuneRanges(MonospacedUpperRange, MonospacedLowerRange),
	)

	registry["parenthesized"] = MustGenerateFromRuneRanges(
		ASCIIUpperLowerRange,
		runerange.CombineRuneRanges(
			runerange.FromRuneRange('🄐', '🄩'),
			runerange.FromRuneRange('⒜', '⒵'),
		),
	)

	// Canadian Aborigianl Syllabics do not actually correspond to Latin letters,
	// but some orthographically look similar to rounded Latin letters.
	//
	// See: https://en.wikipedia.org/wiki/Canadian_Aboriginal_Syllabics
	registry["rounded"] = MustGenerateFromString(
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"ᗩᗷᑕᗪEᖴGᕼIᒍKᒪᗰᑎOᑭᑫᖇᔕTᑌᐯᗯ᙭YᘔᗩᗷᑕᗪEᖴGᕼIᒍKᒪᗰᑎOᑭᑫᖇᔕTᑌᐯᗯ᙭Yᘔ",
	)

	registry["sans-serif"] = MustGenerateFromRuneRanges(
		ASCIIUpperLowerRange,
		runerange.CombineRuneRanges(
			runerange.FromRuneRange('𝖠', '𝖹'),
			runerange.FromRuneRange('𝖺', '𝗓'),
		),
	)

	registry["scream"] = MustGenerateFromString(
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"aáăắặằẳẵǎâấậầẩẫäǟȧǡạȁàảȃāąAÁĂẮẶẰẲẴǍÂẤẬẦẨẪÄǞȦǠẠȀÀẢȂĀĄ",
	)

	registry["script"] = MustGenerateFromString(
		// Capitals B, E, F, H, I, L, M, R are not in order
		// Miniscules e, g, o are also out of order
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"𝒶𝒷𝒸𝒹ℯ𝒻ℊ𝒽𝒾𝒿𝓀𝓁𝓂𝓃ℴ𝓅𝓆𝓇𝓈𝓉𝓊𝓋𝓌𝓍𝓎𝓏𝒜ℬ𝒞𝒟ℰℱ𝒢ℋℐ𝒥𝒦ℒℳ𝒩𝒪𝒫𝒬ℛ𝒮𝒯𝒰𝒱𝒲𝒳𝒴𝒵",
	)

	registry["smallcaps"] = MustGenerateFromString(
		// S, X, Q, F do not have small caps equivalents in Unicode
		"abcdefghijklmnopqrstuvwxyz",
		"ᴀʙᴄᴅᴇғɢʜɪᴊᴋʟᴍɴᴏᴘǫʀsᴛᴜᴠᴡxʏᴢ",
	)

	registry["subscript"] = MustGenerateFromString(
		// no codepoint assigned for: miniscule b, c, d, f, g, q, r.
		// codepoints provisional for: miniscule w, y, z (209D…209F), see 181-C35 (2024-11-07).
		// no codepoint assigned for capitals.
		"aehijklmnoprstuvx0123456789",
		"ₐₑₕᵢⱼₖₗₘₙₒᵖᵣₛₜᵤᵥₓ₀₁₂₃₄₅₆₇₈₉",
	)

	registry["superscript"] = MustGenerateFromString(
		// no codepoint assigned for: capitals X, Y, or Z.
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVW0123456789",
		"ᵃᵇᶜᵈᵉᶠᵍʰⁱʲᵏˡᵐⁿᵒᵖ𐞥ʳˢᵗᵘᵛʷˣʸᶻᴬᴮꟲᴰᴱꟳᴳᴴᴵᴶᴷᴸᴹᴺᴼᴾꟴᴿ*ᵀᵁⱽᵂ⁰¹²³⁴⁵⁶⁷⁸⁹",
	)

	registry["squared"] = MustGenerateFromRuneRanges(
		ASCIIUpperLowerRange,
		runerange.CombineRuneRanges(
			runerange.FromRuneRange('🄰', '🅉'),
			runerange.FromRuneRange('🄰', '🅉'),
		),
	)

	registry["unsquared"] = MustGenerateFromRuneRanges(
		ASCIIUpperLowerRange,
		runerange.CombineRuneRanges(
			runerange.FromRuneRange('🅰', '🆉'),
			runerange.FromRuneRange('🅰', '🆉'),
		),
	)

	registry["circled"] = MustGenerateFromString(
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		"ⓐⓑⓒⓓⓔⓕⓖⓗⓘⓙⓚⓛⓜⓝⓞⓟⓠⓡⓢⓣⓤⓥⓦⓧⓨⓩⒶⒷⒸⒹⒺⒻⒼⒽⒾⒿⓀⓁⓂⓃⓄⓅⓆⓇⓈⓉⓊⓋⓌⓍⓎⓏ⓪①②③④⑤⑥⑦⑧⑨",
	)

	registry["uncircled"] = MustGenerateFromRuneRanges(
		ASCIIAllRange,
		runerange.CombineRuneRanges(
			runerange.FromRuneRange('🅐', '🅩'),
			runerange.FromRuneRange('🅐', '🅩'),
			runerange.FromRune('⓿'), // zero is the only out of order one
			runerange.FromRuneRange('➊', '➒'),
		),
	)

	registry["upside-down"] = MustGenerateFromString(
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"ɐqɔpǝɟƃɥᴉɾʞlɯuodbɹsʇnʌʍxʎz∀qƆpƎℲפHIſʞ˥WNOԀQɹS┴∩ΛMX⅄Z",
	)

	registry["wide"] = MustGenerateFromRuneRanges(
		ASCIIUpperLowerRange,
		runerange.CombineRuneRanges(
			runerange.FromRuneRange('Ａ', 'Ｚ'),
			runerange.FromRuneRange('ａ', 'ｚ'),
		),
	)
}
