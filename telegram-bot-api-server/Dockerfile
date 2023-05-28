FROM ubuntu:22.10 as builder
COPY build.sh .
RUN ./build.sh

FROM ubuntu:22.10
RUN apt-get update
RUN apt-get upgrade -y
RUN apt-get install -y libssl-dev
COPY --from=builder telegram-bot-api/bin/telegram-bot-api /tg-bot-api-srv
ENTRYPOINT [ "/tg-bot-api-srv", "--local" ]
