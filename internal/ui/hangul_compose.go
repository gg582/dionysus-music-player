package ui

import (
	"bytes"

	hangul "github.com/gg582/hangul-logotype/hangul"
	"golang.org/x/text/unicode/norm"
)

// compatibilityJamo reports whether r is a Hangul Compatibility Jamo character
// (the form macOS GTK3 IME emits when composition is broken).  The filler
// U+3164 is intentionally excluded because it is not a meaningful input key.
func compatibilityJamo(r rune) bool {
	return (r >= 0x3131 && r <= 0x314E) || (r >= 0x314F && r <= 0x3163)
}

// composeHangulJamos converts runs of compatibility jamo into precomposed
// Hangul syllables using hangul-logotype.  Latin letters, digits, symbols and
// existing composed Hangul are passed through unchanged so English file names
// or URLs are never transliterated.
func composeHangulJamos(input string) string {
	input = norm.NFC.String(input)
	var out bytes.Buffer
	var jamoBuf []rune

	flush := func() {
		if len(jamoBuf) == 0 {
			return
		}
		typer := hangul.NewLogoTyper()
		typer.WriteRunes(jamoBuf)
		out.Write(typer.Result())
		jamoBuf = jamoBuf[:0]
	}

	for _, r := range input {
		if compatibilityJamo(r) {
			jamoBuf = append(jamoBuf, r)
			continue
		}
		flush()
		out.WriteRune(r)
	}
	flush()
	return out.String()
}
