WASM = sim/engine.wasm
TINYGO ?= tinygo

.PHONY: wasm test verify serve clean

# wasm-unknown: the module imports nothing. The target's default collector
# never frees memory and the engine allocates every tick, so a real one is set.
wasm:
	$(TINYGO) build -o $(WASM) -target wasm-unknown -gc=precise -no-debug ./cmd/wasm

# The second vet type-checks the module's exports, which only build under TinyGo.
# Packages are named: ./... would also walk node_modules, which ships Go files.
test:
	go vet ./engine/... ./bridge/... ./cmd/...
	go vet -tags tinygo.wasm ./cmd/wasm
	go test ./engine/... ./bridge/...

# Every scenario runs on the original page's engine and on this one, natively
# and as the compiled module; any difference in the printed digests fails.
verify: wasm
	./reference/compare.sh
	WASM=$(WASM) ./reference/compare.sh

serve: wasm
	python3 -m http.server 8765

clean:
	rm -f $(WASM)
