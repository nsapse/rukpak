# Test the code
FROM golang:1.17-buster AS builder

WORKDIR /

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY api api
COPY cmd cmd
# COPY pkg pkg
COPY internal internal
COPY hack hack
# COPY main.go main.go
COPY Makefile Makefile

RUN go mod tidy
RUN make test-unit

# build the image
FROM gcr.io/distroless/static:debug
WORKDIR /bin/linux

COPY plain plain
COPY registry registry
COPY unpack unpack
COPY core core
COPY crdvalidator crdvalidator

EXPOSE 8080
