# tg-bot-api-server

https://tdlib.github.io/telegram-bot-api/build.html?os=Linux

## Build

```shell
make build
```

## Run

```shell
make run api_id= api_hash=
```

or

```shell
docker run --rm -it -p 8081:8081 tg-bot-api-server /bin/sh
TELEGRAM_API_ID= TELEGRAM_API_HASH= telegram-bot-api/bin/telegram-bot-api --local
```
