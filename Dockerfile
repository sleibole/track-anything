FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /trackanything .
RUN mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /trackanything /trackanything
# The nonroot user (65532) must own /data so it can create the SQLite file.
COPY --from=build --chown=65532:65532 /data /data
ENV ENV=prod DB_PATH=/data/trackanything.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/trackanything"]
