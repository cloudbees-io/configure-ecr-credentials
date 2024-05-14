FROM alpine:3.18 as certs

RUN apk add -U --no-cache ca-certificates

FROM golang:1.22.1-alpine3.19 AS helper

ENV CGO_ENABLED=0
ENV GOBIN=/usr/local/bin

RUN go install github.com/awslabs/amazon-ecr-credential-helper/ecr-login/cli/docker-credential-ecr-login@276ba673855c511da522c7b6e6b89bff48aebabc

FROM golang:1.22.1-alpine3.19 AS build

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
