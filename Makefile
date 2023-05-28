docker_build:
	@docker build -t media-dl-tg -f Dockerfile .

docker_run:
	@docker run -d --rm \
		-e TG_BOT_ENDPOINT=http://tg-bot-api-server:8081/bot%s/%s -e TG_BOT_TOKEN= -e VERBOSE=1 \
		media-dl-tg 
