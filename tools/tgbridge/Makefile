# Build tgbridge into THIS directory (tools/tgbridge), not a system path.
# Usage (from tools/tgbridge):
#   make            # build tgbridge.exe here
#   make run        # go run (no exe)
#   make clean      # remove the exe
#
# The exe path is stable, which is what /autostart needs.
#
# Running elevated can leave TEMP pointing at C:\Windows ("creating work dir ...
# Access is denied"), so we pin Go's build temp + cache to this writable folder.

# Directory of this Makefile, so `make` works regardless of where it's invoked.
HERE := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
DIR  := $(patsubst %/,%,$(HERE))
APP  := $(DIR)/tgbridge.exe

# Keep Go's build temp + cache inside this (writable) folder.
export GOTMPDIR := $(DIR)
export GOCACHE  := $(DIR)/.gocache

.PHONY: build run clean

build:
	go build -o "$(APP)" "$(DIR)"

run:
	go run "$(DIR)"

clean:
	rm -f "$(APP)"
