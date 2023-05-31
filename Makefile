test:
	@go test -count=1 -v -coverprofile=cover.out ./internal/...

lint: 
	@golangci-lint run ./...

coverage:
	@go tool cover -html=cover.out

docker_build:
	@docker build -t media-dl-tg -f Dockerfile .

docker_run:
	@docker run -d --rm \
		-e TG_BOT_ENDPOINT=http://tg-bot-api-server:8081/bot%s/%s -e TG_BOT_TOKEN= -e VERBOSE=1 \
		media-dl-tg 
