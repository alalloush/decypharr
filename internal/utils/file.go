package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// NameMax is the longest path component, in bytes, that ext4, xfs, btrfs and
// most other Linux filesystems accept (NAME_MAX). Release names written in
// two- and three-byte scripts (Cyrillic, CJK) often exceed it.
const NameMax = 255

// maxKeptExtension bounds what ShortenFileName treats as an extension. A
// longer suffix after the last dot is the tail of a dotted release name.
const maxKeptExtension = 16

// ShortenName fits a directory name into NameMax bytes; see shortenName.
func ShortenName(name string) string {
	return shortenName(name, "")
}

// ShortenFileName fits a file name into NameMax bytes and keeps its extension,
// so a shortened media file is still recognized by its ".mkv".
func ShortenFileName(name string) string {
	ext := filepath.Ext(name)
	if len(ext) > maxKeptExtension {
		ext = ""
	}
	return shortenName(strings.TrimSuffix(name, ext), ext)
}

// shortenName returns stem+ext unchanged when it fits in NameMax bytes.
// Otherwise stem is cut on a UTF-8 boundary and followed by "~" and eight hex
// digits of a hash of the full name, then ext. The hash keeps two long names
// that share a prefix on different paths, so removing one entry's folder never
// removes another's, and the same name always maps to the same result.
func shortenName(stem, ext string) string {
	name := stem + ext
	if len(name) <= NameMax {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	tag := "~" + hex.EncodeToString(sum[:4])
	cut := NameMax - len(tag) - len(ext)
	for cut > 0 && !utf8.RuneStart(stem[cut]) {
		cut--
	}
	return stem[:cut] + tag + ext
}

func PathUnescape(path string) string {
	// try to use url.PathUnescape
	if unescaped, err := url.PathUnescape(path); err == nil {
		return unescaped
	}

	// unescape %
	unescapedPath := strings.ReplaceAll(path, "%25", "%")

	// add others

	return unescapedPath
}

func FormatSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	var size float64
	var unit string

	switch {
	case bytes >= TB:
		size = float64(bytes) / TB
		unit = "TB"
	case bytes >= GB:
		size = float64(bytes) / GB
		unit = "GB"
	case bytes >= MB:
		size = float64(bytes) / MB
		unit = "MB"
	case bytes >= KB:
		size = float64(bytes) / KB
		unit = "KB"
	default:
		size = float64(bytes)
		unit = "bytes"
	}

	// Format to 2 decimal places for larger units, no decimals for bytes
	if unit == "bytes" {
		return fmt.Sprintf("%.0f %s", size, unit)
	}
	return fmt.Sprintf("%.2f %s", size, unit)
}
