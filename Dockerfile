FROM golang:1.13 as build

WORKDIR /go/src/PodLifecycleLogger
COPY . .

RUN go install -v PodLifecycleLogger

FROM debian:stretch-slim

COPY --from=build /go/bin/PodLifecycleLogger /usr/bin/PodLifecycleLogger

ENTRYPOINT ["PodLifecycleLogger"]
