FROM alpine:3.22 AS certs

RUN apk add -U --no-cache ca-certificates

FROM golang:1.25.0-alpine3.22 AS helper

ENV CGO_ENABLED=0
ENV GOBIN=/usr/local/bin

RUN go install github.com/awslabs/amazon-ecr-credential-helper/ecr-login/cli/docker-credential-ecr-login@79ad5557681c631ff7f9391a561609a8452f81c1

FROM golang:1.25.0-alpine3.22 AS build

WORKDIR /work

COPY go.mod* go.sum* ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o /build-out/ .

FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=helper /usr/local/bin/docker-credential-* /usr/bin/
COPY --from=build /build-out/* /usr/bin/

WORKDIR /cloudbees/home

ENV HOME=/cloudbees/home
ENV PATH=/usr/bin

ENTRYPOINT ["configure-ecr-credentials"]
