# 后端发版 / 部署（sparklyi/SOJ）

服务器 `<deploy-host>` 只有 2G 内存，**禁止在服务器上 docker build**（judge-agent 镜像含
docker-cli/go/g++，构建极重）。构建全部发生在 GitHub Actions。

## 发版流程

```bash
git tag v1.3.2 && git push origin v1.3.2
```

1. `deploy-backend.yml` 被触发：对 api / worker / judge-agent / migrate 四个服务分别
   `docker buildx build`（`COMMAND=soj-<svc>`），`docker save | gzip` 成
   `soj-<svc>-<tag>.tar.gz`，传到 GitHub Release。
2. 服务器上的 `soj-api-deploy.timer`（每 5 分钟，错开前端轮询的整 5 分钟点）跑
   `api-deploy-check.sh`：`git ls-remote` 比对最新 `v<数字>*` 与 `/opt/soj/state/api-current`，
   有新版就执行 `api-deploy.sh`。
3. `api-deploy.sh`：下载 4 个镜像包 → `docker load` → 重打 `soj-<svc>:latest` →
   `docker compose --env-file /opt/soj/.env -f docker-compose.yaml -f docker-compose.prod.yaml up -d`
   → `127.0.0.1:8080/readyz` 健康检查（180 秒）→ 失败自动重打回退镜像 ID 并重启。
   每个服务本地只保留最近 3 个版本镜像。

## 手动触发 / 排查

```bash
ssh root@<deploy-host>
/opt/soj/bin/api-deploy.sh v1.3.2          # 手动部署指定 tag
journalctl -u soj-api-deploy -n 80         # 轮询日志
journalctl -u soj-api                      # 如有（见部署方式）
docker compose --env-file /opt/soj/.env -f docker-compose.yaml -f docker-compose.prod.yaml ps
cat /opt/soj/state/api-current             # 当前线上版本
```

## 与前端的边界

- 前端仓库 `sparklyi/SOJ-web`，tag 前缀 `web-v*`，轮询脚本只认 `web-v*`；
- 后端本仓库，tag 是 `v<数字>*`，轮询脚本 grep `^v[0-9]`，两者互不误触。
- compose 项目名 `soj` 来自 `/opt/soj/.env` 的 `COMPOSE_PROJECT_NAME`，必须带
  `--env-file`，否则项目名会变成 `deploy`、整套容器被重建一遍。
