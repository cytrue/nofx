部署说明（方案 A：CI 构建二进制 -> SCP 到服务器 -> systemd 管理）
=================================================

本文档说明如何把已编译或通过 CI 构建好的 `nofx` 可执行文件和前端静态文件部署到远程服务器，并使用 `systemd` 管理后台进程。

准备工作（服务器）
------------------
1. 创建部署用户并设置 SSH 公钥：

```bash
# 在服务器上（root 或有 sudo 权限）
sudo adduser --disabled-password --gecos "" deploy
sudo mkdir -p /home/deploy/.ssh
# 把你的公钥追加到 authorized_keys（从本地上传）
# 本地执行： cat ~/.ssh/id_rsa.pub | ssh root@your.server "cat >> /home/deploy/.ssh/authorized_keys"
sudo chown -R deploy:deploy /home/deploy/.ssh
sudo chmod 700 /home/deploy/.ssh
sudo chmod 600 /home/deploy/.ssh/authorized_keys

# 创建部署目录
sudo mkdir -p /home/deploy/nofx
sudo chown -R deploy:deploy /home/deploy/nofx
```

2. 在服务器上准备 `systemd` 单元（示例放在 /etc/systemd/system/nofx.service）：

```ini
[Unit]
Description=nofx AI Trading Service
After=network.target

[Service]
User=deploy
Group=deploy
WorkingDirectory=/home/deploy/nofx
ExecStart=/home/deploy/nofx/nofx /home/deploy/nofx/config.json
Restart=always
RestartSec=5
Environment=TZ=Asia/Shanghai

[Install]
WantedBy=multi-user.target
```

启用并启动服务（服务器上执行）：

```bash
sudo systemctl daemon-reload
sudo systemctl enable nofx.service
sudo systemctl start nofx.service
sudo journalctl -u nofx.service -f
```

CI / GitHub Actions 集成（我已在仓库添加 workflow）
-------------------------------------------------
仓库包含 `.github/workflows/deploy-binary-scp.yml`，其行为：

- 使用仓库中的 `Dockerfile` 的 `backend-builder` 阶段构建后端二进制（以保证 TA‑Lib/CGO 可用）并把 `nofx` 提取出来。
- 在 `web` 目录运行 `npm ci` + `npm run build` 生成前端 `web/dist`。
- 把 `nofx` 与 `web/dist` 打包为 `nofx_dist.tar.gz`，通过 SCP 上传到服务器 `TARGET_DIR`。
- 通过 SSH 在服务器上解包、设置权限、把前端文件复制到 `/home/deploy/nofx/web/dist`（可根据实际 nginx 配置调整），然后重启 `nofx.service`。

需要在 GitHub 仓库设置 Secrets：

- SERVER_HOST：服务器 IP 或域名
- SERVER_USER：部署用户名（例如 deploy）
- SSH_PRIVATE_KEY：用于 CI 的私钥（对应公钥已加入服务器 `/home/deploy/.ssh/authorized_keys`）
- TARGET_DIR：上传目录，例如 /home/deploy/nofx
- SERVER_PORT：可选，默认 22

手动部署（不使用 CI）
--------------------
若你已经在本地构建好 Linux 可执行文件 `nofx`（ELF），可以直接用 rsync/scp 上传并在服务器上操作：

```bash
# 在本地项目根目录
rsync -avP ./nofx ./config.json ./web/dist/ deploy@your.server:/home/deploy/nofx/

# 登录服务器
ssh deploy@your.server
cd /home/deploy/nofx
chmod +x ./nofx
sudo systemctl restart nofx.service
sudo journalctl -u nofx.service -f
```

注意事项与常见问题
-----------------
- 本地 macOS 构建的 `nofx` 通常是 Mach-O，不可直接在 Linux 服务器上运行；请确认 `file ./nofx` 输出为 `ELF`，否则需交叉编译或在 CI 中用 Docker 构建。
- 如果需要在服务器上运行 TA‑Lib（C 库），建议使用 workflow 中的 Docker 构建流程或在服务器上用 docker compose 构建（避免在 runner 上手动安装复杂依赖）。
- 不要把 API keys 提交到仓库。生产环境请把 `config.json` 放在服务器上并设置权限为 600，或使用环境变量/Secret 管理。

如需我把 workflow 调整为“在 Actions 构建镜像并推送到 GHCR，然后服务器 pull 并 docker compose up”的方案，我也可以帮你生成该 workflow（推荐用于生产）。
