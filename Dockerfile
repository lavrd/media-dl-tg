FROM golang:1.21-alpine as build
#RUN apk add build-base # todo: fix
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY main.go .
RUN go build -o media-dl-tg main.go

FROM alpine:3.19 as run
WORKDIR /run
COPY --from=build /build/media-dl-tg /run/media-dl-tg
COPY migrations /run/migrations
ENTRYPOINT [ "/run/media-dl-tg" ]
