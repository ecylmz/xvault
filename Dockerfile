FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/xvault ./cmd/xvault

FROM alpine:3.22
RUN adduser -D -h /home/xvault xvault && apk add --no-cache ca-certificates
USER xvault
WORKDIR /home/xvault
COPY --from=build /out/xvault /usr/local/bin/xvault
ENTRYPOINT ["xvault"]
