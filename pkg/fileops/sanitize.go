package fileops

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxNameBytes keeps room for the "-EPOCH-n" suffix of unique uploads under
// the usual 255-byte filesystem limit.
const MaxNameBytes = 200

// invisible reports runes that must never appear in a file name: control
// characters (including newlines, which break logs and listings), bidi
// overrides/isolates (used to disguise "fdp.exe" as "exe.pdf"), zero-width
// characters and other format characters.
func invisible(r rune) bool {
	switch {
	case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f, r == 0x061c:
		return true
	case r >= 0x200b && r <= 0x200d, r == 0x2060, r == 0xfeff:
		return true
	case r == utf8.RuneError:
		return true
	}
	return unicode.Is(unicode.Cf, r)
}

// windowsReserved are device names Windows refuses (with any extension).
var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

func reservedBase(name string) bool {
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	return windowsReserved[strings.ToUpper(strings.TrimSpace(base))]
}

// SanitizeName turns an untrusted upload file name into a safe one (it never
// fails): path components are dropped, invisible characters removed, the
// characters Windows forbids replaced, leading dots/spaces and trailing
// dots/spaces trimmed (no hidden files), reserved device names prefixed, and
// the result capped at MaxNameBytes keeping the extension.
func SanitizeName(name string) string {
	name = strings.ToValidUTF8(name, "")
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case invisible(r):
			continue
		case strings.ContainsRune(`<>:"|?*`, r):
			b.WriteRune('_')
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	name = strings.Join(strings.Fields(b.String()), " ")
	name = strings.TrimLeft(name, ". ")
	name = strings.TrimRight(name, ". ")
	if name == "" {
		name = "upload"
	}
	if reservedBase(name) {
		name = "_" + name
	}
	return truncateName(name, MaxNameBytes)
}

// truncateName cuts name to max bytes on a rune boundary, keeping a short
// extension.
func truncateName(name string, max int) string {
	if len(name) <= max {
		return name
	}
	ext := ""
	if i := strings.LastIndexByte(name, '.'); i > 0 && len(name)-i <= 16 {
		ext = name[i:]
	}
	stem := name[:len(name)-len(ext)]
	limit := max - len(ext)
	for len(stem) > limit {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return stem + ext
}

// unsafeNameChars reports names a user may not create or rename to.
func unsafeNameChars(name string) bool {
	for _, r := range name {
		if invisible(r) {
			return true
		}
	}
	return !utf8.ValidString(name)
}
