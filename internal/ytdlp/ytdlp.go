// Package ytdlp is the stateless, Wails-agnostic process wrapper around the
// yt-dlp binary. It builds the command line and child-process environment
// (reusing internal/binaries for ffmpeg-location and node PATH injection),
// runs yt-dlp, and maps its output into the structs the frontend consumes.
//
// This chunk implements the metadata half only: a single `yt-dlp -J
// --flat-playlist <url>` call, parsed into a Preview. The download half
// (--progress-template streaming + progress parsing) is a sibling concern that
// will live in this same package (see download.go) and reuse the command/env
// building here — see Options and buildCommand.
package ytdlp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"ytdlp-gui/internal/binaries"
)

// Options carries the resolved external-binary paths and (later) download
// settings that shape a yt-dlp invocation. It is deliberately Wails-agnostic:
// the bound *App method resolves these from config/doctor and passes them in.
//
// YtdlpPath must be a resolved, runnable yt-dlp path (resolution + the
// hard-fail-on-missing decision belong to the caller). FfmpegPath and JSRuntimePath
// are optional: when set, ffmpeg is passed via --ffmpeg-location and a JS runtime
// directory is prepended to the child PATH (mirroring the binaries package
// rules).
type Options struct {
	YtdlpPath     string
	FfmpegPath    string
	JSRuntimePath JSRuntimePath
}

type JSRuntimePath struct {
	Deno string
	// Node    string
	// QuickJS string
	// Bun     string
}

// Runner executes a prepared *exec.Cmd and returns its stdout, stderr, and the
// run error. It is an interface so the metadata parser can be unit-tested
// against captured -J fixtures without a real yt-dlp binary or network. The
// production implementation is ExecRunner.
type Runner interface {
	Run(cmd *exec.Cmd) (stdout, stderr []byte, err error)
}

// ExecRunner is the production Runner: it captures stdout/stderr into buffers
// and runs the command, returning whatever was written even on a non-zero exit
// (so callers can surface yt-dlp's stderr in the error).
type ExecRunner struct{}

// Run executes cmd, capturing stdout and stderr separately.
func (ExecRunner) Run(cmd *exec.Cmd) (stdout, stderr []byte, err error) {
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// buildCommand constructs the yt-dlp *exec.CommandContext for the given args,
// applying the shared env/arg plumbing: --ffmpeg-location when an ffmpeg path
// is configured, and node's directory prepended to the child PATH when a node
// path is configured. It is the single place both metadata and (chunk 4)
// download invocations should go through so they share identical env handling.
//
// args are the yt-dlp arguments that precede the binary-location plumbing; the
// ffmpeg flag is appended so it applies to every invocation that has a
// configured ffmpeg. The URL (or other trailing positional args) should be
// included by the caller in args.
func buildCommand(ctx context.Context, opts Options, args ...string) *exec.Cmd {
	full := append([]string{}, args...)
	full = append(full, binaries.FfmpegLocationArgs(opts.FfmpegPath)...)
	if opts.JSRuntimePath.Deno != "" {
		full = append(full, []string{"--js-runtimes", fmt.Sprintf("deno:%s", opts.JSRuntimePath.Deno)}...)
	}

	cmd := exec.CommandContext(ctx, opts.YtdlpPath, full...)
	cmd.Env = binaries.NodePathEnv(os.Environ(), opts.JSRuntimePath.Deno)
	return cmd
}
