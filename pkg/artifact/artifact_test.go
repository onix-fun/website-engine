package artifact

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildZip writes entries into a temporary zip file. Symlinks are created by
// setting the mode bit; the content is the link target, matching git zips.
func buildZip(t *testing.T, dir string, entries []zip.FileHeader, contents [][]byte) string {
	t.Helper()
	path := filepath.Join(dir, "artifact.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for i := range entries {
		head := entries[i]
		head.Method = zip.Deflate
		w, err := zw.CreateHeader(&head)
		if err != nil {
			t.Fatalf("create entry %q: %v", head.Name, err)
		}
		if _, err := w.Write(contents[i]); err != nil {
			t.Fatalf("write entry %q: %v", head.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return path
}

func header(name string) zip.FileHeader {
	var h zip.FileHeader
	h.Name = name
	h.SetMode(0o644)
	return h
}

func dirHeader(name string) zip.FileHeader {
	var h zip.FileHeader
	h.Name = name
	h.SetMode(os.ModeDir | 0o755)
	return h
}

func symlinkHeader(name string) zip.FileHeader {
	var h zip.FileHeader
	h.Name = name
	h.SetMode(os.ModeSymlink | 0o777)
	return h
}

func TestExtractValidArchive(t *testing.T) {
	src := buildZip(t, t.TempDir(),
		[]zip.FileHeader{header("index.html"), dirHeader("assets/"), header("assets/app.js")},
		[][]byte{[]byte("<html><body>ok</body></html>"), nil, []byte("console.log('hi')")})
	out := t.TempDir()

	manifest, err := Extract(src, out, DefaultLimits())
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("expected 2 files in manifest, got %v", manifest.Files)
	}
	if manifest.Files[0] != "index.html" || manifest.Files[1] != "assets/app.js" {
		t.Fatalf("unexpected manifest order: %v", manifest.Files)
	}
	data, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil || !bytes.Contains(data, []byte("ok")) {
		t.Fatalf("index.html not extracted correctly: %v %q", err, data)
	}
	if _, err := os.Stat(filepath.Join(out, "assets", "app.js")); err != nil {
		t.Fatalf("nested file missing: %v", err)
	}
	if manifest.Size == 0 || len(manifest.SHA256) != 64 {
		t.Fatalf("manifest hash/size wrong: %d %q", manifest.Size, manifest.SHA256)
	}
	sum, size, err := FileSHA256(src)
	if err != nil || sum != manifest.SHA256 || size != manifest.Size {
		t.Fatalf("FileSHA256 mismatch: %q %d vs %q %d (err %v)", sum, size, manifest.SHA256, manifest.Size, err)
	}
}

func TestExtractRequiresIndexHTML(t *testing.T) {
	src := buildZip(t, t.TempDir(),
		[]zip.FileHeader{header("main.html")}, [][]byte{[]byte("<html></html>")})
	if _, err := Extract(src, t.TempDir(), DefaultLimits()); err == nil {
		t.Fatal("expected error for missing index.html")
	} else if !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractEmptyArchive(t *testing.T) {
	src := buildZip(t, t.TempDir(), nil, nil)
	if _, err := Extract(src, t.TempDir(), DefaultLimits()); err == nil {
		t.Fatal("expected error for empty archive")
	}
}

func TestExtractTraversalEntriesStaysInsideDir(t *testing.T) {
	cases := []string{"../escape.txt", "..\\..\\escape.txt", "sub/../../../escape.txt"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			root := filepath.Dir(out)
			src := buildZip(t, t.TempDir(),
				[]zip.FileHeader{header("index.html"), header(name)},
				[][]byte{[]byte("ok"), []byte("escaped")})
			_, err := Extract(src, out, DefaultLimits())
			if err != nil {
				t.Fatalf("extract with %q: %v", name, err)
			}
			// Nothing may have been written outside the staging directory.
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("read parent: %v", err)
			}
			for _, e := range entries {
				if strings.Contains(e.Name(), "escape") && e.Name() != filepath.Base(out) {
					t.Fatalf("traversal wrote %q outside the staging dir", e.Name())
				}
			}
			if _, err := os.Stat(filepath.Join(out, "escape.txt")); err != nil {
				t.Fatalf("expected remapped entry inside staging dir: %v", err)
			}
		})
	}
}

func TestExtractDuplicatePath(t *testing.T) {
	src := buildZip(t, t.TempDir(),
		[]zip.FileHeader{header("index.html"), header("a/../index.html")},
		[][]byte{[]byte("one"), []byte("two")})
	_, err := Extract(src, t.TempDir(), DefaultLimits())
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate path error, got %v", err)
	}
}

func TestExtractRejectsSymlinks(t *testing.T) {
	src := buildZip(t, t.TempDir(),
		[]zip.FileHeader{header("index.html"), symlinkHeader("link")},
		[][]byte{[]byte("ok"), []byte("/etc/passwd")})
	_, err := Extract(src, t.TempDir(), DefaultLimits())
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestExtractZipBombRatio(t *testing.T) {
	bomb := make([]byte, 8<<20)
	_, _ = rand.Read(bomb)
	for i := range bomb { // all zeros compress far beyond the ratio limit
		bomb[i] = 0
	}
	src := buildZip(t, t.TempDir(),
		[]zip.FileHeader{header("index.html"), header("payload")},
		[][]byte{[]byte("ok"), bomb})
	_, err := Extract(src, t.TempDir(), DefaultLimits())
	if err == nil || !strings.Contains(err.Error(), "compression ratio") {
		t.Fatalf("expected ratio rejection, got %v", err)
	}
}

func TestExtractLimits(t *testing.T) {
	t.Run("too many files", func(t *testing.T) {
		headers := []zip.FileHeader{header("index.html"), header("a"), header("b")}
		contents := [][]byte{[]byte("ok"), []byte("1"), []byte("2")}
		src := buildZip(t, t.TempDir(), headers, contents)
		limits := DefaultLimits()
		limits.MaxFiles = 2
		if _, err := Extract(src, t.TempDir(), limits); err == nil {
			t.Fatal("expected MaxFiles rejection")
		}
	})
	t.Run("single file too large", func(t *testing.T) {
		src := buildZip(t, t.TempDir(),
			[]zip.FileHeader{header("index.html"), header("big.bin")},
			[][]byte{[]byte("ok"), bytes.Repeat([]byte("x"), 4096)})
		limits := DefaultLimits()
		limits.MaxFileSize = 1024
		_, err := Extract(src, t.TempDir(), limits)
		if err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("expected size limit rejection, got %v", err)
		}
	})
	t.Run("uncompressed total too large", func(t *testing.T) {
		src := buildZip(t, t.TempDir(),
			[]zip.FileHeader{header("index.html"), header("a"), header("b")},
			[][]byte{[]byte("ok"), bytes.Repeat([]byte("y"), 2048), bytes.Repeat([]byte("z"), 2048)})
		limits := DefaultLimits()
		limits.MaxUncompressed = 4096
		if _, err := Extract(src, t.TempDir(), limits); err == nil {
			t.Fatal("expected uncompressed total rejection")
		}
	})
}

func TestExtractNotAZip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "broken.zip")
	if err := os.WriteFile(src, []byte("this is not a zip file"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Extract(src, t.TempDir(), DefaultLimits()); err == nil {
		t.Fatal("expected error for non-zip input")
	}
}
