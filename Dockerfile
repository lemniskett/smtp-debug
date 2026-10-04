FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /smtp-debug .

FROM alpine:3
RUN apk add --no-cache ca-certificates \
	&& mkdir -p /failed
COPY --from=build /smtp-debug /usr/local/bin/smtp-debug
ENV SMTP_DEBUG_FAILED_DIR=/failed \
	SMTP_DEBUG_LISTEN=0.0.0.0:2525
VOLUME /failed
EXPOSE 2525
ENTRYPOINT ["smtp-debug"]
