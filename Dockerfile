FROM --platform=$BUILDPLATFORM golang:1.25 as builder-deps
LABEL maintainer="Pico Maintainers <hello@pico.sh>"

WORKDIR /app

RUN apt-get update
RUN apt-get install -y git ca-certificates

COPY go.* ./

RUN go mod download

FROM builder-deps as builder

COPY . .

ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0
ENV LDFLAGS="-s -w"

ENV GOOS=${TARGETOS} GOARCH=${TARGETARCH}

RUN go build -ldflags "$LDFLAGS" -o /go/bin/git-pr ./cmd/git-pr

FROM scratch as release

WORKDIR /app
ENV TERM="xterm-256color"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/bin/git-pr ./git-pr

CMD ["/app/git-pr"]
