FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG GO_BUILD_TAGS=
RUN CGO_ENABLED=0 go build -tags="$GO_BUILD_TAGS" -trimpath -ldflags='-s -w' -o /pulse-control .
FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -g 10001 pulse && adduser -D -u 10001 -G pulse pulse && mkdir /data && chown pulse:pulse /data
COPY --from=build /pulse-control /usr/local/bin/pulse-control
USER pulse
ENV CONTROL_LISTEN=0.0.0.0:7080 CONTROL_DATA_DIR=/data
EXPOSE 7080
ENTRYPOINT ["pulse-control"]
