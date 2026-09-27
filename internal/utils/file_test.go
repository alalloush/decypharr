package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestShortenNameFitsNameMax(t *testing.T) {
	cyrillic := strings.Repeat("Перси Джексон и Олимпийцы - ", 8) + "[2025, WEB-DL 1080p]"
	cjk := strings.Repeat("海賊王", 90) // 810 bytes; 3-byte runes straddle every cut
	for name, in := range map[string]string{
		"ascii one over": strings.Repeat("a", NameMax+1),
		"cyrillic":       cyrillic,
		"cjk":            cjk,
	} {
		t.Run(name, func(t *testing.T) {
			out := ShortenName(in)
			if len(out) > NameMax {
				t.Fatalf("len = %d, want <= %d", len(out), NameMax)
			}
			if !utf8.ValidString(out) {
				t.Fatalf("cut split a rune: %q", out)
			}
			stem := out[:strings.LastIndexByte(out, '~')]
			if !strings.HasPrefix(in, stem) {
				t.Fatalf("%q is not a prefix of the input", stem)
			}
			if again := ShortenName(in); again != out {
				t.Fatalf("not deterministic: %q then %q", out, again)
			}
			// The point of the exercise: the filesystem accepts it.
			if err := os.Mkdir(filepath.Join(t.TempDir(), out), 0o755); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestShortenNameKeepsNamesThatFit(t *testing.T) {
	for _, in := range []string{"Show.S01E01.1080p", strings.Repeat("b", NameMax), strings.Repeat("д", NameMax/2)} {
		if out := ShortenName(in); out != in {
			t.Errorf("ShortenName changed a %d-byte name to %q", len(in), out)
		}
		if out := ShortenFileName(in + ".mkv"); len(in)+4 <= NameMax && out != in+".mkv" {
			t.Errorf("ShortenFileName changed a %d-byte name to %q", len(in)+4, out)
		}
	}
}

// Two releases that differ only past the cut (another dub, another group)
// must not share a folder: deleting one would remove the other's files.
func TestShortenNameKeepsLongNamesApart(t *testing.T) {
	prefix := strings.Repeat("Сериал - Series - Сезон 1 - ", 10)
	a := ShortenName(prefix + "MVO (Studio A)")
	b := ShortenName(prefix + "MVO (Studio B)")
	if a == b {
		t.Fatalf("both names shortened to %q", a)
	}
}

func TestShortenFileNameKeepsExtension(t *testing.T) {
	in := strings.Repeat("Фильм ", 60) + "2025 WEB-DL 1080p.mkv"
	out := ShortenFileName(in)
	if len(out) > NameMax || !utf8.ValidString(out) {
		t.Fatalf("len = %d, valid UTF-8 = %v", len(out), utf8.ValidString(out))
	}
	if !strings.HasSuffix(out, ".mkv") {
		t.Fatalf("extension lost: %q", out)
	}

	// A long tail after the last dot is not an extension to preserve.
	dotted := strings.Repeat("x", 200) + "." + strings.Repeat("y", 100)
	if out := ShortenFileName(dotted); len(out) > NameMax {
		t.Fatalf("len = %d, want <= %d", len(out), NameMax)
	}
}
