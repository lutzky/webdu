FROM golang:1.12 AS build-env
ADD . /src
RUN cd /src && \
	CGO_ENABLED=0 GOOS=linux go build -a \
	-ldflags '-w -extldflags "-static"' -o webdu

FROM scratch
COPY --from=build-env /src/header.html /
COPY --from=build-env /src/table.html /
COPY --from=build-env /src/webdu /
ENTRYPOINT ["/webdu"]
