# 开发环境

通过 Docker 容器提供一致的开发环境，避免本地安装 Go 工具链。

## 启动容器

```bash
cd docker-debug
docker-compose up -d --build
```

## 进入容器

```bash
docker exec -it ccnexus-dev /bin/sh
```

## 首次编译（必须先执行）

因为容器内源码是映射进来的，需要先在容器内生成 `go.sum`：

```bash
cd /app
go mod tidy
go build -o ccnexus-server ./cmd/server
```

## 运行服务

```bash
./ccnexus-server
```

服务启动后访问 http://localhost:3021/health 检查健康状态。

## 修改代码后

代码位于容器的 `/app` 目录，与宿主机源码目录实时同步。修改后直接在容器内重新编译即可：

```bash
cd /app
go build -o ccnexus-server ./cmd/server
./ccnexus-server
```

无需重新 `docker build`。

## 停止容器

```bash
docker-compose down
```

## 清理数据

```bash
rm -rf docker-debug/ccnexus-data
```