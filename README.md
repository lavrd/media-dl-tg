# Media Downloader to Telegram

In order to use this repo you need to write plugin... WIP


## Usage

To start bot use:

```shell
TG_BOT_ENDPOINT=http://127.0.0.1:8081/bot%s/%s TG_BOT_TOKEN= VERBOSE=1 go run .
```

`TG_BOT_ENDPOINT` can be empty, so with empty you will use official Telegram bot API server instead of local. \
But in this case you are restricted to upload media to Telegram up to 50MB.

### docker-compose

Also, you can prepare `.env` file and start

```shell
docker-compose up --build
docker-compose up -d --pull always
```

### .env file

```
VERBOSE=1
TG_BOT_TOKEN=
TG_BOT_ENDPOINT=
TELEGRAM_API_ID=
TELEGRAM_API_HASH=
MEDIA_FOLDER=
```


## Migrations

[Lib](https://github.com/golang-migrate/migrate)

```shell
migrate create -ext sql -dir migrations initialize
```
