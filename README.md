## 项目介绍

`minfo` 是一个用于本地媒体信息检测的小型 Web 工具。

主要功能：

- 输出 MediaInfo 信息
- 输出 BDInfo 信息
- 导出 8 张截图压缩包
- 基于本地媒体目录的路径补全

## 使用方式

使用如下 `docker-compose.yml`：

```yaml
services:
  minfo:
    image: ghcr.io/mirrorb/minfo:latest
    container_name: minfo
    privileged: true
    ports:
      - "28081:8080"
    environment:
      WEB_PASSWORD: "adminadmin"
    volumes:
      - /your/media/path:/media:ro
    restart: unless-stopped
```

启动：

```bash
docker compose up -d
```

访问：

- `http://localhost:28081`
