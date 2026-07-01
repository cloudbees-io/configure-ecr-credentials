FROM alpine:3.24 AS certs

RUN apk add -U --no-cache ca-certificates

FROM golang:1.26.4-alpine3.24 AS helper

ENV CGO_ENABLED=0
ENV GOBIN=/usr/local/bin

RUN go install github.com/awslabs/amazon-ecr-credential-helper/ecr-login/cli/docker-credential-ecr-login@79ad5557681c631ff7f9391a561609a8452f81c1

FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=helper /usr/local/bin/docker-credential-* /usr/bin/
COPY configure-ecr-credentials /usr/bin/

WORKDIR /cloudbees/home

ENV HOME=/cloudbees/home
ENV PATH=/usr/bin

ENTRYPOINT ["configure-ecr-credentials"]
