# Wails build wrapper.
#
# On modern Linux (e.g. Ubuntu 24.04) libwebkit2gtk-4.0 is unavailable, so we
# build against libwebkit2gtk-4.1 via the `webkit2_41` build tag. Every wails
# command that compiles Go must carry this tag, so they're centralised here.
#
# WSLg note: WebKitGTK's DMA-BUF renderer tends to produce a blank window under
# WSLg, so `dev` and `run` disable it. These env vars are harmless on bare-metal
# Linux (they just opt out of a GPU fast-path), so they're left on unconditionally.

WAILS_TAGS := webkit2_41
BIN        := build/bin/ytdlp-ui

# WebKit settings that keep rendering sane under WSLg software GL.
WEBKIT_ENV := WEBKIT_DISABLE_DMABUF_RENDERER=1 WEBKIT_DISABLE_COMPOSITING_MODE=1

.DEFAULT_GOAL := build

.PHONY: build dev run debug clean doctor test seed

## build: production build -> build/bin/ytdlp-ui
build:
	wails build -tags $(WAILS_TAGS)

## test: run Go unit tests (same build tag as the app)
test:
	go test -tags $(WAILS_TAGS) ./...

## dev: live-reload development mode
dev:
	$(WEBKIT_ENV) wails dev -tags $(WAILS_TAGS)

## run: launch the last production build
run:
	$(WEBKIT_ENV) ./$(BIN)

## debug: production build with debugging + devtools enabled
debug:
	wails build -tags $(WAILS_TAGS) -debug -devtools

## clean: remove build artefacts
clean:
	rm -rf build/bin

## doctor: environment diagnostics
doctor:
	wails doctor

## seed: insert sample rows into the library DB for manual UI checks (dev only).
## Gated behind the `seed` build tag so it never ships in the app/bound API.
## Usage: make seed | make seed N=100 | make seed CLEAR=1
N     ?= 24
CLEAR ?=
seed:
	go run -tags seed ./cmd/seed -n $(N) $(if $(CLEAR),-clear,)
