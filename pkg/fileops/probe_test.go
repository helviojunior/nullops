package fileops

import (
	"strings"
	"testing"
)

func TestDetectMime(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		text bool
		mime string
	}{
		{"empty", nil, true, "text/plain"},
		{"plain", []byte("Write-Host \"hi\"\r\n"), true, "text/plain"},
		{"utf8", []byte("ação çãõ 日本語\n"), true, "text/plain"},
		{"html", []byte("<!doctype html><p>x</p>"), true, "text/html"},
		{"png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), false, "image/png"},
		{"pdf", []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n"), false, "application/pdf"},
		{"zip", []byte("PK\x03\x04\x14\x00\x00\x00"), false, "application/zip"},
		{"nul", []byte("abc\x00def"), false, "application/octet-stream"},
		{"utf16", []byte("\xff\xfeW\x00r\x00i\x00"), true, "text/plain"},
		// text start, binary after the first 512 bytes
		{"late-binary", append([]byte(strings.Repeat("a", 600)), 0, 1, 2), false, "application/octet-stream"},
		// multi-byte char cut at the 4096 boundary
		{"cut-rune", append([]byte(strings.Repeat("a", 4094)), 0xc3), true, "text/plain"},
	}
	for _, c := range cases {
		mime, text := DetectMime(c.data)
		if text != c.text || !strings.HasPrefix(mime, c.mime) {
			t.Errorf("%s: got (%q, %v), want (%q*, %v)", c.name, mime, text, c.mime, c.text)
		}
	}
}

func TestProbeReadsOnlyHead(t *testing.T) {
	fs, _ := newFS(t)
	big := strings.Repeat("line of text\n", 1000) // 13 KB of text
	fs.WriteText("big.log", big)
	p, err := fs.Probe("big.log")
	if err != nil || !p.IsText || p.Size != int64(len(big)) {
		t.Fatalf("probe: %+v %v", p, err)
	}
	if _, err := fs.Probe("../etc/passwd"); err == nil {
		t.Fatal("probe outside root")
	}
}
