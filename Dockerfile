FROM golang:1.27.4 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/golem .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/golem /golem
# Worlds, server.properties, ops.json and logs are written to the working
# directory, so it is the volume.
WORKDIR /data
VOLUME /data
EXPOSE 25565/tcp
ENTRYPOINT ["/golem"]
