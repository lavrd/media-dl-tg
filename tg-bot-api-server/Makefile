build:
	docker build -t ghcr.io/lavrd/tg-bot-api-server -f Dockerfile .

run:
	docker run -d --rm -p 8081:8081 \
		-e TELEGRAM_API_ID=$(api_id) \
		-e TELEGRAM_API_HASH=$(api_hash) \
		tg-bot-api-server
