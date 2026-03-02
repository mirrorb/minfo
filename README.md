## 项目介绍

`minfo` 是一个本地媒体信息检测 Web 工具，主要功能：
- 输出 MediaInfo 信息
- 输出 BDInfo 信息
- 导出随机 8 张截图压缩包

## Docker 运行

示例 `docker-compose.yml`：

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
      REQUEST_TIMEOUT: "20m"
    volumes:
      - /your/media/path1:/media_path1
      - /your/media/path2:/media_path2
      - /your/media/path3:/media_path3
      - /your/media/path4:/media_path4
    restart: unless-stopped
```

启动：

```bash
docker compose up -d
```

访问：
- `http://localhost:28081`

