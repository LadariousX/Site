FROM golang:1.26-alpine
LABEL authors="layden"
WORKDIR /workspace/Blog
COPY Blog/go.mod /workspace/Blog
COPY Blog/go.sum /workspace/Blog

RUN go mod download
CMD ["go", "run", "main.go"]