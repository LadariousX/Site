FROM golang:1.26-alpine
LABEL authors="layden"

# Preload module cache so container restarts skip the network fetch.
# Build context is Site/ — see readme.
COPY Blog/go.mod Blog/go.sum /preload/Blog/
RUN cd /preload/Blog && go mod download

COPY Tools/go.mod Tools/go.sum /preload/Tools/
RUN cd /preload/Tools && go mod download

CMD ["sh", "-c", "go mod tidy && go run main.go"]
