# Zai-Proxy 部署与运行指南

本指南详细说明了如何在本地环境部署 `zai-proxy` 项目，并分析了各个云平台的部署可行性。

## 🛠️ 项目环境修复

在首次运行前，我们发现项目缺失 `go.mod` 文件。已为您执行以下修复：

1. `go mod init zai-proxy`
2. `go mod tidy`

现在项目结构已完整，可以直接运行。

## 💻 本地部署 (Local Deployment)

### 方案 A: 直接运行 (推荐开发使用)

需要安装 [Go 1.2+](https://go.dev/dl/)。

```bash
# 1. 下载依赖 (已完成)
go mod tidy

# 2. 运行服务
go run main.go
```

服务将在端口 `8000` (或配置文件指定端口) 启动。

### 方案 B: Docker 部署 (推荐生产使用)

需要安装 Docker。

```bash
# 1. 构建镜像
docker build -t zai-proxy .

# 2. 运行容器
docker run -p 8000:8000 zai-proxy
```

---

## ☁️ 云端托管可行性分析 (Cloud Feasibility)

基于项目架构 (标准 Go `net/http` 服务 + Dockerfile)，以下是各平台的兼容性分析：

### ✅ 推荐平台 (原生支持/Docker 支持)

这些平台最适合本项目，支持直接部署 Docker 容器或标准 Go 应用，**修改成本为零**。

| 平台        |   兼容性   | 说明                                                                    |
| :---------- | :--------: | :---------------------------------------------------------------------- |
| **Koyeb**   | ⭐⭐⭐⭐⭐ | **完美支持**。可直接连接 GitHub 部署，自动识别 Dockerfile。提供免费层。 |
| **Render**  | ⭐⭐⭐⭐⭐ | **完美支持**。支持 Go 原生环境或 Docker 部署。                          |
| **Fly.io**  | ⭐⭐⭐⭐⭐ | **完美支持**。基于 Docker 的部署，全球边缘节点。                        |
| **Railway** | ⭐⭐⭐⭐⭐ | **完美支持**。自动识别 Dockerfile，部署极快。                           |
| **Zeabur**  | ⭐⭐⭐⭐⭐ | **完美支持**。国内团队开发，对网络支持较好，自动识别 Docker。           |

### ⚠️ 受限平台 (需要代码改造)

这些平台主要面向 Serverless 函数或前端静态托管，部署标准 Go 服务需要大量修改。

| 平台                   |  兼容性   | 说明                                                                                                                                                                                   |
| :--------------------- | :-------: | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Vercel**             |  ⚠️ 中等  | 主要支持 Node.js/Frontend。Go 支持仅限于 "Serverless Function" 模式。**不可直接部署** `main.go`，需要将路由重写为 Vercel 兼容的 Handler 函数 (`http.HandlerFunc`) 并放入 `api/` 目录。 |
| **Cloudflare Workers** |   ⚠️ 低   | **不支持原生 Go**。需要使用 TinyGo 将 Go 编译为 WebAssembly (Wasm) 才能运行。由于本项目使用了标准 `net/http` 和可能的 CGO 依赖，迁移成本极高。                                         |
| **Cloudflare Pages**   | ❌ 不支持 | 仅支持静态网站 (HTML/JS/CSS)。                                                                                                                                                         |
| **Netlify**            |  ⚠️ 中等  | 类似 Vercel，仅支持 Go Lambda 函数，不支持长运行的 Web Server。                                                                                                                        |
| **Deno Deploy**        | ❌ 不支持 | 仅支持 JavaScript/TypeScript (Deno/Node.js)。                                                                                                                                          |

### 结论

- **最佳选择**: 使用 **Koyeb**, **Render**, 或 **Railway**。直接使用项目现有的 `Dockerfile` 即可一键部署。
- **本地开发**: 直接使用 `go run main.go`。
