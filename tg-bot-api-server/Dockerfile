FROM alpine:3.19 as builder
COPY build.sh .
RUN ./build.sh

FROM alpine:3.19
COPY --from=builder telegram-bot-api/bin/telegram-bot-api /tg-bot-api-srv
ENTRYPOINT [ "/tg-bot-api-srv", "--local" ]
