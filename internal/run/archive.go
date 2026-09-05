package run

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// pyPath normalizes a path the way pathlib.PurePosixPath does, because the
// raw-archive paths in runs.jsonl are Python's str(Path(...)) and the rows
// are compared byte for byte: "./runs_raw/" becomes "runs_raw", "a//b"
// becomes "a/b", "" becomes ".", while ".." segments are kept (pathlib never
// resolves them, unlike filepath.Clean). Exactly two leading slashes are
// preserved, as POSIX and pathlib both allow.
func pyPath(s string) string {
	root := ""
	if strings.HasPrefix(s, "/") {
		root = "/"
		if strings.HasPrefix(s, "//") && !strings.HasPrefix(s, "///") {
			root = "//"
		}
	}
	var parts []string
	for _, p := range strings.Split(s, "/") {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	if root == "" && len(parts) == 0 {
		return "."
	}
	return root + strings.Join(parts, "/")
}

// pyJoin is Path(dir) / name rendered with as_posix(): a "." directory
// disappears, anything else is joined with a single slash.
func pyJoin(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}

var slugPattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// slug is runner.slug: runs of anything outside [A-Za-z0-9._-] become one
// hyphen, then leading and trailing hyphens are stripped.
func slug(model string) string {
	return strings.Trim(slugPattern.ReplaceAllString(model, "-"), "-")
}

// isoformatUTC renders t as datetime.now(timezone.utc).isoformat() does:
// seconds, then ".ffffff" microseconds only when they are non-zero, then
// "+00:00" rather than "Z".
func isoformatUTC(t time.Time) string {
	t = t.UTC()
	s := t.Format("2006-01-02T15:04:05")
	if us := t.Nanosecond() / 1000; us != 0 {
		s += fmt.Sprintf(".%06d", us)
	}
	return s + "+00:00"
}

// writeFileAtomic writes data to a temporary file in the same directory and
// renames it into place, so a reader (or a crash) never sees a half-written
// archive. rename(2) is atomic on POSIX filesystems; Python's write_text is
// not, and runner.py can leave a truncated file on Ctrl-C.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ilubench-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
