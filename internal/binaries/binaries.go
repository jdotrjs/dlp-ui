// Package binaries resolves the external executables ytdlp-gui shells out to
// (yt-dlp, ffmpeg, node), reports their health via a doctor check, and builds
// the command-line / environment plumbing each binary needs:
//
//   - ffmpeg: yt-dlp has a first-class --ffmpeg-location flag, so a configured
//     ffmpeg path is passed on the command line.
//   - node: has no such flag, so a configured node path is injected by
//     prepending its directory to the child process's PATH.
package binaries

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Source describes how a binary path was resolved.
type Source string

const (
	// SourceConfig means an explicit path from config was used.
	SourceConfig Source = "config"
	// SourcePATH means the binary was found via PATH lookup.
	SourcePATH Source = "path"
	// SourceUnresolved means the binary could not be located.
	SourceUnresolved Source = "unresolved"
)

// ErrNotFound is the typed error returned when a binary cannot be resolved
// from either an explicit path or PATH.
var ErrNotFound = errors.New("binary not found")

// Resolved is the outcome of locating a binary on disk.
type Resolved struct {
	// Path is the absolute (or PATH-resolved) executable path. Empty when
	// unresolved.
	Path string
	// Source records how Path was determined.
	Source Source
}

// Resolve locates a binary. If explicit is set it is stat'd and checked for an
// executable bit (SourceConfig); otherwise exec.LookPath is consulted
// (SourcePATH). A failure at either stage returns an error wrapping
// ErrNotFound.
func Resolve(name, explicit string) (Resolved, error) {
	if explicit != "" {
		info, err := os.Stat(explicit)
		if err != nil {
			return Resolved{Source: SourceUnresolved},
				fmt.Errorf("%w: %s at %q: %v", ErrNotFound, name, explicit, err)
		}
		if info.IsDir() {
			return Resolved{Source: SourceUnresolved},
				fmt.Errorf("%w: %s at %q is a directory", ErrNotFound, name, explicit)
		}
		if !isExecutable(info) {
			return Resolved{Source: SourceUnresolved},
				fmt.Errorf("%w: %s at %q is not executable", ErrNotFound, name, explicit)
		}
		return Resolved{Path: explicit, Source: SourceConfig}, nil
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return Resolved{Source: SourceUnresolved},
			fmt.Errorf("%w: %s not on PATH: %v", ErrNotFound, name, err)
	}
	return Resolved{Path: path, Source: SourcePATH}, nil
}

// isExecutable reports whether the file looks runnable. On Windows the
// executable bit is not meaningful, so any existing regular file qualifies.
func isExecutable(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

// BinaryStatus is the per-binary doctor result surfaced to the frontend. JSON
// tags are camelCase for the Wails bindings.
type BinaryStatus struct {
	Name     string `json:"name"`
	Resolved string `json:"resolved"`
	Source   Source `json:"source"`
	OK       bool   `json:"ok"`
	Version  string `json:"version"`
	Error    string `json:"error"`
}

// VersionRunner runs `<path> --version` (or equivalent) and returns the raw
// output. It is an interface so tests can avoid depending on real binaries.
type VersionRunner interface {
	Version(path string, spec Spec) (string, error)
}

// ExecVersionRunner is the production VersionRunner: it actually invokes the
// binary with --version and returns trimmed stdout.
type ExecVersionRunner struct{}

// Version runs `<path> --version` and returns the first line of trimmed output.
func (ExecVersionRunner) Version(path string, spec Spec) (string, error) {
	versionArgs := "--version"
	if spec.VersionFlag != "" {
		versionArgs = spec.VersionFlag
	}
	out, err := exec.Command(path, versionArgs).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run %q %s: %w", path, versionArgs, err)
	}
	return firstLine(out), nil
}

// FileSHA256 returns the lowercase-hex sha256 of the file at path. It is used
// by the caching version runner to key cached --version output by binary
// identity, so a yt-dlp upgrade automatically invalidates the cached version.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// Spec names a binary and the explicit (config) path to try first.
type Spec struct {
	Name        string
	Explicit    string
	VersionFlag string // overrides default `--version` when set
}

// Doctor resolves each spec and, when resolution succeeds, runs the injected
// VersionRunner to confirm the binary is runnable. The returned statuses are
// in the same order as specs and never error out wholesale — each binary's
// problem is captured in its own BinaryStatus.
func Doctor(runner VersionRunner, specs ...Spec) []BinaryStatus {
	statuses := make([]BinaryStatus, len(specs))
	wg := sync.WaitGroup{}
	for idx, s := range specs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st := BinaryStatus{Name: s.Name}

			res, err := Resolve(s.Name, s.Explicit)
			st.Source = res.Source
			st.Resolved = res.Path
			if err != nil {
				st.OK = false
				st.Error = err.Error()
				statuses[i] = st
				return
			}

			ver, verr := runner.Version(res.Path, s)
			if verr != nil {
				st.OK = false
				st.Error = verr.Error()
				statuses[i] = st
				return
			}
			st.OK = true
			st.Version = ver
			statuses[i] = st
		}(idx)
	}
	wg.Wait()
	return statuses
}

// FfmpegLocationArgs returns the yt-dlp argument fragment for a configured
// ffmpeg path. yt-dlp's --ffmpeg-location accepts either the binary or its
// containing directory; we pass the configured value through directly. Empty
// ffmpegPath yields no args (yt-dlp falls back to PATH).
func FfmpegLocationArgs(ffmpegPath string) []string {
	if ffmpegPath == "" {
		return nil
	}
	return []string{"--ffmpeg-location", ffmpegPath}
}

// NodePathEnv returns an environment slice (suitable for exec.Cmd.Env) derived
// from base, with filepath.Dir(nodePath) prepended to PATH so a child process
// resolves the configured node first. When nodePath is empty, base is returned
// unchanged. If base has no PATH entry, one is added.
func NodePathEnv(base []string, nodePath string) []string {
	if nodePath == "" {
		return base
	}
	nodeDir := filepath.Dir(nodePath)

	out := make([]string, 0, len(base)+1)
	found := false
	for _, kv := range base {
		if name, val, ok := splitEnv(kv); ok && strings.EqualFold(name, "PATH") {
			out = append(out, name+"="+prependPath(nodeDir, val))
			found = true
			continue
		}
		out = append(out, kv)
	}
	if !found {
		out = append(out, "PATH="+nodeDir)
	}
	return out
}

// prependPath puts dir at the front of an OS PATH list (no-op if already
// leading).
func prependPath(dir, existing string) string {
	if existing == "" {
		return dir
	}
	sep := string(os.PathListSeparator)
	if strings.HasPrefix(existing, dir+sep) || existing == dir {
		return existing
	}
	return dir + sep + existing
}

// splitEnv splits a "KEY=VALUE" environment entry.
func splitEnv(kv string) (name, value string, ok bool) {
	i := strings.IndexByte(kv, '=')
	if i < 0 {
		return "", "", false
	}
	return kv[:i], kv[i+1:], true
}
