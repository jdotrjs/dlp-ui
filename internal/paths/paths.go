// Package paths centralises the on-disk locations the app reads and writes:
// the user config dir (`config.json`), the vendor dir for auto-installed
// binaries, and per-binary paths under it. Other packages should call into
// here instead of recomputing UserConfigDir/joining strings ad-hoc, so a
// future layout change is a one-file edit.
package paths

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

// AppDirName is the per-app folder name placed inside os.UserConfigDir().
func AppDirName() string {
	if runtime.GOOS == "windows" {
		return "ytdlp-ui"
	}
	return ".ytdlp-ui"
}

// ConfigDir returns <os.UserConfigDir>/.ytdlp-ui. Validates it is a directory
// and creates it if it doesn't exist.
func ConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("Failed to locate user config dir: %w", err)
	}
	path := filepath.Join(dir, AppDirName())
	slog.Info("Using config dir: "+path, "err", err)
	finfo, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(path, 0x775); err == nil {
			finfo, err = os.Stat(path)
			if err != nil {
				return "", fmt.Errorf("Failed to get directory: %w", err)
			}
		}
	} else if err != nil {
		return "", fmt.Errorf("Checking config directory failed: %w", err)
	}
	if !finfo.IsDir() {
		return "", errors.New("%s is not a directory")
	}
	return path, err
}

// ConfigPath returns the absolute path to config.json.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// VendorDir returns the directory used for auto-installed third-party
// binaries (yt-dlp, ffmpeg, deno, ...).
func VendorDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vendor"), nil
}

// VendorBinPath returns the install path for a given binary name (with .exe
// appended on Windows).
func VendorBinPath(name string) (string, error) {
	dir, err := VendorDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name), nil
}
