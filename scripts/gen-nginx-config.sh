#!/bin/bash
# 🤖 Web3 Indexer - Nginx Config Generator (Industrial HA)
# This script generates a subdomain-aware, active-backup Nginx configuration.

OUTPUT_FILE="gateway/nginx.conf"
BASE_DOMAIN="st6160.click"

# 🛠️ 环境映射配置: "子域名:主端口:备端口"
SERVICES=(
    "demo1:8081:8091"
    "demo2:8082:8092"
    "debug:8083:8093"
)

cat <<EOF > "$OUTPUT_FILE"
worker_processes auto;
events {
    worker_connections 1024;
}

http {
    include       /etc/nginx/mime.types;
    default_type  application/octet-stream;
    sendfile        on;
    keepalive_timeout  65;

    # --- Upstream Clusters (Active-Backup) ---
EOF

# 生成 Upstream 块
for service in "${SERVICES[@]}"; do
    IFS=':' read -r sub primary backup <<< "$service"
    cat <<EOF >> "$OUTPUT_FILE"
    upstream ${sub}_cluster {
        server host.docker.internal:$primary max_fails=1 fail_timeout=2s;
        server host.docker.internal:$backup backup;
    }
EOF
done

cat <<EOF >> "$OUTPUT_FILE"

    # --- Server Routing Layer ---
    server {
        listen 80;
        
        # Buffer optimizations for large Ed25519 headers
        proxy_buffer_size 128k;
        proxy_buffers 4 256k;
        proxy_busy_buffers_size 256k;

        location /gw-health {
            return 200 'OK';
        }
EOF

# 生成 Server 路由逻辑 (基于 Server Name)
for service in "${SERVICES[@]}"; do
    IFS=':' read -r sub primary backup <<< "$service"
    cat <<EOF >> "$OUTPUT_FILE"

        # Routing for $sub.$BASE_DOMAIN
        if (\$http_host ~* "$sub\.$BASE_DOMAIN") {
            set \$target_upstream "http://${sub}_cluster";
        }
EOF
done

cat <<EOF >> "$OUTPUT_FILE"

        if (\$target_upstream = "") {
            return 404 "No environment matched for this hostname";
        }

        location / {
            proxy_pass \$target_upstream;
            proxy_next_upstream error timeout invalid_header http_502 http_503 http_504;
            proxy_next_upstream_tries 2;
            proxy_next_upstream_timeout 5s;

            proxy_set_header Host \$host;
            proxy_set_header X-Real-IP \$remote_addr;
            proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto \$scheme;

            # WebSocket support
            proxy_http_version 1.1;
            proxy_set_header Upgrade \$http_upgrade;
            proxy_set_header Connection "Upgrade";
        }
    }
}
EOF

echo "✅ Nginx configuration generated at $OUTPUT_FILE"
