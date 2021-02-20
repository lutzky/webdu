FROM golang:1.16 AS build-env
ADD . /src
RUN cd /src && \
	CGO_ENABLED=0 GOOS=linux go build -a \
	-ldflags '-w -extldflags "-static"' -o webdu

FROM scratch
COPY --from=build-env /src/webdu /
EXPOSE 8099
ENTRYPOINT ["/webdu"]
