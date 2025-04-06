FROM --platform=$BUILDPLATFORM golang:1.22 as builder-deps
LABEL maintainer="Pico Maintainers <hello@pico.sh>"

WORKDIR /app

RUN apt-get update
RUN apt-get install -y git ca-certificates

COPY go.* ./

RUN go mod download

FROM builder-deps as builder-web

COPY . .

ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0
ENV LDFLAGS="-s -w"

ENV GOOS=${TARGETOS} GOARCH=${TARGETARCH}

RUN go build -ldflags "$LDFLAGS" -o /go/bin/git-web ./cmd/git-web

FROM builder-deps as builder-ssh

COPY . .

ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0
ENV LDFLAGS="-s -w"

ENV GOOS=${TARGETOS} GOARCH=${TARGETARCH}

RUN go build -ldflags "$LDFLAGS" -o /go/bin/git-ssh ./cmd/git-ssh

FROM scratch as release-web

WORKDIR /app

COPY --from=builder-web /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder-web /go/bin/git-web ./git-web

CMD ["/app/web"]

FROM scratch as release-ssh

WORKDIR /app
ENV TERM="xterm-256color"

COPY --from=builder-ssh /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder-ssh /go/bin/git-ssh ./git-ssh

CMD ["/app/git-ssh"]
