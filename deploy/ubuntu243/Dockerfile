ARG GO_IMAGE=docker.1ms.run/library/golang:1.25.0
ARG NODE_IMAGE=node:20-slim
ARG NGINX_IMAGE=docker.1ms.run/library/nginx:1.28-alpine
FROM ${GO_IMAGE} AS backend-build
WORKDIR /src
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/apiserver ./cmd/apiserver && CGO_ENABLED=0 go build -trimpath -o /out/mcp-gateway ./cmd/mcp-gateway

FROM ${NODE_IMAGE} AS web-build
WORKDIR /src/web
COPY web/ ./
RUN npm install -g pnpm@9.15.0 --registry=https://registry.npmmirror.com && pnpm install --frozen-lockfile --registry=https://registry.npmmirror.com && pnpm run build

FROM ${NGINX_IMAGE} AS backend
WORKDIR /app
COPY --from=backend-build /out/ /usr/local/bin/
COPY configs/ /etc/unla/
COPY assets/ /app/assets/
ENTRYPOINT []
CMD ["mcp-gateway", "--conf", "/etc/unla/mcp-gateway.yaml"]

FROM ${NGINX_IMAGE} AS frontend
COPY --from=web-build /src/web/dist/ /usr/share/nginx/html/
COPY deploy/ubuntu243/nginx.conf /etc/nginx/conf.d/default.conf
