package main

import (
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/pkg/browser"
)

// revealInFolder opens the OS file manager showing the given file, selecting it
// where the platform supports a "reveal" gesture (Windows Explorer /select,
// macOS Finder open -R). On other platforms (Linux/*nix) there's no portable
// select-the-file primitive, so it opens the containing directory instead.
//
// It is best-effort: the path is assumed to exist (callers check the row's
// filepath is non-empty first); any launcher error is returned so the frontend
// can surface it.
func revealInFolder(path string) error {
	switch runtime.GOOS {
	case "windows":
		// explorer returns a non-zero exit code even on success, so the error
		// from Run is intentionally ignored here.
		_ = exec.Command("explorer", "/select,"+path).Run()
		return nil
	case "darwin":
		return exec.Command("open", "-R", path).Run()
	default:
		// Linux and friends: open the containing directory with the default
		// file manager via the same mechanism used to open files.
		return browser.OpenFile(filepath.Dir(path))
	}
}
