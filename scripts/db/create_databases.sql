-- 自动创建物理隔离的数据库
SELECT 'CREATE DATABASE web3_sepolia' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'web3_sepolia')\gexec
SELECT 'CREATE DATABASE web3_demo' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'web3_demo')\gexec
SELECT 'CREATE DATABASE web3_debug' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'web3_debug')\gexec
