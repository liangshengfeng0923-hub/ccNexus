FROM golang:1.24-alpine

RUN apk add --no-cache gcc musl-dev sqlite-dev ca-certificates tzdata

WORKDIR /app

COPY go.mod ./
RUN go env -w GO111MODULE=on && \
    go env -w GOPROXY=https://goproxy.cn,direct && \
    go mod download

# 保持容器运行，便于 docker exec 进入调试
CMD ["tail", "-f", "/dev/null"]