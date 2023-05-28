# tg-bot-api-server


## Build

```shell
docker build -t tg-bot-api-server -f Dockerfile .
```


## Run

```shell
docker run -d --rm -p 8081:8081 -e TELEGRAM_API_ID= -e TELEGRAM_API_HASH= tg-bot-api-server
```

or

```shell
docker run --rm -it -p 8081:8081 tg-bot-api-server /bin/bash
TELEGRAM_API_ID= TELEGRAM_API_HASH= telegram-bot-api/bin/telegram-bot-api --local
```
