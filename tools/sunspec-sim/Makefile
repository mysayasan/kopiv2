# sunspec-sim — SunSpec-over-Modbus-TCP solar simulator.
# Part of the developer tooling; no runtime dependency on apps/domain/infra. Uses the root module.
#
# Build the exe into THIS directory so the path is stable for scripts and re-runs, mirroring
# tools/tgbridge. Run from the repo root.

GOFLAGS ?=
EXE := sunspec-sim.exe

.PHONY: build run test clean

build:
	cd ../.. && go build $(GOFLAGS) -o tools/sunspec-sim/$(EXE) ./tools/sunspec-sim

run:
	cd ../.. && go run ./tools/sunspec-sim $(ARGS)

test:
	cd ../.. && go test ./tools/sunspec-sim/...

clean:
	rm -f $(EXE)
