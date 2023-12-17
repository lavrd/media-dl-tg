lint:
	@go fmt ./...
	@golangci-lint run ./...

test:
	@go test -count=1 -v -coverprofile=cover.out ./internal/...

coverage:
	@go tool cover -html=cover.out

run:
	@TG_BOT_TOKEN=$(token) VERBOSE=1 go run ./cmd/media-dl-tg

build_plugin:
	@mkdir -p plugin
	@cp ../$(plugin)/main.go ./plugin/main.go
	@go mod tidy # to install plugin dependencies
	@go build -buildmode=plugin ./plugin
	@rm -rf plugin/*
	@go mod tidy # to remove plugin dependencies

build_docker:
	@docker build -t media-dl-tg -f Dockerfile .
