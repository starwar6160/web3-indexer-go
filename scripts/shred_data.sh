#!/bin/bash

# --- 🚀 Yokohama Lab: Physical Shredder ---
# "Nuke everything to ensure absolute zero-state."

echo "🔥 SHREDDING all persistent and cached data..."

# 1. Docker Volumes (The Big Hammer)
if [ -f "docker-compose.yml" ]; then
    docker-compose down -v --remove-orphans
fi

# 2. Local DB files (if any)
rm -rf *.db *.db-journal *.db-shm *.db-wal

# 3. Replay & Trajectory files
rm -rf *.lz4 *.jsonl

# 4. Binary & Log artifacts
rm -rf bin/ logs/ tmp/*.log *.out

# 5. Go Cache
go clean -cache -testcache

echo "✨ System is now PHYSICALLY PURE. Ready for Block 0."
