import time
import subprocess
import re
from collections import Counter

# 监控设置
LOG_FILE = "logs/indexer.log"
NGINX_LOG = "bin/gateway.log" # 假设您已将容器日志重定向

print("🕵️ Web3 Indexer 流量监控启动...")
print("[*] 正在监控异常扫描和数据库连接尝试...")

def get_last_lines(file_path, n=50):
    try:
        return subprocess.check_output(['tail', f'-n', str(n), file_path]).decode('utf-8')
    except:
        return ""

try:
    while True:
        # 1. 检查 Indexer 日志中的 db_fail
        indexer_logs = get_last_lines(LOG_FILE)
        if "db_fail" in indexer_logs:
            errors = re.findall(r'err="(.*?)"', indexer_logs)
            if errors:
                print(f"⚠️  检测到数据库连接异常: {errors[-1]}")

        # 2. 模拟检查连接数 (netstat)
        try:
            netstat = subprocess.check_output(['netstat', '-ant']).decode('utf-8')
            # 统计连接到 15432 (Postgres) 的外部 IP
            ext_conns = re.findall(r'(\d+\.\d+\.\d+\.\d+):15432\s+ESTABLISHED', netstat)
            # 过滤掉本地
            malicious = [ip for ip in ext_conns if not ip.startswith(('127.', '100.', '192.168.'))]
            
            if malicious:
                print(f"🚨 警报！发现未经授权的公网 IP 连接数据库: {Counter(malicious)}")
        except:
            pass

        time.sleep(10)
except KeyboardInterrupt:
    print("
监控停止。")
