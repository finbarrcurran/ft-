.PHONY: build run dev test fmt vet tidy clean sync-assets

BIN := bin/ft
WEB_SRC := web
WEB_DST := internal/web/assets

build: sync-assets
	@mkdir -p bin
	go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/ft

# Source-of-truth for the frontend is web/. Copy into the embed dir before
# every build so editing web/app.js actually takes effect.
sync-assets:
	@mkdir -p $(WEB_DST)
	@cp -r $(WEB_SRC)/. $(WEB_DST)/

# Local dev: insecure cookies (no HTTPS) so cookies survive http://localhost
run: build
	FT_COOKIE_SECURE=false FT_ADDR=:8081 ./$(BIN)

dev: run

test:
	go test ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin data
