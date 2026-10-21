# dog 的本地部署与开发

本目录是个人部署配置。业务代码未修改；根目录 Dockerfile 增加可选的 `NODE_OPTIONS`
构建参数，本地将前端构建的 Node 堆上限设为 4GB，避免首次构建内存不足。
`.dockerignore` 明确排除本目录的 `.env`，避免把本地密钥送入构建上下文。

- `origin`: https://github.com/handsomedogx/sub2api.git
- `upstream`: https://github.com/ranxi2001/sub2api.git
- `production`: 克隆时的生产基线；个人开发使用 `dog/develop`。
- 当前目录里的 `.env` 是本机私密配置，Git 已忽略，禁止提交。

## 启动与更新本地源码构建

先启动 Docker Desktop。在仓库根目录打开 PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\dog\manage.ps1 Start
powershell -ExecutionPolicy Bypass -File .\deploy\dog\manage.ps1 Status
powershell -ExecutionPolicy Bypass -File .\deploy\dog\manage.ps1 Logs
powershell -ExecutionPolicy Bypass -File .\deploy\dog\manage.ps1 Stop
```

访问 http://127.0.0.1:8080 。管理员用户名为 `admin@sub2api.local`。
初始密码在 `deploy/dog/.env` 的 `ADMIN_PASSWORD` 字段，首次登录后在后台修改。
修改 `.env` 的初始密码不会重置已经创建的管理员。

前后端源码修改后执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\dog\manage.ps1 Rebuild
```

只构建镜像、不重启正在运行的实例：

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\dog\manage.ps1 Build
```

首次构建下载 Go/Node 依赖较多，后续利用缓存增量构建。程序、PostgreSQL 和 Redis 使用
`sub2api-dog` 项目的独立命名卷；停止和重建容器不会清除数据。不要执行 `down -v` 或卷清理。
数据库和 Redis 不发布主机端口，应用只监听本机回环地址。

## 同步上游与二次开发

先把本目录的配置和自己的代码提交到 `dog/develop`，再执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\dog\sync-upstream.ps1
```

脚本只从 `upstream/production` 获取并合并代码，不推送、不部署；冲突时保留现场供解决。
验证后执行 `git push -u origin dog/develop` 保存到自己的 fork，随后重建本地实例。
较大功能可以从 `dog/develop` 创建 `feature/功能名` 分支开发。

GitHub 的 HTTPS 代理仅在这个仓库的 `.git/config` 配置为 `http://127.0.0.1:10808`。
代理端口变化时修改：

```powershell
git config http.https://github.com.proxy http://127.0.0.1:10808
```

origin 的推送使用已验证的 SSH 身份 `git@github.com:handsomedogx/sub2api.git`，
fetch 保持 HTTPS 代理配置。Git 使用 Windows OpenSSH，以复用本机现有密钥。

Docker 镜像下载使用 Docker Desktop 自身的代理设置，与 Git 的代理配置分开。
管理脚本的构建命令也默认使用 `http://127.0.0.1:10808`，以便 Docker CLI 获取镜像认证。
端口变化时运行 `manage.ps1 Rebuild -Proxy http://127.0.0.1:新端口`；无需代理可传 `-Proxy ''`。
如需为应用上游请求设置代理，容器内的 `127.0.0.1` 指容器自身，应使用能从容器访问的代理地址。

## 云端构建并发布镜像（推荐正式发布使用）

工作流为 `.github/workflows/dog-image.yml`，名字是 **Dog container image**。
仅 `dog-*` 标签推送或手动执行时构建，不会因为每次普通代码 push 就构建镜像。
`dog-*` 与上游 `v*` 发布标签分开，镜像路径为 `ghcr.io/handsomedogx/sub2api`。

先提交自己的代码，保持工作区干净，然后在仓库根目录运行：

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\dog\publish-cloud.ps1
```

脚本生成 `dog-日期-时间` 标签，原子推送个人开发分支与这个标签，不修改 `production`。
也可指定可读的版本名：

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\dog\publish-cloud.ps1 -Tag dog-v2.14.0-1
```

到 https://github.com/handsomedogx/sub2api/actions 查看构建。
fork 首次使用可能需要在 Actions 页面点击启用工作流；手动运行入口仅当工作流已在默认分支中时显示，
目前优先使用标签触发。已有 CI/Security Scan 可能也响应 push，其结果需与个人镜像工作流分开查看。

云端使用 GitHub 自动提供的 `GITHUB_TOKEN` 推送 GHCR，不需要上传你的生产 `.env` 或创建发布 PAT。
构建使用 Linux amd64（匹配常见 x86 VPS），推送版本标签和 `sha-12位提交号` 标签；运行结果 Summary
还会给出精确的镜像 digest。部署时选固定标签或 digest，不自动覆盖线上服务。
ARM 服务器需要另外扩展构建平台。

GHCR 新镜像可能默认为 private，拉取前需要拥有 `read:packages` 的凭据登录，或由你主动将包设为 public。
工作流不会改变镜像可见性，也不会部署服务器。

## 从本地推送镜像（备用发布方式）

先自行完成 GHCR 登录：

```powershell
docker login ghcr.io -u handsomedogx
```

Password 使用有 `write:packages` 权限的 GitHub classic PAT，而非 GitHub 网页密码。
请在自己的终端输入，不把令牌写进仓库或聊天。
之后运行：

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\dog\manage.ps1 Push
```

脚本会先构建本地源码，再推送为 `ghcr.io/handsomedogx/sub2api:local-日期-时间`。
本地版本必须使用 `local-*` 标签，避免与云端版本标签混淆。此操作会使用本机网络和上行带宽，
不会重启当前实例；通常正式发布用云端流程即可，不必重复上传。

## 以后部署到 Linux 服务器

推荐流程：本地开发和测试 → 推送自己的 fork → CI 构建自己命名空间的容器镜像（以 Git SHA
或版本号标识）→ 服务器拉取指定镜像部署。上线前备份，保留上个镜像版本。
不直接让生产服务器跟随上游 `latest`，这样个人改动与回退版本都可追踪。

服务器单独生成生产 `.env` 和管理员密码，使用 Caddy/Nginx 提供 HTTPS，应用只绑定回环地址，
数据库和 Redis 留在容器内部网络。每天做 PostgreSQL 一致性备份，并备份应用数据和所有加密密钥；
加密复制一份到其他设备/对象存储，实际测试恢复后再开始承载业务。
不能把这套本地命名卷当作生产备份，也不要将本地数据库与生产共用。
