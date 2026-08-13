#!/bin/sh
set -e

# Railway sets the PORT environment variable
export PORT=${PORT:-8080}
echo "🚀 Starting NOFX on port $PORT..."

# One-time login reset when Railway env vars are set (CLI SSH is often blocked).
if [ -n "$RESET_LOGIN_EMAIL" ] && [ -n "$RESET_LOGIN_PASSWORD" ]; then
    echo "🔑 Attempting password reset for configured login email..."
    if ! /app/nofx reset-password --email "$RESET_LOGIN_EMAIL" --password "$RESET_LOGIN_PASSWORD" --db /app/data/data.db; then
        echo "📋 Existing login emails:"
        sqlite3 /app/data/data.db "SELECT email FROM users;" || true
        COUNT=$(sqlite3 /app/data/data.db "SELECT COUNT(*) FROM users;" 2>/dev/null || echo 0)
        if [ "$COUNT" = "1" ]; then
            echo "🔑 Single-user instance: pointing the existing account at the configured email, then resetting password..."
            sqlite3 /app/data/data.db "UPDATE users SET email='$RESET_LOGIN_EMAIL' WHERE id=(SELECT id FROM users LIMIT 1);"
            /app/nofx reset-password --email "$RESET_LOGIN_EMAIL" --password "$RESET_LOGIN_PASSWORD" --db /app/data/data.db \
                || echo "⚠️ Password reset still failed after email update"
        else
            echo "⚠️ Password reset skipped (email not found or reset failed)"
        fi
    fi
fi

# Generate encryption keys (if not already set)
if [ -z "$RSA_PRIVATE_KEY" ]; then
    export RSA_PRIVATE_KEY=$(openssl genrsa 2048 2>/dev/null)
fi
if [ -z "$DATA_ENCRYPTION_KEY" ]; then
    export DATA_ENCRYPTION_KEY=$(openssl rand -base64 32)
fi

# Generate nginx config
cat > /etc/nginx/http.d/default.conf << NGINX_EOF
server {
    listen $PORT;
    server_name _;
    root /usr/share/nginx/html;
    index index.html;
    gzip on;
    gzip_types text/plain text/css application/json application/javascript;

    location / {
        try_files \$uri \$uri/ /index.html;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8081/api/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_connect_timeout 300s;
        proxy_send_timeout 300s;
        proxy_read_timeout 300s;
    }

    location /health {
        return 200 'OK';
        add_header Content-Type text/plain;
    }
}
NGINX_EOF

# Start backend (port 8081)
API_SERVER_PORT=8081 /app/nofx &
sleep 2

# Start nginx (background)
nginx

echo "✅ NOFX started successfully"

# Keep the container running
tail -f /dev/null
